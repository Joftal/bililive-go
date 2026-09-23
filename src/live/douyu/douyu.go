package douyu

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/hr3lxphr6j/requests"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/live/internal"
	"github.com/bililive-go/bililive-go/src/pkg/utils"

	"github.com/robertkrimen/otto"
	uuid "github.com/satori/go.uuid"
	"github.com/tidwall/gjson"
)

/*
From https://github.com/zhangn1985/ykdl

Thanks
*/
const (
	domain = "www.douyu.com"
	cnName = "斗鱼"

	liveInfoUrl = "https://www.douyu.com/betard"
	liveEncUrl  = "https://www.douyu.com/swf_api/homeH5Enc"
	liveAPIUrl  = "https://www.douyu.com/lapi/live/getH5Play"
)

func init() {
	live.Register(domain, new(builder))
}

type builder struct{}

func (b *builder) Build(url *url.URL) (live.Live, error) {
	return &Live{
		BaseLive: internal.NewBaseLive(url),
	}, nil
}

var (
	cryptoJS        []byte
	douyuRoomIDRegs = []string{
		`\$ROOM\.room_id\s*=\s*(\d+)`,
		`room_id\s*=\s*(\d+)`,
		`"room_id.?":(\d+)`,
		`data-onlineid=(\d+)`,
	}
	workflowReg = `function ub98484234\(.+?\Weval\((\w+)\);`
	jsDomTmpl   = template.Must(template.New("jsDom").Parse(`
		{{.DebugMessages}} = { {{.DecryptedCodes}}: []};
		if (!this.window) {window = {};}
		if (!this.document) {document = {};}
	`))
	jsPatchTmpl = template.Must(template.New("jsPatch").Parse(`
		{{.DebugMessages}}.{{.DecryptedCodes}}.push({{.Workflow}});
		var patchCode = function(workflow) {
			var testVari = /(\w+)=(\w+)\([\w\+]+\);.*?(\w+)="\w+";/.exec(workflow);
			if (testVari && testVari[1] == testVari[2]) {
				{{.Workflow}} += testVari[1] + "[" + testVari[3] + "] = function() {return true;};";
			}
		};
		patchCode({{.Workflow}});
		var subWorkflow = /(?:\w+=)?eval\((\w+)\)/.exec({{.Workflow}});
		if (subWorkflow) {
			var subPatch = (
				"{{.DebugMessages}}.{{.DecryptedCodes}}.push('sub workflow: ' + subWorkflow);" +
				"patchCode(subWorkflow);"
			).replace(/subWorkflow/g, subWorkflow[1]) + subWorkflow[0];
			{{.Workflow}} = {{.Workflow}}.replace(subWorkflow[0], subPatch);
		}
		eval({{.Workflow}});
	`))
	jsDebugTmpl = template.Must(template.New("jsDebug").Parse(`
		var {{.Ub98484234}} = ub98484234;
		ub98484234 = function(p1, p2, p3) {
			try {
				var resoult = {{.Ub98484234}}(p1, p2, p3);
				{{.DebugMessages}}.{{.Resoult}} = resoult;
			} catch(e) {
				{{.DebugMessages}}.{{.Resoult}} = e.message;
			}
			return {{.DebugMessages}};
		};
	`))
)

