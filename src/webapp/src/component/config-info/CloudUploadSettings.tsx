import React, { useMemo } from 'react';
import { Card, Form, Switch, Select, Input, Tag, Alert, Descriptions } from 'antd';
import { CloudUploadOutlined, FileOutlined, DeleteOutlined, UploadOutlined } from '@ant-design/icons';
import TemplateBuilder, { TemplateVariable, PresetTemplate, MockStream } from '../TemplateBuilder';

interface ConfigFieldProps {
  label: string;
  description?: string;
  children: React.ReactElement;
}

// 简化版 ConfigField 组件
const ConfigField: React.FC<ConfigFieldProps> = ({ label, description, children }) => (
  <div className="config-item" style={{ marginBottom: 16 }}>
    <div className="config-item-label" style={{ marginBottom: 4, fontWeight: 500 }}>{label}</div>
    <div className="config-item-content">
      <div className="config-item-input">{children}</div>
      {description && (
        <div className="config-item-description" style={{ marginTop: 4, color: '#888', fontSize: 12 }}>
          {description}
        </div>
      )}
    </div>
  </div>
);

// 上传路径模板 - 可用变量
const UPLOAD_VARIABLES: TemplateVariable[] = [
  { key: 'Platform', label: '平台', example: 'bilibili', template: '{{ .Platform }}' },
  { key: 'HostName', label: '主播名', example: '小辞炒糍粑', template: '{{ .HostName }}' },
  { key: 'RoomName', label: '房间名', example: '中午好今天单排！', template: '{{ .RoomName }}' },
  { key: 'FileName', label: '文件名', example: '[2026-08-05][小辞炒糍粑].flv', template: '{{ .FileName }}' },
  { key: 'Date', label: '日期', example: '2026-08-05', template: '{{ now | date "2006-01-02" }}' },
  { key: 'DateTime', label: '日期时间', example: '2026-08-05 15-04-05', template: '{{ now | date "2006-01-02 15-04-05" }}' },
  { key: 'Ext', label: '扩展名', example: 'flv', template: '{{ .Ext }}' },
];

// 上传路径模板 - 预设模板
const UPLOAD_PRESETS: PresetTemplate[] = [
  {
    name: '按日期归档',
    description: '按平台/主播/日期归档',
    template: '/录播归档/{{ .Platform }}/{{ .HostName }}/{{ now | date "2006-01-02" }}/{{ .FileName }}',
  },
  {
    name: '简洁归档',
    description: '按平台/主播归档，文件名带日期',
    template: '/录播归档/{{ .Platform }}/{{ .HostName }}/{{ .RoomName }}-{{ now | date "2006-01-02" }}.{{ .Ext }}',
  },
  {
    name: '按房间归档',
    description: '按平台/主播/房间名归档',
    template: '/录播归档/{{ .Platform }}/{{ .HostName }}/{{ .RoomName }}/{{ .FileName }}',
  },
  {
    name: '原始文件名',
    description: '保留原始文件名，按平台/主播归档',
    template: '/录播归档/{{ .Platform }}/{{ .HostName }}/{{ .FileName }}',
  },
];

// 模拟数据（用于预览）
const MOCK_DATA: MockStream[] = [
  {
    platform: 'bilibili',
    platformCN: '哔哩哔哩',
    hostName: '小辞炒糍粑',
    rooms: [
      { roomName: '中午好今天单排！', date: '2026-08-05', time: '12-00-00' },
      { roomName: '晚上吃鸡', date: '2026-08-05', time: '20-00-00' },
      { roomName: '中午好今天单排！', date: '2026-08-06', time: '12-00-00' },
    ],
  },
  {
    platform: 'bilibili',
    platformCN: '哔哩哔哩',
    hostName: '老番茄',
    rooms: [
      { roomName: '杂谈闲聊', date: '2026-08-05', time: '19-00-00' },
      { roomName: '杂谈闲聊', date: '2026-08-07', time: '19-00-00' },
    ],
  },
  {
    platform: 'douyu',
    platformCN: '斗鱼',
    hostName: '一条小团团',
    rooms: [
      { roomName: '团团的直播间', date: '2026-08-05', time: '21-00-00' },
      { roomName: '团团的直播间', date: '2026-08-06', time: '21-00-00' },
    ],
  },
];

