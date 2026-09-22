package btoolssrv

// 抖音房间信息 API 移植自 biliLive-tools DouYinRecorder/lib/douyin_api.js。
// 四条通道：web(enter API + a_bogus 签名) / webHTML(直播页HTML) /
// userHTML(用户主页HTML) / mobile(amemv reflow)。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	dyUARequester = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	dyUAHTML      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36 Edg/133.0.0.0"
)

// proxyDisabledClient 对应 axios 的 proxy:false：绝不走环境变量代理。
var dyClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
	},
}

func nowMillis() int64 { return time.Now().UnixMilli() }

// ---------- HTTP 基础 ----------

type dyResp struct {
	statusCode int
	body       string
	setCookies []string
	finalURL   string
}

func dyGet(rawURL string, headers map[string]string) (*dyResp, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := dyClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &dyResp{
		statusCode: resp.StatusCode,
		body:       string(body),
		setCookies: resp.Header.Values("Set-Cookie"),
		finalURL:   resp.Request.URL.String(),
	}, nil
}

// ---------- cookie / nonce 缓存 ----------

type cookieCacheEntry struct {
	startTimestamp int64
	cookies        string
}

var (
	dyCookieMu    sync.Mutex
	dyCookieCache *cookieCacheEntry
	dyNonceMu     sync.Mutex
	dyNonceCache  *cookieCacheEntry // 复用结构：存 nonce
)

// getCookie 对应 douyin_api.js 的 getCookie：访问直播首页拿 ttwid，缓存 6 小时。
func getCookie() (string, error) {
	dyCookieMu.Lock()
	defer dyCookieMu.Unlock()
	now := nowMillis()
	if dyCookieCache != nil && now-dyCookieCache.startTimestamp < 6*60*60*1000 {
		return dyCookieCache.cookies, nil
	}
	res, err := dyGet("https://live.douyin.com/", map[string]string{"User-Agent": dyUARequester})
	if err != nil {
		return "", err
	}
	if len(res.setCookies) == 0 {
		return "", errors.New("No cookie in response")
	}
	parts := make([]string, 0, len(res.setCookies))
	for _, c := range res.setCookies {
		parts = append(parts, strings.Split(c, ";")[0])
	}
	cookies := strings.Join(parts, "; ")
	if !strings.Contains(cookies, "ttwid") {
		// 不含 ttwid 时若旧缓存存在则延长 1 小时复用
		if dyCookieCache != nil && dyCookieCache.cookies != "" {
			dyCookieCache.startTimestamp += 60 * 60 * 1000
			return dyCookieCache.cookies, nil
		}
	}
	dyCookieCache = &cookieCacheEntry{startTimestamp: now, cookies: cookies}
	return cookies, nil
}

// getNonce 对应 douyin_api.js 的 getNonce：从页面响应 Set-Cookie 里取 __ac_nonce，缓存 6 小时。
func getNonce(pageURL string) (string, error) {
	dyNonceMu.Lock()
	defer dyNonceMu.Unlock()
	now := nowMillis()
	if dyNonceCache != nil && now-dyNonceCache.startTimestamp < 6*60*60*1000 {
		return dyNonceCache.cookies, nil
	}
	res, err := dyGet(pageURL, map[string]string{"User-Agent": dyUARequester})
	if err != nil {
		return "", err
	}
	if len(res.setCookies) == 0 {
		return "", errors.New("No cookie in response")
	}
	for _, c := range res.setCookies {
		kv := strings.Split(c, ";")[0]
		if strings.HasPrefix(kv, "__ac_nonce=") {
			nonce := strings.TrimPrefix(kv, "__ac_nonce=")
			dyNonceCache = &cookieCacheEntry{startTimestamp: now, cookies: nonce}
			return nonce, nil
		}
	}
	return "", nil
}

func generateNonce() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]rune, 21)
	for i := range b {
		b[i] = rune(chars[rand.Intn(len(chars))])
	}
	return string(b)
}

// ---------- 房间信息结构 ----------

type qualityOption struct {
	Name      string `json:"name"`
	SdkKey    string `json:"sdk_key"`
	VBitRate  int    `json:"v_bit_rate"`
}

type streamEntry struct {
	Quality string `json:"quality"`
	Name    string `json:"name"`
	Flv     string `json:"flv"`
	Hls     string `json:"hls"`
}

type pullData struct {
	Options struct {
		Qualities []qualityOption `json:"qualities"`
	} `json:"options"`
	StreamData any `json:"stream_data"` // string 或 object
}

type roomStreamURL struct {
	PullDatas map[string]pullData `json:"pull_datas"`
	LiveCoreSDKData struct {
		PullData struct {
			Options struct {
				Qualities []qualityOption `json:"qualities"`
			} `json:"options"`
			StreamData any `json:"stream_data"`
		} `json:"pull_data"`
	} `json:"live_core_sdk_data"`
}

