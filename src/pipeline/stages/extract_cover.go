package stages

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/bililive-go/bililive-go/src/configs"
	"github.com/bililive-go/bililive-go/src/pipeline"
	"github.com/bililive-go/bililive-go/src/pkg/openlist"
	"github.com/bililive-go/bililive-go/src/tools"
)

// ExtractCoverStage 封面提取阶段
type ExtractCoverStage struct {
	config   pipeline.StageConfig
	commands []string
	logs     string
}

// NewExtractCoverStage 创建封面提取阶段工厂
func NewExtractCoverStage(config pipeline.StageConfig) (pipeline.Stage, error) {
	return &ExtractCoverStage{
		config: config,
	}, nil
}

func (s *ExtractCoverStage) Name() string {
	return pipeline.StageNameExtractCover
}

func (s *ExtractCoverStage) Execute(ctx *pipeline.PipelineContext, input []pipeline.FileInfo) ([]pipeline.FileInfo, error) {
	if len(input) == 0 {
		s.logs = "没有输入文件"
		return input, nil
	}

	var output []pipeline.FileInfo

	// 先添加所有输入文件到输出
	output = append(output, input...)

	// 对每个视频文件提取封面
	for _, file := range input {
		// 只处理视频文件
		if file.Type != pipeline.FileTypeVideo {
			continue
		}

		// 检查文件是否存在
		if _, err := os.Stat(file.Path); os.IsNotExist(err) {
			s.logs += fmt.Sprintf("文件不存在: %s\n", file.Path)
			continue
		}

		ctx.Logger.Infof("提取封面: %s", file.Path)

		// 提取封面
		coverPath, err := tools.ExtractCover(ctx.Ctx, file.Path)
		if err != nil {
			s.logs += fmt.Sprintf("提取封面失败: %s - %s\n", filepath.Base(file.Path), err.Error())
			ctx.Logger.Warnf("提取封面失败: %s - %s", file.Path, err)
			continue
		}

		// 添加封面文件到输出
		output = append(output, pipeline.FileInfo{
			Path:       coverPath,
			Type:       pipeline.FileTypeCover,
			SourcePath: file.Path,
		})

		s.logs += fmt.Sprintf("封面已保存: %s\n", filepath.Base(coverPath))
		ctx.Logger.Infof("封面已保存: %s", coverPath)
	}

	return output, nil
}

func (s *ExtractCoverStage) GetCommands() []string {
	return s.commands
}

func (s *ExtractCoverStage) GetLogs() string {
	return s.logs
}

// CloudUploadStage 云上传阶段
type CloudUploadStage struct {
	config         pipeline.StageConfig
	storageName    string
	pathTemplate   string
	deleteAfter    bool   // 仅删除已上传的文件
	deleteAllAfter bool   // 删除全部文件（含中间产物）
	uploadTiming   string // immediate 或 after_process
	fileTypes      []string // 过滤的文件类型，空表示所有
	commands       []string
	logs           string
}

// NewCloudUploadStage 创建云上传阶段工厂
func NewCloudUploadStage(config pipeline.StageConfig) (pipeline.Stage, error) {
	return &CloudUploadStage{
		config:         config,
		storageName:    config.GetStringOption(pipeline.OptionStorage, ""),
		pathTemplate:   config.GetStringOption(pipeline.OptionPathTemplate, ""),
		deleteAfter:    config.GetBoolOption(pipeline.OptionDeleteAfter, false),
		deleteAllAfter: config.GetBoolOption(pipeline.OptionDeleteAllAfter, false),
		uploadTiming:   config.GetStringOption(pipeline.OptionUploadTiming, ""),
		fileTypes:      config.GetStringSliceOption(pipeline.OptionFileTypes),
	}, nil
}

func (s *CloudUploadStage) Name() string {
	return pipeline.StageNameCloudUpload
}