// 渲染上传路径模板
const renderUploadTemplate = (template: string, stream: MockStream, room: MockStream['rooms'][0]): string => {
  const fileName = `[${room.date} ${room.time}][${stream.hostName}][${room.roomName}].flv`;
  let path = template;
  path = path.replace(/\{\{ ?\.Platform ?\}\}/g, stream.platform);
  path = path.replace(/\{\{ ?\.HostName ?\}\}/g, stream.hostName);
  path = path.replace(/\{\{ ?\.RoomName ?\}\}/g, room.roomName);
  path = path.replace(/\{\{ ?\.FileName ?\}\}/g, fileName);
  path = path.replace(/\{\{ ?\.Ext ?\}\}/g, 'flv');
  path = path.replace(/\{\{ ?now \| date "2006-01-02" ?\}\}/g, room.date);
  path = path.replace(/\{\{ ?now \| date "2006-01-02 15-04-05" ?\}\}/g, `${room.date} ${room.time}`);
  return path;
};

// 分析配置，返回上传文件和本地剩余文件
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const analyzeConfig = (config: any) => {
  const orf = config.on_record_finished || {};
  const cu = orf.cloud_upload || {};
  const isImmediate = orf.upload_timing === 'immediate';
  const isAfterProcess = !isImmediate;

  // 功能开关
  const convert = orf.convert_to_mp4 ?? false;
  const deleteFlv = orf.delete_flv_after_convert ?? false;
  const burn = orf.burn_subtitles ?? false;
  const burnDelSource = orf.burn_delete_source ?? false;
  const burnDelAss = orf.burn_delete_ass ?? false;
  const cover = orf.save_cover ?? false;
  const upload = cu.enable ?? false;
  const delAfter = cu.delete_after_upload ?? false;
  const delAllAfter = cu.delete_all_after_upload ?? false;

  // 文件状态：uploaded(上传), deleted(删除), kept(保留)
  const files: Record<string, { ext: string; desc: string; status: string }> = {};

  // 初始文件
  files['video.flv'] = { ext: '.flv', desc: '原始录制视频', status: 'kept' };
  files['video.ass'] = { ext: '.ass', desc: '弹幕字幕', status: 'kept' };

  // immediate 模式：先上传原始文件
  if (isImmediate && upload) {
    files['video.flv'].status = 'uploaded';
  }

  // 转码
  if (convert) {
    files['video.mp4'] = { ext: '.mp4', desc: '转码后视频', status: 'kept' };
    if (deleteFlv) {
      // 如果已经被标记为 uploaded，改为 uploaded_deleted
      if (files['video.flv'].status === 'uploaded') {
        files['video.flv'].status = 'uploaded_deleted';
      } else {
        files['video.flv'].status = 'deleted';
      }
    }
  }

  // 烧录
  if (burn) {
    files['video.mkv'] = { ext: '.mkv', desc: '烧录后视频', status: 'kept' };
    if (burnDelSource) {
      // 删除转码后的 mp4 或原始 flv
      const target = convert ? 'video.mp4' : 'video.flv';
      if (files[target].status === 'uploaded') {
        files[target].status = 'uploaded_deleted';
      } else {
        files[target].status = 'deleted';
      }
    }
    if (burnDelAss) {
      files['video.ass'].status = 'deleted';
    }
  }

  // 封面
  if (cover) {
    files['cover.jpg'] = { ext: '.jpg', desc: '视频封面', status: 'kept' };
  }

  // after_process 模式：上传最终文件
  if (isAfterProcess && upload) {
    // 上传第一个视频和封面
    if (burn) {
      files['video.mkv'].status = 'uploaded';
    } else if (convert) {
      files['video.mp4'].status = 'uploaded';
    } else {
      files['video.flv'].status = 'uploaded';
    }
    if (cover) {
      files['cover.jpg'].status = 'uploaded';
    }

    // 删除逻辑
    if (delAllAfter) {
      // 删除全部
      Object.values(files).forEach(f => { f.status = 'deleted'; });
      // 但上传的文件标记为 uploaded_deleted
      if (burn) files['video.mkv'].status = 'uploaded_deleted';
      else if (convert) files['video.mp4'].status = 'uploaded_deleted';
      else files['video.flv'].status = 'uploaded_deleted';
      if (cover) files['cover.jpg'].status = 'uploaded_deleted';
    } else if (delAfter) {
      // 只删除已上传的文件
      if (burn) files['video.mkv'].status = 'uploaded_deleted';
      else if (convert) files['video.mp4'].status = 'uploaded_deleted';
      else files['video.flv'].status = 'uploaded_deleted';
      if (cover) files['cover.jpg'].status = 'uploaded_deleted';
    }
  }

  // 兜底逻辑：清理无关联视频文件的 .ass 文件
  if (files['video.ass'] && files['video.ass'].status === 'kept') {
    const hasVideo = Object.entries(files).some(([name, f]) => {
      if (name === 'video.ass') return false;
      const ext = f.ext.toLowerCase();
      return (ext === '.flv' || ext === '.mp4' || ext === '.mkv') && f.status !== 'deleted' && f.status !== 'uploaded_deleted';
    });
    if (!hasVideo) {
      files['video.ass'].status = 'deleted';
    }
  }

  // 分类结果
  const uploaded: string[] = [];
  const deleted: string[] = [];
  const kept: string[] = [];

  Object.entries(files).forEach(([, f]) => {
    const name = f.desc + ' (' + f.ext + ')';
    if (f.status === 'uploaded') {
      uploaded.push(name);
      kept.push(name); // 上传但本地保留
    } else if (f.status === 'uploaded_deleted') {
      uploaded.push(name);
      deleted.push(name);
    } else if (f.status === 'deleted') {
      deleted.push(name);
    } else {
      kept.push(name);
    }
  });

  return { uploaded, deleted, kept };
};

