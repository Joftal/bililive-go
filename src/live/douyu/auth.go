// Package douyu 中的扫码登录实现（auth.go）
// 协议来源：斗鱼官方登录页（passport.douyu.com/index/login）实测抓包：
// 1. POST /scan/generateCode        -> {error:0, data:{expire:300, url:<二维码内容>, code:<32位hex>}}
// 2. GET  /japi/scan/auth?time&code -> error: -2 未扫码 / 1 已扫码待确认 / 0 确认成功(data.url) / -1 二维码失效
// 3. 确认成功后 GET data.url（JSONP），登录 cookie 由该响应 Set-Cookie 下发
package douyu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	qrGenerateUrl  = "https://passport.douyu.com/scan/generateCode"
	qrPollUrl      = "https://passport.douyu.com/japi/scan/auth"
	qrLoginReferer = "https://passport.douyu.com/index/login"

	// CookieHost 是斗鱼登录 cookie 在 configs.Cookies 中使用的 host key
	CookieHost = "www.douyu.com"
)

// douyuWebUAString 与录制侧一致的浏览器 UA，避免签名/登录接口风控
const douyuWebUAString = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// douyuHTTPClient 登录相关请求专用 client：不自动跟随重定向，
// 由 exchangeLoginCookie 手动逐跳处理以保留中间跳的 Set-Cookie。
var douyuHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var qrCodeReg = regexp.MustCompile(`^[0-9a-f]{32}$`)

// QRLoginState 扫码登录的轮询状态
type QRLoginState string

const (
	QRStateWaiting  QRLoginState = "waiting"  // 客户端还未扫码
	QRStateScanned  QRLoginState = "scanned"  // 已扫码，等待手机端确认
	QRStateSuccess  QRLoginState = "success"  // 确认成功，已换取登录 cookie
	QRStateExpired  QRLoginState = "expired"  // 二维码失效/被取消
	QRStateRejected QRLoginState = "rejected" // 手机端拒绝授权
)

// QRLoginSession 一次扫码登录会话
type QRLoginSession struct {
	Code   string `json:"code"`   // 轮询用的会话 code
	QRUrl  string `json:"qr_url"` // 二维码内容（斗鱼中间页链接）
	Expire int    `json:"expire"` // 有效期（秒）
}

// QRPollResult 轮询结果；成功时附带登录 cookie 与用户信息
type QRPollResult struct {
	State    QRLoginState `json:"state"`
	Nickname string       `json:"nickname,omitempty"`
	UserID   string       `json:"uid,omitempty"`
	Cookie   string       `json:"cookie,omitempty"` // 仅成功时有值
	Msg      string       `json:"msg,omitempty"`
}

// GenerateLoginQRCode 向后端代理请求斗鱼登录二维码。
// ctx 用于在 HTTP 客户端断开或超时后中止上游请求。
func GenerateLoginQRCode(ctx context.Context) (*QRLoginSession, error) {
	form := url.Values{"client_id": {"1"}, "isMultiAccount": {"0"}}
	req, err := http.NewRequestWithContext(ctx, "POST", qrGenerateUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", douyuWebUAString)
	req.Header.Set("Referer", qrLoginReferer)
	req.Header.Set("Origin", "https://passport.douyu.com")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := doDouyuRequest(req)
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "error").Int() != 0 {
		return nil, fmt.Errorf("斗鱼返回错误: %s", strings.TrimSpace(gjson.GetBytes(body, "data").String()))
	}
	code := gjson.GetBytes(body, "data.code").String()
	qrUrl := gjson.GetBytes(body, "data.url").String()
	if !qrCodeReg.MatchString(code) || qrUrl == "" {
		return nil, fmt.Errorf("斗鱼二维码响应异常, body: %s", truncateForLog(body))
	}
	return &QRLoginSession{
		Code:   code,
		QRUrl:  qrUrl,
		Expire: int(gjson.GetBytes(body, "data.expire").Int()),
	}, nil
}

