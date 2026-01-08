package bilibili

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/hr3lxphr6j/requests"
	"github.com/tidwall/gjson"

	"github.com/bililive-go/bililive-go/src/live"
	"github.com/bililive-go/bililive-go/src/live/internal"
	"github.com/bililive-go/bililive-go/src/pkg/utils"
)

const (
	domain = "live.bilibili.com"
	cnName = "哔哩哔哩"

	roomInitUrl     = "https://api.live.bilibili.com/room/v1/Room/room_init"
	roomApiUrl      = "https://api.live.bilibili.com/room/v1/Room/get_info"
	userApiUrl      = "https://api.live.bilibili.com/live_user/v1/UserInfo/get_anchor_in_room"
	liveApiUrlv2    = "https://api.live.bilibili.com/xlive/web-room/v2/index/getRoomPlayInfo"
	appLiveApiUrlv2 = "https://api.live.bilibili.com/xlive/app-room/v2/index/getRoomPlayInfo"
	biliAppAgent    = "Bilibili Freedoooooom/MarkII BiliDroid/5.49.0 os/android model/MuMu mobi_app/android build/5490400 channel/dw090 innerVer/5490400 osVer/6.0.1 network/2"
	biliWebAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_12_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/59.0.3071.115 Safari/537.36"
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

type Live struct {
	internal.BaseLive
	realID string
}

func (l *Live) parseRealId() error {
	paths := strings.Split(l.Url.Path, "/")
	if len(paths) < 2 {
		return live.ErrRoomUrlIncorrect
	}
	cookies := l.Options.Cookies.Cookies(l.Url)
	cookieKVs := make(map[string]string)
	for _, item := range cookies {
		cookieKVs[item.Name] = item.Value
	}
	resp, err := l.RequestSession.Get(roomInitUrl, live.CommonUserAgent, requests.Query("id", paths[1]), requests.Cookies(cookieKVs))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return live.ErrRoomNotExist
	}
	body, err := resp.Bytes()
	if err != nil || gjson.GetBytes(body, "code").Int() != 0 {
		return live.ErrRoomNotExist
	}
	l.realID = gjson.GetBytes(body, "data.room_id").String()
	return nil
}

func (l *Live) GetInfo() (info *live.Info, err error) {
	// Parse the short id from URL to full id
	if l.realID == "" {
		if err := l.parseRealId(); err != nil {
			return nil, err
		}
	}
	cookies := l.Options.Cookies.Cookies(l.Url)
	cookieKVs := make(map[string]string)
	for _, item := range cookies {
		cookieKVs[item.Name] = item.Value
	}
	resp, err := l.RequestSession.Get(
		roomApiUrl,
		live.CommonUserAgent,
		requests.Query("room_id", l.realID),
		requests.Query("from", "room"),
		requests.Cookies(cookieKVs),
	)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, live.ErrRoomNotExist
	}
	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "code").Int() != 0 {
		return nil, live.ErrRoomNotExist
	}

	info = &live.Info{
		Live:      l,
		RoomName:  gjson.GetBytes(body, "data.title").String(),
		Status:    gjson.GetBytes(body, "data.live_status").Int() == 1,
		AudioOnly: l.Options.AudioOnly,
	}

	resp, err = l.RequestSession.Get(userApiUrl, live.CommonUserAgent, requests.Query("roomid", l.realID))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, live.ErrInternalError
	}
	body, err = resp.Bytes()
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(body, "code").Int() != 0 {
		return nil, live.ErrInternalError
	}

	info.HostName = gjson.GetBytes(body, "data.info.uname").String()
	return info, nil
}