// 文件处理预览组件
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const FileProcessingPreview: React.FC<{ config: any }> = ({ config }) => {
  const result = useMemo(() => analyzeConfig(config), [config]);

  if (!config.on_record_finished?.cloud_upload?.enable) {
    return null;
  }

  return (
    <Card
      size="small"
      title={<><FileOutlined /> 文件处理预览</>}
      style={{ marginBottom: 16, background: '#fafafa' }}
    >
      <Descriptions column={1} size="small" labelStyle={{ width: 100 }}>
        <Descriptions.Item label={<><UploadOutlined /> 上传文件</>}>
          {result.uploaded.length > 0
            ? result.uploaded.map((f, i) => <Tag key={i} color="blue">{f}</Tag>)
            : <Tag>无</Tag>
          }
        </Descriptions.Item>
        <Descriptions.Item label={<><DeleteOutlined /> 删除文件</>}>
          {result.deleted.length > 0
            ? result.deleted.map((f, i) => <Tag key={i} color="red">{f}</Tag>)
            : <Tag>无</Tag>
          }
        </Descriptions.Item>
        <Descriptions.Item label={<><FileOutlined /> 本地保留</>}>
          {result.kept.length > 0
            ? result.kept.map((f, i) => <Tag key={i} color="green">{f}</Tag>)
            : <Tag>无（全部清理）</Tag>
          }
        </Descriptions.Item>
      </Descriptions>
    </Card>
  );
};

interface CloudUploadSettingsProps {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  config: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  form?: any; // Form instance for mutual exclusion
}

/**
 * 云盘上传设置组件
 * 用于 GlobalSettings 中显示云上传配置
 */
