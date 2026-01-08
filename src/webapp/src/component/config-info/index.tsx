import React from "react";
import Editor from 'react-simple-code-editor';
import { highlight, languages } from 'prismjs';
import 'prismjs/components/prism-yaml';
import 'prismjs/components/prism-clike';
import 'prismjs/components/prism-javascript';
import 'prismjs/themes/prism.css';
import API from '../../utils/api';
import { Button, Drawer, Divider, Tag } from "antd";
import './config-info.css';

const api = new API();

interface Props { }

interface IState {
  config: any;
  helpVisible: boolean;
}

class ConfigInfo extends React.Component<Props, IState> {

  constructor(props: Props) {
    super(props);
    this.state = {
      config: null,
      helpVisible: false,
    }
  }

  componentDidMount(): void {
    api.getConfigInfo()
      .then((rsp: any) => {
        this.setState({
          config: rsp.config
        });
      })
      .catch(err => {
        alert("获取配置信息失败");
      });
  }

  /**
   * 保存设置至config文件
   */
  onSettingSave = () => {
    api.saveRawConfig({ config: this.state.config })
      .then((rsp: any) => {
        if (rsp.err_no === 0) {
          alert("设置保存成功");
        } else {
          alert(`Server Error!\n${rsp.err_msg}`);
        }
      })
      .catch(err => {
        alert("设置保存失败！");
      })
  }

  showHelp = () => {
    this.setState({ helpVisible: true });
  }

  onClose = () => {
    this.setState({ helpVisible: false });
  }

  renderHelpContent() {
    return (
      <div className="help-content-container">
        <div className="help-item">
          <Tag color="magenta">rpc</Tag>
          <div className="help-item-details">
            <div><span className="help-item-code">enable</span>: 是否启用 Web 后端服务。</div>
            <div><span className="help-item-code">bind</span>: Web 后端服务绑定端口，如 <code>:8080</code>。</div>
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="red">debug</Tag>
          <div className="help-item-details-single"> 是否开启调试模式。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="volcano">interval</Tag>
          <div className="help-item-details-single"> 轮询检查主播是否开播的周期（秒）。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="orange">out_put_path</Tag>
          <div className="help-item-details-single"> 录制文件的总输出保存目录。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="gold">ffmpeg_path</Tag>
          <div className="help-item-details-single"> FFmpeg 可执行文件的路径，留空则从系统路径查找。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="lime">log</Tag>
          <div className="help-item-details">
            <div><span className="help-item-code">out_put_folder</span>: 日志文件存储路径。</div>
            <div><span className="help-item-code">save_last_log</span>: 是否保留上一次运行的日志。</div>
            <div><span className="help-item-code">save_every_log</span>: 是否为每次运行保存独立日志。</div>
            <div><span className="help-item-code">rotate_days</span>: 日志保留天数，过期自动清理。</div>
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="green">feature</Tag>
          <div className="help-item-details">
            <div><span className="help-item-code">use_native_flv_parser</span>: 是否启用内置的高级 FLV 解析器（实验性）。</div>
            <div><span className="help-item-code">remove_symbol_other_character</span>: 是否移除文件名中的特殊非法字符。</div>
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="cyan">live_rooms</Tag>
          <div className="help-item-details-single"> 直播间列表，支持多个房间配置。内部参数：</div>
          <div className="help-item-details">
            <div><code>url</code>: 直播间完整链接。</div>
            <div><code>is_listening</code>: 是否启用监控。</div>
            <div><code>quality</code>: 录制画质。B站 quality 参数说明：
              <ul style={{ paddingLeft: 16, marginTop: 4, marginBottom: 4, fontSize: '11px', color: '#666' }}>
                <li><b>0</b>: 默认建议。自动请求最高画质（杜比视界/4K/原画）。</li>
                <li><b>30000</b>: 杜比视界（需大会员）。</li>
                <li><b>20000</b>: 4K (需大会员)。</li>
                <li><b>10000</b>: 1080P/原画。</li>
                <li><b>250</b>: 720P/超清。</li>
                <li><b>80</b>: 360P/流畅。</li>
              </ul>
            </div>
            <div><code>audio_only</code>: 是否仅录制音频。</div>
            <div><code>nick_name</code>: 别名，用于显示和文件名。</div>
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="blue">out_put_tmpl</Tag>
          <div className="help-item-details-single"> 文件名模板代码，支持时间、主播名、标题等变量。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="geekblue">video_split_strategies</Tag>
          <div className="help-item-details">
            <div><span className="help-item-code">on_room_name_changed</span>: 直播间修改标题时是否强制另起文件录制。</div>
            <div><span className="help-item-code">max_duration</span>: 单个文件最长时间，超出时长自动切分文件（如 <code>2h</code>）。</div>
            <div><span className="help-item-code">max_file_size</span>: 单个文件最大大小，超出大小自动切分文件（字节）。</div>
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="purple">cookies</Tag>
          <div className="help-item-details-single"> 域名到 Cookie 的映射，用于解决高清画质权限问题。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="magenta">on_record_finished</Tag>
          <div className="help-item-details">
            <div><span className="help-item-code">convert_to_mp4</span>: 结束后是否自动转换为 MP4。</div>
            <div><span className="help-item-code">delete_flv_after_convert</span>: 转码后是否删除原始文件。</div>
            <div><span className="help-item-code">custom_commandline</span>: 录制结束后执行的自定义 Shell 命令。</div>
            <div><span className="help-item-code">fix_flv_at_first</span>: 结束后是否先行修复 FLV 损坏（推荐）。</div>
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="red">timeout_in_us</Tag>
          <div className="help-item-details-single"> 网络请求超时时间（微秒）。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="volcano">notify</Tag>
          <div className="help-item-details">
            <strong>Telegram:</strong> 包含 <code>enable</code>, <code>botToken</code>, <code>chatID</code> 等。<br />
            <strong>Email:</strong> 包含 <code>enable</code>, <code>smtpHost</code> 等配置。
          </div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="orange">app_data_path</Tag>
          <div className="help-item-details-single"> 应用程序数据的持久化目录。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="gold">read_only_tool_folder</Tag>
          <div className="help-item-details-single"> 只读工具存放目录（通常用于 Docker 预置）。</div>
        </div>

        <Divider />

        <div className="help-item">
          <Tag color="lime">tool_root_folder</Tag>
          <div className="help-item-details-single"> 工具（如 FFmpeg, node）下载安装的根目录。</div>
        </div>
      </div>
    );
  }