func render(tmpl *template.Template, data any) (string, error) {
	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (l *Live) loadCryptoJS() {
	var (
		resp *requests.Response
		body []byte
		err  error
	)
	cdnUrls := [...]string{"https://cdnjs.cloudflare.com/ajax/libs/crypto-js/3.1.9-1/crypto-js.min.js",
		"https://cdn.jsdelivr.net/npm/crypto-js@3.1.9-1/crypto-js.min.js",
		"https://cdn.staticfile.org/crypto-js/3.1.9-1/crypto-js.min.js",
		"https://cdn.bootcdn.net/ajax/libs/crypto-js/3.1.9-1/crypto-js.min.js"}

	for _, url := range cdnUrls {
		resp, err = l.RequestSession.Get(url)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		body, err = resp.Bytes()
		if err != nil {
			continue
		}
		cryptoJS = body
		return
	}
	panic(fmt.Errorf("failed to load CryptoJS, please check network"))
}

func (l *Live) getEngineWithCryptoJS() (*otto.Otto, error) {
	if cryptoJS == nil {
		l.loadCryptoJS()
	}
	engine := otto.New()
	if _, err := engine.Eval(cryptoJS); err != nil {
		return nil, err
	}
	return engine, nil
}

type Live struct {
	internal.BaseLive
	roomID string
	// did 在 Live 生命周期内固定，避免每次签名都换新设备号触发风控
	did string
}

// GetRoomID 返回已解析的数字房间号（由 fetchRoomID 解析）。
// 如果 URL 本身就是数字房间号，也会在 fetchRoomID 中缓存。
func (l *Live) GetRoomID() string {
	return l.roomID
}

// getCookieString 从 Options cookie jar 中读取斗鱼域名的 cookie 字符串（"k1=v1; k2=v2"）。
// 未配置 cookie 时返回空串，调用方据此决定是否附加 Cookie 头。
func (l *Live) getCookieString() string {
	if l.Options == nil || l.Options.Cookies == nil {
		return ""
	}
	cookies := l.Options.Cookies.Cookies(l.Url)
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// getDID 返回固定的设备号：优先复用用户 cookie 中的斗鱼设备号
// （浏览器实际使用 dy_did，登录态下 acf_did 与其一致），使签名 did
// 与登录 cookie 保持一致、更接近浏览器行为；否则生成一次并在该 Live 生命周期内复用。
func (l *Live) getDID() string {
	if l.did != "" {
		return l.did
	}
	if l.Options != nil && l.Options.Cookies != nil {
		for _, c := range l.Options.Cookies.Cookies(l.Url) {
			if (c.Name == "dy_did" || c.Name == "acf_did") && c.Value != "" {
				l.did = c.Value
				return l.did
			}
		}
	}
	l.did = strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")
	return l.did
}

// cookieRequestOption 返回带 Cookie 头的请求选项；无 cookie 时返回 nil，调用方需过滤。
func (l *Live) cookieRequestOption() requests.RequestOption {
	if ck := l.getCookieString(); ck != "" {
		return requests.Header("Cookie", ck)
	}
	return nil
}

func (l *Live) requestOpts(opts ...requests.RequestOption) []requests.RequestOption {
	if opt := l.cookieRequestOption(); opt != nil {
		opts = append(opts, opt)
	}
	return opts
}

func (l *Live) fetchRoomID() error {
	if l.roomID != "" {
		return nil
	}
	var body []byte
	resp, err := l.RequestSession.Get(l.Url.String(), l.requestOpts(live.CommonUserAgent)...)
	if err != nil {
		return errors.New("request failed. error: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("response code is " + strconv.Itoa(resp.StatusCode))
	}
	body, err = resp.Bytes()
	if err != nil {
		return errors.New("failed to read response body. error: " + err.Error())
	}
	for _, reg := range douyuRoomIDRegs {
		if str := utils.Match1(reg, string(body)); str != "" {
			l.roomID = str
			return nil
		}
	}
	if strings.Contains(string(body), "该房间目前没有开放") {
		errorMessage := "房间未开放"
		return errors.New(errorMessage)
	}
	if strings.Contains(string(body), "您观看的房间已被关闭，请选择其他直播进行观看哦！") {
		errorMessage := "房间被关闭"
		return errors.New(errorMessage)
	}
	showedBodyMaxLength := 20
	bodyLen := len(body)
	if bodyLen < 20 {
		showedBodyMaxLength = bodyLen
	}
	errorMessage := "unexcepted error. body: " + string(body[:showedBodyMaxLength])
	if bodyLen > showedBodyMaxLength {
		errorMessage += "... "
	}
	return errors.New(errorMessage)
}

func (l *Live) GetInfo() (info *live.Info, err error) {
	if err := l.fetchRoomID(); err != nil {
		if err.Error() == "房间未开放" {
			return nil, errors.New("room not exists, fetchRoomID failed")
		} else if err.Error() == "房间被关闭" {
			return &live.Info{
				Live:     l,
				HostName: "您观看的房间已被关闭",
				RoomName: "您观看的房间已被关闭",
				Status:   false,
			}, nil
		} else {
			return nil, err
		}

	}
	resp, err := l.RequestSession.Get(fmt.Sprintf("%s/%s", liveInfoUrl, l.roomID), l.requestOpts(live.CommonUserAgent)...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetInfo() failed, response code: %d", resp.StatusCode)
	}
	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}
	info = &live.Info{
		Live:         l,
		HostName:     gjson.GetBytes(body, "room.owner_name").String(),
		RoomName:     gjson.GetBytes(body, "room.room_name").String(),
		Status:       gjson.GetBytes(body, "room.show_status").Int() == 1 && gjson.GetBytes(body, "room.videoLoop").Int() == 0,
		CustomLiveId: "douyu/" + l.roomID,
	}
	return info, nil
}

func (l *Live) getSignParams() (map[string]string, error) {
	resp, err := l.RequestSession.Get(liveEncUrl, l.requestOpts(live.CommonUserAgent, requests.Query("rids", l.roomID))...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getSignParams() failed, response code: %d", resp.StatusCode)
	}
	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}

	jsEnc := gjson.GetBytes(body, "data.room"+l.roomID).String()

	workflow := utils.Match1(workflowReg, jsEnc)

	context := struct {
		DebugMessages  string
		DecryptedCodes string
		Resoult        string
		Ub98484234     string
		Workflow       string
	}{
		DebugMessages:  utils.GenRandomName(8),
		DecryptedCodes: utils.GenRandomName(8),
		Resoult:        utils.GenRandomName(8),
		Ub98484234:     utils.GenRandomName(8),
		Workflow:       workflow,
	}
	jsDom, err := render(jsDomTmpl, context)
	if err != nil {
		return nil, err
	}
	jsPatch, err := render(jsPatchTmpl, context)
	if err != nil {
		return nil, err
	}
	jsDebug, err := render(jsDebugTmpl, context)
	if err != nil {
		return nil, err
	}

	jsEnc = strings.ReplaceAll(jsEnc, fmt.Sprintf("eval(%s);", context.Workflow), jsPatch)
	engine, err := l.getEngineWithCryptoJS()
	if err != nil {
		return nil, err
	}
	if _, err := engine.Eval(jsDom); err != nil {
		return nil, err
	}
	if _, err := engine.Eval(jsEnc); err != nil {
		return nil, err
	}
	if _, err := engine.Eval(jsDebug); err != nil {
		return nil, err
	}
	did := l.getDID()
	ts := time.Now()
	res, err := engine.Call("ub98484234", nil, l.roomID, did, ts.Unix())
	if err != nil {
		return nil, err
	}
	values := map[string]string{
		"cdn":  "",
		"iar":  "0",
		"ive":  "0",
		"rate": "0",
	}
	resoult, err := res.Object().Get(context.Resoult)
	if err != nil {
		return nil, err
	}
	for _, entry := range strings.Split(resoult.String(), "&") {
		if entry == "" {
			continue
		}
		strs := strings.SplitN(entry, "=", 2)
		values[strs[0]] = strs[1]
	}
	return values, nil
}

func (l *Live) GetStreamInfos() (infos []*live.StreamUrlInfo, err error) {
	if err := l.fetchRoomID(); err != nil {
		return nil, err
	}
	params, err := l.getSignParams()
	if err != nil {
		return nil, err
	}
	resp, err := l.RequestSession.Post(
		fmt.Sprintf("%s/%s", liveAPIUrl, l.roomID),
		l.requestOpts(
			requests.Form(params),
			requests.Header("origin", "https://www.douyu.com"),
			requests.Referer(l.GetRawUrl()),
			live.CommonUserAgent,
		)...,
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, live.ErrInternalError
	}
	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}
	if errorInt := gjson.GetBytes(body, "error").Int(); errorInt != 0 {
		return nil, fmt.Errorf("GetStreamInfos() failed, error: %d", errorInt)
	}
	us, err := utils.GenUrls(
		fmt.Sprintf("%s/%s",
			gjson.GetBytes(body, "data.rtmp_url").String(),
			gjson.GetBytes(body, "data.rtmp_live").String(),
		),
	)
	if err != nil {
		return nil, err
	}

	// 下载直播流时同样携带 Cookie/Referer：带登录态 cookie 时斗鱼 CDN 不再
	// 按约 5 分钟周期性切断匿名流，从而避免一次直播被切成大量碎片分段。
	headers := map[string]string{
		"Referer": l.GetRawUrl(),
	}
	if ck := l.getCookieString(); ck != "" {
		headers["Cookie"] = ck
	}

	for _, u := range us {
		format := "flv"
		if strings.Contains(u.Path, "m3u8") {
			format = "hls"
		}
		infos = append(infos, &live.StreamUrlInfo{
			Url:                  u,
			Name:                 "原画",
			Quality:              "原画",
			Format:               format,
			HeadersForDownloader: headers,
			AttributesForStreamSelect: map[string]string{
				"画质": "原画",
			},
		})
	}
	return infos, nil
}

// Deprecated: 使用 GetStreamInfos 代替
func (l *Live) GetStreamUrls() (us []*url.URL, err error) {
	infos, err := l.GetStreamInfos()
	if err != nil {
		return nil, err
	}
	us = make([]*url.URL, 0, len(infos))
	for _, info := range infos {
		us = append(us, info.Url)
	}
	return us, nil
}

func (l *Live) GetPlatformCNName() string {
	return cnName
}