type roomData struct {
	Title     string
	Cover     string
	IDStr     string
	StreamURL *roomStreamURL
}

type roomInfoResult struct {
	Living   bool
	Nickname string
	SecUID   string
	Avatar   string
	API      string
	Room     *roomData
}

// ---------- 通道 1：web（enter API + a_bogus） ----------

func getRoomInfoByWeb(webRoomID string, auth string) (*roomInfoResult, error) {
	cookies := auth
	if cookies == "" {
		// enter API 需要 ttwid，由首页请求自动获取
		c, err := getCookie()
		if err != nil {
			return nil, err
		}
		cookies = c
	}
	// 参数顺序必须与 JS 的 URLSearchParams 一致
	params := "aid=6383" +
		"&live_id=1" +
		"&device_platform=web" +
		"&language=zh-CN" +
		"&enter_from=web_live" +
		"&cookie_enabled=true" +
		"&screen_width=1920" +
		"&screen_height=1080" +
		"&browser_language=zh-CN" +
		"&browser_platform=MacIntel" +
		"&browser_name=Chrome" +
		"&browser_version=108.0.0.0" +
		"&web_rid=" + url.QueryEscape(webRoomID) +
		"&Room-Enter-User-Login-Ab=0" +
		"&is_need_double_stream=false"
	ab := newAbogus()
	query := ab.generateAbogus(params, "")
	finalUA := ab.userAgent
	res, err := dyGet("https://live.douyin.com/webcast/room/web/enter/?"+query, map[string]string{
		"cookie":     cookies,
		"User-Agent": finalUA,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		StatusCode int `json:"status_code"`
		Data       struct {
			RoomStatus int `json:"room_status"`
			User       struct {
				Nickname string `json:"nickname"`
				SecUID   string `json:"sec_uid"`
				Avatar   struct {
					URLList []string `json:"url_list"`
				} `json:"avatar_thumb"`
			} `json:"user"`
			Data []struct {
				Title     string          `json:"title"`
				IDStr     string          `json:"id_str"`
				Cover     json.RawMessage `json:"cover"`
				StreamURL *roomStreamURL  `json:"stream_url"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.body), &parsed); err != nil {
		return nil, fmt.Errorf("enter API 响应解析失败: %w", err)
	}
	if parsed.StatusCode != 0 {
		return nil, fmt.Errorf("Unexpected resp, code %d, id %s", parsed.StatusCode, webRoomID)
	}
	var room *roomData
	if len(parsed.Data.Data) > 0 {
		r := parsed.Data.Data[0]
		cover := jsonGetString(r.Cover, "url_list", 0)
		room = &roomData{
			Title:     r.Title,
			Cover:     cover,
			IDStr:     r.IDStr,
			StreamURL: r.StreamURL,
		}
	}
	avatar := ""
	if len(parsed.Data.User.Avatar.URLList) > 0 {
		avatar = parsed.Data.User.Avatar.URLList[0]
	}
	return &roomInfoResult{
		Living:   parsed.Data.RoomStatus == 0,
		Nickname: parsed.Data.User.Nickname,
		Avatar:   avatar,
		SecUID:   parsed.Data.User.SecUID,
		API:      "web",
		Room:     room,
	}, nil
}

// jsonGetString 从 {"url_list":[...]} 形态的 RawMessage 中取第 i 个元素。
func jsonGetString(raw json.RawMessage, listKey string, idx int) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	lst, ok := m[listKey].([]any)
	if !ok || idx >= len(lst) {
		return ""
	}
	s, _ := lst[idx].(string)
	return s
}

// ---------- 通道 2：webHTML（直播间页 HTML） ----------

var (
	dyStateRegex = regexp.MustCompile(`(\{\\"state\\":.*?)\]\\n"\]\)`)
	dyUserRegex  = regexp.MustCompile(`(\{\\"user\\":.*?)\]\\n"\]\)`)
	dyVerifyRe   = regexp.MustCompile(`验证码`)
	dyEscapedQuoteRe = regexp.MustCompile(`\\"`)
	dyNullFieldRe    = regexp.MustCompile(`"\$\w+"`)
)

func unescapeDouyinJSON(s string) string {
	// JS 中连续 replace 两次 \\" -> "（双重转义）
	s = dyEscapedQuoteRe.ReplaceAllString(s, `"`)
	s = dyEscapedQuoteRe.ReplaceAllString(s, `"`)
	s = dyNullFieldRe.ReplaceAllString(s, "null")
	return s
}

func getRoomInfoByHtml(webRoomID string, auth string) (*roomInfoResult, error) {
	pageURL := "https://live.douyin.com/" + webRoomID
	nonce := generateNonce()
	cookies := auth
	if cookies == "" {
		signed := GetACSignature(time.Now().Unix(), pageURL, nonce, dyUAHTML)
		cookies = "__ac_nonce=" + nonce + "; __ac_signature=" + signed + "; __ac_referer=__ac_blank"
	}
	res, err := dyGet(pageURL, map[string]string{
		"User-Agent": dyUAHTML,
		"cookie":     cookies,
	})
	if err != nil {
		return nil, err
	}
	match := dyStateRegex.FindStringSubmatch(res.body)
	if match == nil {
		return nil, errors.New("No match found in HTML")
	}
	var parsed struct {
		State struct {
			RoomStore struct {
				RoomInfo struct {
					Room struct {
						Status    int    `json:"status"`
						Title     string `json:"title"`
						IDStr     string `json:"id_str"`
						Cover     struct {
							URLList []string `json:"url_list"`
						} `json:"cover"`
						StreamURL *roomStreamURL `json:"stream_url"`
					} `json:"room"`
					Anchor struct {
						Nickname string `json:"nickname"`
						SecUID   string `json:"sec_uid"`
						Avatar   struct {
							URLList []string `json:"url_list"`
						} `json:"avatar_thumb"`
					} `json:"anchor"`
				} `json:"roomInfo"`
			} `json:"roomStore"`
			StreamStore struct {
				StreamData struct {
					H264StreamData struct {
						Options struct {
							Qualities []qualityOption `json:"qualities"`
						} `json:"options"`
						Stream any `json:"stream"`
					} `json:"H264_streamData"`
				} `json:"streamData"`
			} `json:"streamStore"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(unescapeDouyinJSON(match[1])), &parsed); err != nil {
		return nil, fmt.Errorf("Failed to parse JSON: %w", err)
	}
	ri := parsed.State.RoomStore.RoomInfo
	cover := ""
	if len(ri.Room.Cover.URLList) > 0 {
		cover = ri.Room.Cover.URLList[0]
	}
	avatar := ""
	if len(ri.Anchor.Avatar.URLList) > 0 {
		avatar = ri.Anchor.Avatar.URLList[0]
	}
	return &roomInfoResult{
		Living:   ri.Room.Status == 2,
		Nickname: ri.Anchor.Nickname,
		SecUID:   ri.Anchor.SecUID,
		Avatar:   avatar,
		API:      "userHTML", // JS 中该通道返回的 api 字段名即 "userHTML"（见原实现）
		Room: &roomData{
			Title: ri.Room.Title,
			Cover: cover,
			IDStr: ri.Room.IDStr,
			StreamURL: &roomStreamURL{
				PullDatas: ri.Room.StreamURL.PullDatas,
			},
		},
	}, nil
}

// ---------- 通道 3：userHTML（用户主页 HTML） ----------

func getRoomInfoByUserWeb(secUserID string, auth string) (*roomInfoResult, error) {
	pageURL := "https://www.douyin.com/user/" + secUserID
	nonce := "068ea1c0100bb2c06590f"
	if n, err := getNonce(pageURL); err == nil && n != "" {
		nonce = n
	}
	cookies := auth
	if cookies == "" {
		signed := GetACSignature(time.Now().Unix(), pageURL, nonce, dyUAHTML)
		cookies = "__ac_nonce=" + nonce + "; __ac_signature=" + signed + "; __ac_referer=__ac_blank"
	}
	res, err := dyGet(pageURL, map[string]string{
		"User-Agent": dyUAHTML,
		"cookie":     cookies,
	})
	if err != nil {
		return nil, err
	}
	if strings.Contains(res.body, "验证码") {
		return nil, errors.New("需要验证码，请在浏览器中打开链接获取" + pageURL)
	}
	if !strings.Contains(res.body, "直播中") {
		return &roomInfoResult{Living: false, API: "webHTML", Room: &roomData{}}, nil
	}
	match := dyUserRegex.FindStringSubmatch(res.body)
	if match == nil {
		return nil, errors.New("No match found in HTML")
	}
	var parsed struct {
		User struct {
			User struct {
				RoomData struct {
					Status int `json:"status"`
				} `json:"roomData"`
				Nickname string `json:"nickname"`
				SecUID   string `json:"secUid"`
				Avatar   string `json:"avatar"`
				RoomIDStr string `json:"roomIdStr"`
			} `json:"user"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(unescapeDouyinJSON(match[1])), &parsed); err != nil {
		return nil, fmt.Errorf("Failed to parse JSON: %w", err)
	}
	u := parsed.User.User
	return &roomInfoResult{
		Living:   u.RoomData.Status == 2,
		Nickname: u.Nickname,
		SecUID:   u.SecUID,
		Avatar:   u.Avatar,
		API:      "webHTML",
		Room: &roomData{
			IDStr: u.RoomIDStr,
		},
	}, nil
}

// ---------- 通道 4：mobile（amemv reflow） ----------

func getRoomInfoByMobile(secUserID string, auth string) (*roomInfoResult, error) {
	if secUserID == "" {
		return nil, errors.New("Mobile API need secUserId, please set uid field")
	}
	params := url.Values{}
	params.Set("app_id", "1128")
	params.Set("live_id", "1")
	params.Set("verifyFp", "")
	params.Set("room_id", "2")
	params.Set("type_id", "0")
	params.Set("sec_user_id", secUserID)
	reqURL := "https://webcast.amemv.com/webcast/room/reflow/info/?" + params.Encode()
	res, err := dyGet(reqURL, map[string]string{
		"User-Agent": dyUARequester,
		"cookie":     auth,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data struct {
			Room *struct {
				Status int    `json:"status"`
				Title  string `json:"title"`
				IDStr  string `json:"id_str"`
				Cover  struct {
					URLList []string `json:"url_list"`
				} `json:"cover"`
				Owner struct {
					Nickname string `json:"nickname"`
					SecUID   string `json:"sec_uid"`
					Avatar   struct {
						URLList []string `json:"url_list"`
					} `json:"avatar_thumb"`
				} `json:"owner"`
				StreamURL *roomStreamURL `json:"stream_url"`
			} `json:"room"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.body), &parsed); err != nil {
		return nil, fmt.Errorf("reflow 响应解析失败: %w", err)
	}
	if parsed.Data.Room == nil {
		return nil, errors.New("No room data from mobile api")
	}
	room := parsed.Data.Room
	cover := ""
	if len(room.Cover.URLList) > 0 {
		cover = room.Cover.URLList[0]
	}
	avatar := ""
	if len(room.Owner.Avatar.URLList) > 0 {
		avatar = room.Owner.Avatar.URLList[0]
	}
	return &roomInfoResult{
		Living:   room.Status == 2,
		Nickname: room.Owner.Nickname,
		SecUID:   room.Owner.SecUID,
		Avatar:   avatar,
		API:      "mobile",
		Room: &roomData{
			Title:     room.Title,
			Cover:     cover,
			IDStr:     room.IDStr,
			StreamURL: room.StreamURL,
		},
	}, nil
}

// debugRawEnter 调试用：签名请求 enter API 并返回原始响应体。
func debugRawEnter(webRoomID string) ([]byte, error) {
	cookies, err := getCookie()
	if err != nil {
		return nil, err
	}
	params := "aid=6383" +
		"&live_id=1" +
		"&device_platform=web" +
		"&language=zh-CN" +
		"&enter_from=web_live" +
		"&cookie_enabled=true" +
		"&screen_width=1920" +
		"&screen_height=1080" +
		"&browser_language=zh-CN" +
		"&browser_platform=MacIntel" +
		"&browser_name=Chrome" +
		"&browser_version=108.0.0.0" +
		"&web_rid=" + url.QueryEscape(webRoomID) +
		"&Room-Enter-User-Login-Ab=0" +
		"&is_need_double_stream=false"
	ab := newAbogus()
	query := ab.generateAbogus(params, "")
	res, err := dyGet("https://live.douyin.com/webcast/room/web/enter/?"+query, map[string]string{
		"cookie":     cookies,
		"User-Agent": ab.userAgent,
	})
	if err != nil {
		return nil, err
	}
	return []byte(res.body), nil
}

// debugHomeLiveRooms 调试用：抓直播首页推荐位中正在直播的 webRid 列表。
func debugHomeLiveRooms() ([]string, error) {
	nonce := generateNonce()
	signed := GetACSignature(time.Now().Unix(), "https://live.douyin.com/", nonce, dyUAHTML)
	res, err := dyGet("https://live.douyin.com/", map[string]string{
		"User-Agent": dyUAHTML,
		"cookie":     "__ac_nonce=" + nonce + "; __ac_signature=" + signed + "; __ac_referer=__ac_blank",
	})
	if err != nil {
		return nil, err
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\\"web_rid\\":\\"(\d+)\\"`),
		regexp.MustCompile(`\\"webRid\\":\\"(\d+)\\"`),
		regexp.MustCompile(`live\.douyin\.com/(\d{6,})`),
		regexp.MustCompile(`\"rid\":\"?(\d{6,})`),
	}
	seen := map[string]bool{}
	var out []string
	for pi, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(res.body, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				ctx := m[0]
				if len(ctx) > 80 {
					ctx = ctx[:80]
				}
				out = append(out, fmt.Sprintf("p%d:%s ctx=%q", pi, m[1], ctx))
			}
		}
	}
	return out, nil
}