  render() {
    if (this.state.config === null) {
      return <div>loading...</div>;
    }
    return (
      <div className="config-info-container">
        <div className="config-header">
          <div>
            <span className="config-title">系统配置文件编辑器</span>
            <Button
              type="primary"
              ghost
              icon="question-circle"
              onClick={this.showHelp}
            >
              参数详细说明
            </Button>
          </div>
          <Button
            type="primary"
            size="large"
            onClick={this.onSettingSave}
          >
            保存并应用设置
          </Button>
        </div>

        <div style={{
          border: '1px solid #e8e8e8',
          borderRadius: '8px',
          overflow: 'hidden',
          boxShadow: '0 4px 12px rgba(0,0,0,0.08)'
        }}>
          <Editor
            value={this.state.config}
            onValueChange={code => this.setState({ config: code })}
            highlight={code => highlight(code, languages.yaml, 'yaml')}
            padding={20}
            style={{
              fontFamily: '"Fira Code", "Fira Mono", "JetBrains Mono", monospace',
              fontSize: 14,
              minHeight: '600px',
              backgroundColor: '#fafafa'
            }}
          />
        </div>

        <Drawer
          title={<span style={{ fontWeight: 'bold' }}><span role="img" aria-label="doc">📑</span> 全部配置参数详细说明</span>}
          placement="right"
          closable={true}
          onClose={this.onClose}
          visible={this.state.helpVisible}
          width={500}
        >
          {this.renderHelpContent()}
        </Drawer>
      </div>
    );
  }
}

export default ConfigInfo;