const CloudUploadSettings: React.FC<CloudUploadSettingsProps> = ({ config, form }) => {
  const isEnabled = config.on_record_finished?.cloud_upload?.enable;

  // 互斥逻辑：开启一个时关闭另一个
  const handleDeleteAfterChange = (checked: boolean) => {
    if (checked && form) {
      form.setFieldValue(['on_record_finished', 'cloud_upload', 'delete_all_after_upload'], false);
    }
  };

  const handleDeleteAllAfterChange = (checked: boolean) => {
    if (checked && form) {
      form.setFieldValue(['on_record_finished', 'cloud_upload', 'delete_after_upload'], false);
    }
  };

  return (
    <Card
      title={<><CloudUploadOutlined /> 云盘上传</>}
      size="small"
      style={{ marginBottom: 16 }}
      extra={
        <Tag color={isEnabled ? 'green' : 'default'}>{isEnabled ? '已启用' : '未启用'}</Tag>
      }
    >
      <Alert
        message="云盘自动上传"
        description={
          <>
            录制结束后自动把视频传到网盘。需要先在{' '}
            <a
              href={`http://${window.location.hostname}:${config.openlist?.port || 5244}`}
              target="_blank"
              rel="noopener noreferrer"
            >
              OpenList 管理页面
            </a>{' '}
            添加网盘。
          </>
        }
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
      />

      {/* 文件处理预览 */}
      <FileProcessingPreview config={config} />

      <ConfigField label="启用云上传" description="录制结束后自动把视频上传到网盘">
        <Form.Item name={['on_record_finished', 'cloud_upload', 'enable']} valuePropName="checked" noStyle>
          <Switch />
        </Form.Item>
      </ConfigField>
      <ConfigField label="上传时机" description="选择上传哪种文件：原始录制文件 or 处理后的最终文件">
        <Form.Item name={['on_record_finished', 'upload_timing']} noStyle>
          <Select style={{ width: 350 }} placeholder="选择上传时机">
            <Select.Option value="">处理完再上传（默认）</Select.Option>
            <Select.Option value="immediate">先上传再处理</Select.Option>
            <Select.Option value="after_process">处理完再上传</Select.Option>
          </Select>
        </Form.Item>
      </ConfigField>
      <div style={{ marginTop: -12, marginBottom: 16, marginLeft: 140, color: '#888', fontSize: 12 }}>
        <div><b>先上传再处理</b>：录制结束后立刻上传原始文件，然后再做修复、转码、烧录</div>
        <div><b>处理完再上传</b>：先做修复、转码、烧录，全部完成后再上传最终文件</div>
      </div>
      <ConfigField label="存储名称" description="你在 OpenList 里添加的网盘名称，比如 115、阿里云盘">
        <Form.Item name={['on_record_finished', 'cloud_upload', 'storage_name']} noStyle>
          <Input placeholder="例如: 115" style={{ width: 200 }} />
        </Form.Item>
      </ConfigField>

      {/* 上传路径模板 - 使用通用 TemplateBuilder 组件 */}
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8, fontWeight: 500 }}>上传路径模板</div>
        <TemplateBuilder
          name={['on_record_finished', 'cloud_upload', 'upload_path_tmpl']}
          variables={UPLOAD_VARIABLES}
          presets={UPLOAD_PRESETS}
          mockData={MOCK_DATA}
          renderTemplate={renderUploadTemplate}
          placeholder="/录播归档/{{ .Platform }}/{{ .HostName }}/{{ now | date '2006-01-02' }}/{{ .FileName }}"
          showTreePreview={true}
          width={500}
        />
      </div>

      <ConfigField label="上传后删除本地文件" description="选「处理完再上传」时，上传成功后仅删除已上传的文件（如最终视频），不影响其他中间文件。选「先上传再处理」时此开关无效">
        <Form.Item name={['on_record_finished', 'cloud_upload', 'delete_after_upload']} valuePropName="checked" noStyle>
          <Switch onChange={handleDeleteAfterChange} />
        </Form.Item>
      </ConfigField>
      <ConfigField label="上传后删除全部文件" description="选「处理完再上传」时，上传成功后删除所有本地文件（含中间产物）。选「先上传再处理」时此开关无效">
        <Form.Item name={['on_record_finished', 'cloud_upload', 'delete_all_after_upload']} valuePropName="checked" noStyle>
          <Switch onChange={handleDeleteAllAfterChange} />
        </Form.Item>
      </ConfigField>

      <Card type="inner" title="OpenList 认证配置" size="small" style={{ marginTop: 16, marginBottom: 8 }}>
        <Alert
          message="关于账号密码"
          description={<>首次启动程序时会自动生成密码并填入下方，无需手动操作。如果在 OpenList 网页改了密码，程序会自动重新登录。</>}
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
        />
        <ConfigField label="管理员用户名" description="首次启动自动填入，一般不需要改">
          <Form.Item name={['openlist', 'username']} noStyle>
            <Input placeholder="admin" style={{ width: 200 }} />
          </Form.Item>
        </ConfigField>
        <ConfigField label="管理员密码" description="首次启动自动填入，一般不需要改">
          <Form.Item name={['openlist', 'password']} noStyle>
            <Input.Password placeholder="首次启动后自动填充" style={{ width: 200 }} />
          </Form.Item>
        </ConfigField>
      </Card>
    </Card>
  );
};

export default CloudUploadSettings;
