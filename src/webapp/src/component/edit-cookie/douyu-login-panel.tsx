import React, { useState, useEffect, useRef, useCallback } from 'react';
import { Button, Spin, Input, Badge, Alert, Divider, notification } from 'antd';
import API from '../../utils/api';
import './edit-cookie.css';

const { TextArea } = Input;

interface DouyuLoginPanelProps {
    initialCookie: string;
    onCookieChange: (cookie: string) => void;
    /** 扫码登录成功后由父组件重新拉取服务端 Cookie 并同步显示基准 */
    onScanSuccess?: () => void;
    api: API;
}

type QRStatus = 'loading' | 'active' | 'scanned' | 'expired' | 'success';

interface DouyuIdentity {
    uid: string;
    nickname: string;
}

/**
 * 斗鱼扫码登录面板。
 * 与 B 站面板不同：确认登录后由后端自动完成 cookie 换取与保存，
 * 前端只负责展示二维码、轮询状态并提示结果。
 * 斗鱼没有独立的用户信息校验接口，登录身份直接从 cookie 的
 * acf_uid / acf_nickname 字段解析。
 */

// 从 cookie 串中解析斗鱼登录身份（acf_uid / acf_nickname，后者为 URL 编码）
const parseDouyuIdentity = (cookieStr: string): DouyuIdentity | null => {
    if (!cookieStr) return null;
    const get = (name: string): string => {
        const match = cookieStr.match(new RegExp(`(?:^|;)\\s*${name}=([^;]*)`));
        return match ? match[1].trim() : '';
    };
    const uid = get('acf_uid');
    if (!uid) return null;
    const rawNick = get('acf_nickname');
    let nickname = rawNick;
    try {
        nickname = decodeURIComponent(rawNick);
    } catch (e) {
        // 保持原值
    }
    return { uid, nickname };
};

// 解析 acf_jwt_token（JWT，exp 签发时固定为 7 天后）payload 的过期时间，返回毫秒时间戳；
// 无 token 或非标准 JWT 时返回 null
const parseDouyuCookieExpiry = (cookieStr: string): number | null => {
    if (!cookieStr) return null;
    const match = cookieStr.match(/(?:^|;)\s*acf_jwt_token=([^;]+)/);
    if (!match) return null;
    const parts = match[1].trim().split('.');
    if (parts.length !== 3) return null;
    try {
        let b64 = parts[1].replace(/-/g, '+').replace(/_/g, '/');
        b64 += '='.repeat((4 - (b64.length % 4)) % 4);
        const payload = JSON.parse(atob(b64));
        return typeof payload.exp === 'number' ? payload.exp * 1000 : null;
    } catch (e) {
        return null;
    }
};