// PollLoginQRCode 轮询一次扫码状态；确认成功时自动完成 cookie 换取
func PollLoginQRCode(ctx context.Context, code string) (*QRPollResult, error) {
	if !qrCodeReg.MatchString(code) {
		return nil, errors.New("非法的二维码 code")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s?time=%d&code=%s", qrPollUrl, time.Now().UnixMilli(), code), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", douyuWebUAString)
	req.Header.Set("Referer", qrLoginReferer)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	body, err := doDouyuRequest(req)
	if err != nil {
		return nil, err
	}
	errVal := gjson.GetBytes(body, "error").Int()
	msg := gjson.GetBytes(body, "msg").String()
	switch errVal {
	case 0:
		exchangeUrl := gjson.GetBytes(body, "data.url").String()
		if exchangeUrl == "" {
			return nil, fmt.Errorf("斗鱼登录成功响应缺少回调 URL, body: %s", truncateForLog(body))
		}
		cookie, infoBody, trace, err := exchangeLoginCookie(ctx, exchangeUrl)
		if err != nil {
			return nil, err
		}
		if cookie == "" {
			return nil, fmt.Errorf("斗鱼登录确认成功但未获得 Set-Cookie，接口可能已变更; %s", trace)
		}
		nickname := pickFirstString(infoBody, "nickname", "user_nickname", "uname", "data.nickname")
		userID := pickFirstString(infoBody, "uid", "user_id", "id", "data.uid")
		// JSONP 回调结构可能变化，cookie 中的 acf_uid/acf_nickname 是更稳定的来源
		if userID == "" {
			userID = cookieValue(cookie, "acf_uid")
		}
		if nickname == "" {
			if enc := cookieValue(cookie, "acf_nickname"); enc != "" {
				if dec, e := url.QueryUnescape(enc); e == nil {
					nickname = dec
				}
			}
		}
		return &QRPollResult{
			State:    QRStateSuccess,
			Nickname: nickname,
			UserID:   userID,
			Cookie:   cookie,
		}, nil
	case 1:
		return &QRPollResult{State: QRStateScanned, Msg: msg}, nil
	case -2:
		return &QRPollResult{State: QRStateWaiting, Msg: msg}, nil
	case -1:
		return &QRPollResult{State: QRStateExpired, Msg: msg}, nil
	default:
		return &QRPollResult{State: QRStateRejected, Msg: msg}, nil
	}
}

// doDouyuRequest 执行一次斗鱼上游请求并返回响应体（限制 1MB）
func doDouyuRequest(req *http.Request) ([]byte, error) {
	resp, err := douyuHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// exchangeLoginCookie 请求登录成功回调 URL（JSONP），从 Set-Cookie 收集登录态。
// 注意：斗鱼可能在 302 跳转链的中间跳下发登录 cookie，
// 而 http.Client 自动跟随重定向时只能读到最后一跳的 Set-Cookie，
// 因此这里手动逐跳请求并累积所有 Set-Cookie（每跳都校验 douyu 域名白名单）。
func exchangeLoginCookie(ctx context.Context, rawUrl string) (cookie string, jsonpBody string, trace string, err error) {
	if err = checkDouyuURL(rawUrl); err != nil {
		return "", "", "", err
	}
	seen := make(map[string]bool) // 已收集的 k=v，兼做去重
	var hops []string             // 跳转链诊断信息（仅记录 cookie 名，不记录值）
	cur := rawUrl
	var finalBody string
	for hop := 0; hop < 8; hop++ {
		req, err := http.NewRequestWithContext(ctx, "GET", cur, nil)
		if err != nil {
			return "", "", "", err
		}
		req.Header.Set("User-Agent", douyuWebUAString)
		req.Header.Set("Referer", qrLoginReferer)
		resp, err := douyuHTTPClient.Do(req)
		if err != nil {
			return "", "", "", err
		}
		names := make([]string, 0, len(resp.Cookies()))
		for _, c := range resp.Cookies() {
			seen[c.Name+"="+c.Value] = true
			names = append(names, c.Name)
		}
		hops = append(hops, fmt.Sprintf("%s -> %d (Set-Cookie: %v)", cur, resp.StatusCode, names))
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc, _ := resp.Location()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if loc == nil {
				finalBody = string(body)
				break
			}
			if err = checkDouyuURL(loc.String()); err != nil {
				return "", "", strings.Join(hops, "; "), err
			}
			cur = loc.String()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return "", "", strings.Join(hops, "; "), err
		}
		finalBody = string(body)
		break
	}
	parts := make([]string, 0, len(seen))
	for kv := range seen {
		parts = append(parts, kv)
	}
	trace = strings.Join(hops, "; ") + "; 终点响应体: " + truncateForLog([]byte(finalBody))
	return strings.Join(parts, "; "), stripJSONP(finalBody), trace, nil
}

// checkDouyuURL 只允许斗鱼官方域名，防止被诱导请求任意地址（SSRF）
func checkDouyuURL(rawUrl string) error {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return fmt.Errorf("回调 URL 解析失败: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("拒绝非 HTTPS 回调: %s", u.Scheme)
	}
	if u.Host != "douyu.com" && !strings.HasSuffix(u.Host, ".douyu.com") {
		return fmt.Errorf("拒绝请求非斗鱼域名回调: %s", u.Host)
	}
	return nil
}

// stripJSONP 从 JSONP 响应中提取 JSON 主体（appClient_json_callback({...});）
func stripJSONP(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}

func pickFirstString(jsonBody string, paths ...string) string {
	for _, p := range paths {
		if v := gjson.Get(jsonBody, p).String(); v != "" {
			return v
		}
	}
	return ""
}

// cookieValue 从 "k1=v1; k2=v2" 形式的 cookie 串中取出指定字段的原始值
func cookieValue(cookieStr, name string) string {
	for _, item := range strings.Split(cookieStr, ";") {
		item = strings.TrimSpace(item)
		if v, ok := strings.CutPrefix(item, name+"="); ok {
			return v
		}
	}
	return ""
}

func truncateForLog(body []byte) string {
	const max = 200
	if len(body) > max {
		return string(body[:max]) + "..."
	}
	return string(body)
}