func (s *CloudUploadStage) Execute(ctx *pipeline.PipelineContext, input []pipeline.FileInfo) ([]pipeline.FileInfo, error) {
	if len(input) == 0 {
		s.logs = "没有输入文件"
		return input, nil
	}

	if s.storageName == "" {
		s.logs = "未配置存储名称，跳过上传"
		return input, nil
	}

	// 获取 OpenList 管理器
	mgr := openlist.GetGlobalManager()
	if mgr == nil {
		s.logs = "OpenList 管理器未初始化，跳过上传\n"
		return input, nil
	}

	// 获取配置并创建客户端
	config := configs.GetCurrentConfig()

	client, err := mgr.GetClient(ctx.Ctx, config.OpenList.Token, config.OpenList.Username, config.OpenList.Password)
	if err != nil {
		s.logs += fmt.Sprintf("创建 OpenList 客户端失败: %s\n", err.Error())
		return input, fmt.Errorf("创建 OpenList 客户端失败: %w", err)
	}

	// 如果 token 变化了（通过用户名密码登录获取了新 token），自动写回配置
	if newToken := client.GetCurrentToken(); newToken != "" && newToken != config.OpenList.Token {
		config.OpenList.Token = newToken
		if err := config.Marshal(); err != nil {
			ctx.Logger.Warnf("保存 OpenList token 到配置文件失败: %v", err)
		} else {
			ctx.Logger.Info("OpenList token 已自动更新到配置文件")
		}
	}

	var output []pipeline.FileInfo
	var uploadFailed bool

	// 清除所有输入文件的 Deletable 标记（防止数据库中残留的旧标记影响本次判断）
	for i := range input {
		input[i].Deletable = false
	}

	// 只上传第一个视频文件和封面文件，跳过中间产物
	// Pipeline 阶段输出顺序保证：最终视频在最前面，中间文件在后面
	videoUploaded := false
	uploadedIndices := map[int]bool{} // 记录实际上传的文件在 output 中的索引

	for _, file := range input {
		// 跳过标记为待删除的中间文件（由其他阶段产出，不应上传）
		if file.Deletable {
			output = append(output, file)
			continue
		}

		// 文件类型过滤
		if len(s.fileTypes) > 0 && !s.matchFileType(file.Type) {
			output = append(output, file)
			continue
		}

		// 视频文件：只上传第一个（最终视频），跳过后续中间视频
		if file.Type == pipeline.FileTypeVideo {
			if videoUploaded {
				output = append(output, file)
				continue
			}
			videoUploaded = true
		}

		// 检查文件是否存在
		if _, err := os.Stat(file.Path); os.IsNotExist(err) {
			s.logs += fmt.Sprintf("文件不存在: %s\n", file.Path)
			continue
		}

		// 渲染目标路径
		targetPath := s.renderTargetPath(ctx, file)
		if targetPath == "" {
			s.logs += fmt.Sprintf("无法生成目标路径: %s\n", file.Path)
			output = append(output, file)
			uploadFailed = true
			continue
		}

		ctx.Logger.Infof("上传文件: %s -> %s", file.Path, targetPath)
		s.commands = append(s.commands, fmt.Sprintf("upload %s to %s/%s", file.Path, s.storageName, targetPath))

		// 构建完整的远程路径（storage + targetPath）
		// OpenList API 的 File-Path 需要包含存储路径
		remotePath := targetPath
		if !strings.HasPrefix(remotePath, "/") {
			remotePath = "/" + remotePath
		}
		// 确保存储名和路径之间没有双斜杠
		fullRemotePath := "/" + s.storageName + remotePath

		// 确保远程目录存在（递归创建）
		// 使用 path.Dir 而非 filepath.Dir，因为远程路径始终用正斜杠
		remoteDir := path.Dir(fullRemotePath)
		if remoteDir != "" && remoteDir != "." {
			if err := client.MkdirRecursive(ctx.Ctx, remoteDir); err != nil {
				ctx.Logger.Warnf("创建远程目录失败（可能已存在）: %s - %v", remoteDir, err)
			}
		}

		// 执行上传（带进度追踪）
		fileName := filepath.Base(file.Path)
		ctx.Logger.Infof("开始上传: %s -> %s/%s", fileName, s.storageName, targetPath)
		err := client.Upload(ctx.Ctx, file.Path, fullRemotePath, func(p openlist.UploadProgress) {
			// 进度回调仅用于内部追踪，不打印日志
		})

		if err != nil {
			s.logs += fmt.Sprintf("上传失败: %s -> %s/%s - %s\n", fileName, s.storageName, targetPath, err.Error())
			ctx.Logger.Errorf("上传失败: %s - %v", file.Path, err)
			// 上传失败，保留文件
			output = append(output, file)
			uploadFailed = true
			continue
		}

		s.logs += fmt.Sprintf("上传成功: %s -> %s/%s\n", fileName, s.storageName, targetPath)
		ctx.Logger.Infof("文件上传成功: %s -> %s/%s", fileName, s.storageName, targetPath)
		uploadedIndices[len(output)] = true // 记录上传成功的文件索引
		output = append(output, file)
	}

	// 全部上传成功后，根据配置标记文件为可删除（由 Executor 在管道全部成功后统一删除）
	// 任意一个上传失败，则不标记任何文件，确保本地文件全部保留
	if !uploadFailed && s.uploadTiming != "immediate" {
		if s.deleteAllAfter {
			// 删除全部文件（含中间产物）
			for i := range output {
				output[i].Deletable = true
			}
			s.logs += fmt.Sprintf("全部上传成功，已标记 %d 个文件待删除（含中间产物）\n", len(output))
			ctx.Logger.Infof("全部上传成功，已标记 %d 个文件待删除（含中间产物）", len(output))
		} else if s.deleteAfter {
			// 仅删除已上传的文件
			count := 0
			for i := range output {
				if uploadedIndices[i] {
					output[i].Deletable = true
					count++
				}
			}
			s.logs += fmt.Sprintf("全部上传成功，已标记 %d 个已上传文件待删除\n", count)
			ctx.Logger.Infof("全部上传成功，已标记 %d 个已上传文件待删除", count)
		}
	}

	// 任意上传失败，返回错误（让 Executor 将任务标记为 failed）
	if uploadFailed {
		return output, fmt.Errorf("部分文件上传失败，详见日志")
	}

	return output, nil
}