const DouyuLoginPanel: React.FC<DouyuLoginPanelProps> = ({ initialCookie, onCookieChange, onScanSuccess, api }) => {
    const [qrCodeUrl, setQrCodeUrl] = useState('');
    const [loginStatus, setLoginStatus] = useState<QRStatus>('loading');
    const [loginMsg, setLoginMsg] = useState('正在获取二维码...');
    const [loginUser, setLoginUser] = useState<DouyuIdentity | null>(null);
    const [textView, setTextView] = useState(initialCookie);

    const pollTimerRef = useRef<any>(null);
    const isMounted = useRef(true);
    // 连续轮询失败计数：后端持续异常（如网络中断）时避免每 2s 无限重试
    const pollErrorCountRef = useRef(0);

    const stopPolling = useCallback(() => {
        if (pollTimerRef.current) {
            clearInterval(pollTimerRef.current);
            pollTimerRef.current = null;
        }
    }, []);

    // 父组件在扫码成功后会重新拉取服务端 cookie 并更新 initialCookie，同步到本地显示
    useEffect(() => {
        setTextView(initialCookie);
    }, [initialCookie]);

    const startPolling = useCallback((code: string) => {
        stopPolling();
        pollErrorCountRef.current = 0;
        pollTimerRef.current = setInterval(() => {
            api.pollDouyuQRCode(code)
                .then((res: any) => {
                    if (!isMounted.current) return;
                    // 后端统一响应格式为 {err_no, err_msg, data}
                    if (res.err_no !== 0 || !res.data) {
                        pollErrorCountRef.current += 1;
                    } else {
                        pollErrorCountRef.current = 0;
                    }
                    if (pollErrorCountRef.current >= 5) {
                        stopPolling();
                        setLoginStatus('expired');
                        setLoginMsg('登录状态查询连续失败，请刷新二维码重试');
                        return;
                    }
                    if (res.err_no !== 0 || !res.data) return;
                    const data = res.data;
                    switch (data.state) {
                        case 'waiting':
                            setLoginStatus('active');
                            setLoginMsg('请用斗鱼 App 扫码');
                            break;
                        case 'scanned':
                            setLoginStatus('scanned');
                            setLoginMsg('已扫码，请在手机上确认登录');
                            break;
                        case 'success':
                            stopPolling();
                            setLoginStatus('success');
                            setLoginMsg('登录成功，Cookie 已自动保存并生效');
                            setLoginUser({ uid: data.uid, nickname: data.nickname });
                            // 后端不回传 cookie 原文，重新拉取服务端最新值刷新 TextArea，
                            // 避免用户基于过期的旧文本编辑并保存、覆盖掉新登录态
                            onScanSuccess?.();
                            notification.success({
                                message: '斗鱼登录成功',
                                description: '登录 Cookie 已保存，新的录制将携带登录态（可消除约 5 分钟的匿名断流）',
                            });
                            break;
                        case 'expired':
                            stopPolling();
                            setLoginStatus('expired');
                            setLoginMsg('二维码已失效，请点击下方按钮刷新');
                            break;
                        case 'rejected':
                            stopPolling();
                            setLoginStatus('expired');
                            setLoginMsg('登录被取消或拒绝，请刷新二维码重试');
                            break;
                        default:
                            break;
                    }
                })
                .catch(console.error);
        }, 2000);
    }, [api, stopPolling, onScanSuccess]);

    const getDouyuQRCode = useCallback(() => {
        setLoginStatus('loading');
        setLoginMsg('正在获取二维码...');
        setLoginUser(null);
        api.getDouyuQRCode()
            .then((res: any) => {
                if (!isMounted.current) return;
                if (res.err_no === 0 && res.data) {
                    setQrCodeUrl(res.data.qr_url);
                    setLoginStatus('active');
                    setLoginMsg('请用斗鱼 App 扫码');
                    startPolling(res.data.code);
                } else {
                    setLoginStatus('expired');
                    setLoginMsg('获取二维码失败: ' + (res.err_msg || '未知错误'));
                }
            })
            .catch(err => {
                if (!isMounted.current) return;
                setLoginStatus('expired');
                setLoginMsg('获取二维码失败，请检查网络');
                console.error(err);
            });
    }, [api, startPolling]);

    const handleTextChange = (e: any) => {
        const val = e.target.value;
        setTextView(val);
        onCookieChange(val);
    };

    useEffect(() => {
        isMounted.current = true;
        getDouyuQRCode();
        return () => {
            isMounted.current = false;
            stopPolling();
        };
    }, [getDouyuQRCode, stopPolling]);

    // 验证卡片展示的身份：优先扫码结果，其次从当前 Cookie 文本实时解析
    const displayUser = loginUser || parseDouyuIdentity(textView);
    const scanSucceeded = loginStatus === 'success';

    // 登录态有效期：来自 acf_jwt_token 的 exp（斗鱼签发时固定 7 天）
    const expiryMs = parseDouyuCookieExpiry(textView);
    let expiryInfo: { text: string; color: string } | null = null;
    if (expiryMs !== null) {
        const remainDays = (expiryMs - Date.now()) / 86400000;
        const until = new Date(expiryMs).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
        if (remainDays <= 0) {
            expiryInfo = { text: `Cookie 已于 ${until} 过期，请重新扫码登录`, color: '#ff4d4f' };
        } else if (remainDays <= 2) {
            expiryInfo = { text: `Cookie 将于 ${until} 过期（剩余不足 2 天），建议尽快重新扫码`, color: '#fa8c16' };
        } else {
            expiryInfo = { text: `Cookie 有效期至 ${until}（剩余 ${Math.floor(remainDays)} 天）`, color: '#8c8c8c' };
        }
    }

    return (
        <div className="bili-login-container">
            <div className="bili-login-layout">
                {/* QR Section */}
                <div className="bili-qr-section">
                    <div className="section-label" style={{ borderLeft: 'none', paddingLeft: 0, justifyContent: 'center' }}>
                        斗鱼 App 扫码登录
                    </div>
                    <div className="qr-frame">
                        {loginStatus === 'loading' ? (
                            <div className="qr-overlay"><Spin tip="获取中..." /></div>
                        ) : (
                            <>
                                <img
                                    className="qr-image"
                                    src={`https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=${encodeURIComponent(qrCodeUrl)}`}
                                    alt="QR Code"
                                />
                                {(loginStatus === 'scanned' || loginStatus === 'success' || loginStatus === 'expired') && (
                                    <div className="qr-overlay">
                                        <div className="qr-status-icon">
                                            {loginStatus === 'scanned' && '📱'}
                                            {loginStatus === 'success' && '✅'}
                                            {loginStatus === 'expired' && '⌛'}
                                        </div>
                                        <div className="qr-status-text">
                                            {loginStatus === 'scanned' && '已扫描，待确认'}
                                            {loginStatus === 'success' && '登录成功'}
                                            {loginStatus === 'expired' && '二维码已失效'}
                                        </div>
                                        {loginStatus === 'expired' && (
                                            <div style={{ color: '#8c8c8c', fontSize: '12px', marginTop: 4 }}>
                                                点击下方按钮获取新二维码
                                            </div>
                                        )}
                                    </div>
                                )}
                            </>
                        )}
                    </div>
                    <div className="login-msg-text">{loginMsg}</div>
                    {loginStatus === 'expired' && (
                        <Button
                            className="verify-btn"
                            size="small"
                            type="primary"
                            style={{ marginTop: 10 }}
                            onClick={getDouyuQRCode}
                        >
                            刷新二维码
                        </Button>
                    )}
                </div>

                {/* Manual Section */}
                <div className="bili-manual-section">
                    <div className="section-label">
                        <span>手动管理 Cookie</span>
                    </div>
                    <TextArea
                        className="cookie-textarea"
                        placeholder="在此粘贴或修改 Cookie 字符串... (需包含 acf_uid、acf_auth 等登录字段)"
                        value={textView}
                        autoSize={{ minRows: 6, maxRows: 6 }}
                        onChange={handleTextChange}
                    />
                    <div className="manual-tip">提示：手动粘贴的 Cookie 需点击"保存并生效"落盘；扫码登录由后端自动保存，无需再点击</div>

                    {displayUser ? (
                        <div className="verification-card">
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                                <div style={{ display: 'flex', alignItems: 'center' }}>
                                    <Badge status="processing" color="#52c41a" />
                                    <span className="user-badge">{displayUser.nickname || '未知昵称'}</span>
                                    <span className="uid-text">UID: {displayUser.uid}</span>
                                </div>
                            </div>
                            <div style={{ fontSize: '13px', color: '#52c41a', marginTop: '8px', fontWeight: 500 }}>
                                {scanSucceeded
                                    ? '状态：扫码登录成功，Cookie 已自动保存并应用到运行中的房间'
                                    : '状态：检测到登录 Cookie，保存后可正常携带登录态拉流'}
                            </div>
                            {expiryInfo && (
                                <div style={{ fontSize: '13px', color: expiryInfo.color, marginTop: '4px', fontWeight: 500 }}>
                                    {expiryInfo.text}
                                </div>
                            )}
                        </div>
                    ) : (
                        <div className="verification-card pending">
                            <span>
                                {textView
                                    ? '当前 Cookie 未包含登录字段（acf_uid），可能仍处于未登录状态'
                                    : '请扫码或输入 Cookie 以开始'}
                            </span>
                        </div>
                    )}
                </div>
            </div>

            <Divider className="divider-text">如果您选择手动获取 Cookie</Divider>

            <Alert
                className="info-alert"
                showIcon
                message={<span style={{ fontWeight: 700, fontSize: '15px' }}>手动获取教程</span>}
                type="info"
                description={
                    <div style={{ fontSize: '14px' }}>
                        推荐使用扫码登录。匿名（无 Cookie）录制会被斗鱼 CDN 约 5 分钟切断一次流并不断重开分段，请确保 Cookie 处于登录状态，步骤如下：
                        <ul className="instruction-list">
                            <li>在浏览器打开 <b>斗鱼直播</b>（www.douyu.com）并保持登录状态。</li>
                            <li>按键盘上的 <b>F12</b> 或右键选择 <b>检查</b>，切换到 <b>网络 (Network)</b> 面板。</li>
                            <li>刷新页面，点开列表中的 <b>www.douyu.com</b> 第一个请求，在 <b>标头 (Headers)</b> 中找到 <b>Cookie</b> 一栏。</li>
                            <li><b>右键选中复制值</b>，并粘贴到上方输入框内（需包含 <b>acf_uid</b>、<b>acf_auth</b> 字段）。</li>
                            <li>点击 <b>保存并生效</b> 按钮完成落盘。</li>
                        </ul>
                    </div>
                }
            />
        </div>
    );
};

export default DouyuLoginPanel;