func (l *Live) GetStreamInfos() (infos []*live.StreamUrlInfo, err error) {
	if l.realID == "" {
		if err := l.parseRealId(); err != nil {
			return nil, err
		}
	}
	cookies := l.Options.Cookies.Cookies(l.Url)
	cookieKVs := make(map[string]string)
	for _, item := range cookies {
		cookieKVs[item.Name] = item.Value
	}
	qn := l.Options.Quality
	if qn == 0 {
		qn = 30000 // 默认尝试请求杜比视界，API 会根据权限自动降级
	}
	apiUrl := liveApiUrlv2
	query := fmt.Sprintf("?room_id=%s&protocol=0,1&format=0,1,2&codec=0,1&qn=%d&platform=web&ptype=8&dolby=5&panorama=1", l.realID, qn)
	agent := live.CommonUserAgent
	// for audio only use android api
	if l.Options.AudioOnly {
		params := map[string]string{"appkey": "iVGUTjsxvpLeuDCf",
			"build":       "6310200",
			"codec":       "0,1",
			"device":      "android",
			"device_name": "ONEPLUS",
			"dolby":       "5",
			"format":      "0,2",
			"only_audio":  "1",
			"platform":    "android",
			"protocol":    "0,1",
			"room_id":     l.realID,
			"qn":          strconv.Itoa(l.Options.Quality),
		}
		values := url.Values{}
		for key, value := range params {
			values.Add(key, value)
		}
		query = "?" + values.Encode()
		apiUrl = appLiveApiUrlv2
		agent = requests.UserAgent(biliAppAgent)
	}
	resp, err := l.RequestSession.Get(apiUrl+query, agent, requests.Cookies(cookieKVs))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, live.ErrRoomNotExist
	}
	body, err := resp.Bytes()
	if err != nil {
		return nil, err
	}
	urlStrings := make([]string, 0, 4)
	addr := ""

	// 选取最高画质的流地址
	maxQN := int64(0)
	bestStreamPath := "data.playurl_info.playurl.stream.0.format.0.codec.0"

	gjson.GetBytes(body, "data.playurl_info.playurl.stream").ForEach(func(sIdx, stream gjson.Result) bool {
		protocolName := stream.Get("protocol_name").String()
		stream.Get("format").ForEach(func(fIdx, format gjson.Result) bool {
			format.Get("codec").ForEach(func(cIdx, codec gjson.Result) bool {
				currentQN := codec.Get("current_qn").Int()
				// 优先级策略：
				// 1. 谁的 QN 大选谁
				// 2. 如果 QN 相同，优先选 http_hls (HLS)
				if currentQN > maxQN || (currentQN == maxQN && protocolName == "http_hls") {
					maxQN = currentQN
					bestStreamPath = fmt.Sprintf("data.playurl_info.playurl.stream.%d.format.%d.codec.%d", sIdx.Int(), fIdx.Int(), cIdx.Int())
				}
				return true
			})
			return true
		})
		return true
	})
	addr = bestStreamPath

	baseURL := gjson.GetBytes(body, addr+".base_url").String()
	gjson.GetBytes(body, addr+".url_info").ForEach(func(_, value gjson.Result) bool {
		hosts := gjson.Get(value.String(), "host").String()
		queries := gjson.Get(value.String(), "extra").String()
		urlStrings = append(urlStrings, hosts+baseURL+queries)
		return true
	})

	ext := ".flv"
	addrParts := strings.Split(addr, ".")
	if len(addrParts) >= 7 {
		formatPath := strings.Join(addrParts[:7], ".")
		formatName := gjson.GetBytes(body, formatPath+".format_name").String()
		switch formatName {
		case "ts":
			ext = ".ts"
		case "fmp4":
			ext = ".mp4"
		}
	}

	urls, err := utils.GenUrls(urlStrings...)
	if err != nil {
		return nil, err
	}
	for _, u := range urls {
		infos = append(infos, &live.StreamUrlInfo{
			Url:                  u,
			Extension:            ext,
			HeadersForDownloader: l.getHeadersForDownloader(),
		})
	}
	return
}

func (l *Live) GetPlatformCNName() string {
	return cnName
}

func (l *Live) getHeadersForDownloader() map[string]string {
	agent := biliWebAgent
	referer := l.GetRawUrl()
	if l.Options.AudioOnly {
		agent = biliAppAgent
		referer = ""
	}
	return map[string]string{
		"User-Agent": agent,
		"Referer":    referer,
	}
}