// matchFileType 检查文件类型是否匹配
func (s *CloudUploadStage) matchFileType(fileType pipeline.FileType) bool {
	for _, ft := range s.fileTypes {
		if strings.EqualFold(ft, string(fileType)) {
			return true
		}
	}
	return false
}

// renderTargetPath 渲染目标路径
func (s *CloudUploadStage) renderTargetPath(ctx *pipeline.PipelineContext, file pipeline.FileInfo) string {
	if s.pathTemplate == "" {
		// 默认路径：/录播归档/{平台}/{主播名}/{文件名}
		return fmt.Sprintf("/录播归档/%s/%s/%s",
			ctx.RecordInfo.Platform,
			ctx.RecordInfo.HostName,
			filepath.Base(file.Path),
		)
	}

	// 获取扩展名
	ext := filepath.Ext(file.Path)
	if len(ext) > 0 && ext[0] == '.' {
		ext = ext[1:]
	}

	// 模板数据
	data := struct {
		Platform string
		HostName string
		RoomName string
		FileName string
		Ext      string
	}{
		Platform: ctx.RecordInfo.Platform,
		HostName: ctx.RecordInfo.HostName,
		RoomName: ctx.RecordInfo.RoomName,
		FileName: filepath.Base(file.Path),
		Ext:      ext,
	}

	// 使用 Go 模板引擎
	funcMap := template.FuncMap{
		"date": func(format string, t time.Time) string {
			return t.Format(format)
		},
		"now": func() time.Time {
			return time.Now()
		},
		"trimSuffix": func(suffix, s string) string {
			return strings.TrimSuffix(s, suffix)
		},
		"ext": func(path string) string {
			return filepath.Ext(path)
		},
		"filenameFilter": func(s string) string {
			// 过滤文件名中的非法字符
			replacer := strings.NewReplacer(
				"/", "_", "\\", "_", ":", "_", "*", "_",
				"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
			)
			return replacer.Replace(s)
		},
	}

	tmpl, err := template.New("path").Funcs(funcMap).Parse(s.pathTemplate)
	if err != nil {
		// 模板解析失败，回退到简单替换
		return s.fallbackRender(data)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// 模板执行失败，回退到简单替换
		return s.fallbackRender(data)
	}

	return buf.String()
}

// fallbackRender 简单字符串替换（模板引擎失败时的回退方案）
func (s *CloudUploadStage) fallbackRender(data struct {
	Platform string
	HostName string
	RoomName string
	FileName string
	Ext      string
}) string {
	path := s.pathTemplate
	path = strings.ReplaceAll(path, "{{ .Platform }}", data.Platform)
	path = strings.ReplaceAll(path, "{{.Platform}}", data.Platform)
	path = strings.ReplaceAll(path, "{{ .HostName }}", data.HostName)
	path = strings.ReplaceAll(path, "{{.HostName}}", data.HostName)
	path = strings.ReplaceAll(path, "{{ .RoomName }}", data.RoomName)
	path = strings.ReplaceAll(path, "{{.RoomName}}", data.RoomName)
	path = strings.ReplaceAll(path, "{{ .FileName }}", data.FileName)
	path = strings.ReplaceAll(path, "{{.FileName}}", data.FileName)
	path = strings.ReplaceAll(path, "{{ .Ext }}", data.Ext)
	path = strings.ReplaceAll(path, "{{.Ext}}", data.Ext)
	return path
}

func (s *CloudUploadStage) GetCommands() []string {
	return s.commands
}

func (s *CloudUploadStage) GetLogs() string {
	return s.logs
}
