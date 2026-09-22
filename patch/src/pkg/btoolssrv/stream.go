package btoolssrv

// getRoomInfo / getInfo / getStream 移植自 douyin_api.js 的 getRoomInfo
// 与 stream.js 的 getInfo / getStream。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var dyWebRidRegex = regexp.MustCompile(`"webRid\\":\\"(\d+)\\"`)
var dyUniqueIDRegex = regexp.MustCompile(`\\"uniqueId\\":\\"(.*?)\\"`)

// qualityList 顺序决定清晰度优先级。
var qualityList = []struct {
	Key  string
	Desc string
}{
	{"origin", "原画"},
	{"uhd", "蓝光"},
	{"hd", "超清"},
	{"sd", "高清"},
	{"ld", "标清"},
	{"ao", "音频流"},
	{"real_origin", "真原画"},
}

type getRoomInfoOpts struct {
	API          string // web / webHTML / mobile / userHTML / random / 空=web
	Auth         string // cookie
	UID          string // sec_user_id
	DoubleScreen bool
}

type roomInfoFull struct {
	Living  bool
	RoomID  string
	Owner   string
	Title   string
	Streams []qualityOption
	Sources []sourceInfo
	Avatar  string
	Cover   string
	LiveID  string
	UID     string
	API     string
}

type sourceInfo struct {
	Name      string
	StreamMap any
	Streams   []streamEntry
}

// getRoomInfo 对应 douyin_api.js 的 getRoomInfo。
func getRoomInfo(webRoomID string, opts getRoomInfoOpts) (*roomInfoFull, error) {
	api := opts.API
	if api == "" {
		api = "web"
	}
	if api == "random" {
		apis := []string{"web", "webHTML", "mobile", "userHTML"}
		api = apis[randIntn(len(apis))]
	}
	// mobile/userHTML 需要 sec_uid，老数据没有则回退 web
	if (api == "mobile" || api == "userHTML") && opts.UID == "" {
		api = "web"
	}

	var data *roomInfoResult
	var err error
	switch api {
	case "webHTML":
		data, err = getRoomInfoByHtml(webRoomID, opts.Auth)
	case "mobile":
		data, err = getRoomInfoByMobile(opts.UID, opts.Auth)
	case "userHTML":
		data, err = getRoomInfoByUserWeb(opts.UID, opts.Auth)
	default:
		data, err = getRoomInfoByWeb(webRoomID, opts.Auth)
	}
	if err != nil {
		return nil, err
	}
	if data.Room == nil {
		return nil, fmt.Errorf("No room data, id %s", webRoomID)
	}
	room := data.Room

	if api == "userHTML" {
		return &roomInfoFull{
			Living:  data.Living,
			RoomID:  webRoomID,
			Owner:   data.Nickname,
			Title:   orDefault(room.Title, data.Nickname),
			Streams: []qualityOption{},
			Sources: []sourceInfo{},
			Avatar:  data.Avatar,
			Cover:   room.Cover,
			LiveID:  room.IDStr,
			UID:     data.SecUID,
			API:     data.API,
		}, nil
	}
	if room.StreamURL == nil {
		return &roomInfoFull{
			Living:  false,
			RoomID:  webRoomID,
			Owner:   data.Nickname,
			Title:   orDefault(room.Title, data.Nickname),
			Streams: []qualityOption{},
			Sources: []sourceInfo{},
			Avatar:  data.Avatar,
			Cover:   room.Cover,
			LiveID:  room.IDStr,
			UID:     data.SecUID,
			API:     data.API,
		}, nil
	}

	var qualities []qualityOption
	var streamData any
	if opts.DoubleScreen && len(room.StreamURL.PullDatas) > 0 {
		// 取第一个 pull_data
		for _, pd := range room.StreamURL.PullDatas {
			qualities = pd.Options.Qualities
			streamData = pd.StreamData
			break
		}
	}
	if streamData == nil {
		qualities = room.StreamURL.LiveCoreSDKData.PullData.Options.Qualities
		streamData = room.StreamURL.LiveCoreSDKData.PullData.StreamData
	}

	// stream_data 可能是 JSON 字符串或对象，最终取 .data 字段
	var streamMap map[string]struct {
		Main struct {
			Flv string `json:"flv"`
			Hls string `json:"hls"`
		} `json:"main"`
	}
	switch sd := streamData.(type) {
	case string:
		if sd != "" {
			var wrapper struct {
				Data map[string]struct {
					Main struct {
						Flv string `json:"flv"`
						Hls string `json:"hls"`
					} `json:"main"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(sd), &wrapper); err != nil {
				return nil, fmt.Errorf("stream_data 解析失败: %w", err)
			}
			streamMap = wrapper.Data
		}
	case map[string]any:
		b, _ := json.Marshal(sd)
		var wrapper struct {
			Data map[string]struct {
				Main struct {
					Flv string `json:"flv"`
					Hls string `json:"hls"`
				} `json:"main"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &wrapper); err != nil {
			return nil, fmt.Errorf("stream_data 解析失败: %w", err)
		}
		streamMap = wrapper.Data
	}

	streamList := make([]streamEntry, 0, len(streamMap))
	for q, info := range streamMap {
		name := "未知"
		for _, item := range qualityList {
			if item.Key == q {
				name = item.Desc
				break
			}
		}
		if info.Main.Flv == "" && info.Main.Hls == "" {
			continue
		}
		streamList = append(streamList, streamEntry{Quality: q, Name: name, Flv: info.Main.Flv, Hls: info.Main.Hls})
	}
	// 真原画藏在 ao 流里
	for _, s := range streamList {
		if s.Quality == "ao" {
			streamList = append(streamList, streamEntry{
				Quality: "real_origin",
				Name:    "真原画",
				Flv:     strings.ReplaceAll(s.Flv, "&only_audio=1", ""),
				Hls:     strings.ReplaceAll(s.Hls, "&only_audio=1", ""),
			})
			break
		}
	}
	qualityRank := func(q string) int {
		for i, item := range qualityList {
			if item.Key == q {
				return i
			}
		}
		return len(qualityList)
	}
	// 稳定排序：按 qualityList 顺序
	for i := 1; i < len(streamList); i++ {
		for j := i; j > 0 && qualityRank(streamList[j-1].Quality) > qualityRank(streamList[j].Quality); j-- {
			streamList[j-1], streamList[j] = streamList[j], streamList[j-1]
		}
	}

	sources := []sourceInfo{{Name: "自动", StreamMap: streamMap, Streams: streamList}}
	return &roomInfoFull{
		Living:  data.Living,
		RoomID:  webRoomID,
		Owner:   data.Nickname,
		Title:   room.Title,
		Streams: qualities,
		Sources: sources,
		Avatar:  data.Avatar,
		Cover:   room.Cover,
		LiveID:  room.IDStr,
		UID:     data.SecUID,
		API:     data.API,
	}, nil
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// ---------- getInfo / getStream（stream.js） ----------

type liveInfo struct {
	Living   bool   `json:"living"`
	Owner    string `json:"owner"`
	Title    string `json:"title"`
	RoomID   string `json:"roomId"`
	Avatar   string `json:"avatar"`
	Cover    string `json:"cover"`
	LiveID   string `json:"liveId"`
	UID      string `json:"uid"`
	API      string `json:"api"`
	Current  *streamEntry
	VideoURL string `json:"-"`
}

// getInfo 对应 stream.js getInfo；api=balance 时走负载均衡。
func getInfo(channelID string, opts getRoomInfoOpts) (*liveInfo, error) {
	var info *roomInfoFull
	var err error
	if opts.API == "balance" {
		info, err = globalLoadBalancer.callWithLoadBalance(channelID, opts)
	} else {
		info, err = getRoomInfo(channelID, opts)
	}
	if err != nil {
		return nil, err
	}
	return &liveInfo{
		Living:  info.Living,
		Owner:   info.Owner,
		Title:   info.Title,
		RoomID:  info.RoomID,
		Avatar:  info.Avatar,
		Cover:   info.Cover,
		LiveID:  info.LiveID,
		UID:     info.UID,
		API:     info.API,
	}, nil
}

type getStreamOpts struct {
	ChannelID        string
	Quality          any // 传入 0 表示未指定
	FormatPriorities []string
	DoubleScreen     bool
	API              string
	Auth             string
	UID              string
	StrictQuality    bool
}

// getStream 对应 stream.js getStream。
func getStream(opts getStreamOpts) (*liveInfo, error) {
	api := opts.API
	if api == "" {
		api = "web"
	}
	if api == "userHTML" {
		// userHTML 接口只能用于状态检测
		api = "web"
	}
	info, err := getRoomInfo(opts.ChannelID, getRoomInfoOpts{
		DoubleScreen: opts.DoubleScreen,
		Auth:         opts.Auth,
		API:          api,
		UID:          opts.UID,
	})
	if err != nil {
		return nil, err
	}
	if !info.Living {
		return nil, errors.New("It must be called getStream when living")
	}
	if len(info.Sources) == 0 {
		return nil, errors.New("未找到对应的流")
	}
	sources := info.Sources[0]
	formatPriorities := opts.FormatPriorities
	if len(formatPriorities) == 0 {
		formatPriorities = []string{"flv", "hls"}
	}
	var target *streamEntry
	if q, ok := opts.Quality.(string); ok && q != "" {
		for i := range sources.Streams {
			if sources.Streams[i].Quality == q {
				target = &sources.Streams[i]
				break
			}
		}
		if target == nil && opts.StrictQuality {
			return nil, errors.New("Can not get expect quality because of strictQuality")
		}
	}
	if target == nil {
		for i := range sources.Streams {
			if sources.Streams[i].Flv != "" || sources.Streams[i].Hls != "" {
				target = &sources.Streams[i]
				break
			}
		}
	}
	if target == nil {
		return nil, errors.New("未找到对应的流")
	}
	u := ""
	for _, format := range formatPriorities {
		switch format {
		case "flv":
			u = target.Flv
		case "hls":
			u = target.Hls
		}
		if u != "" {
			break
		}
	}
	if u == "" {
		return nil, errors.New("未找到对应的流")
	}
	return &liveInfo{
		Living:   info.Living,
		Owner:    info.Owner,
		Title:    info.Title,
		RoomID:   info.RoomID,
		Avatar:   info.Avatar,
		Cover:    info.Cover,
		LiveID:   info.LiveID,
		UID:      info.UID,
		API:      info.API,
		Current:  target,
		VideoURL: u,
	}, nil
}

// ---------- URL 解析（provider.resolveChannelInfoFromURL 相关） ----------

var dyMatchURLRe = regexp.MustCompile(`^https?://(live|v|www)\.douyin\.com/`)

func matchDouyinURL(channelURL string) bool {
	return dyMatchURLRe.MatchString(channelURL)
}

// resolveShortURL 解析 v.douyin.com 短链接为 webRid。
func resolveShortURL(shortURL string) (string, error) {
	res, err := dyGet(shortURL, map[string]string{"User-Agent": dyUARequester})
	if err != nil {
		return "", err
	}
	if strings.Contains(res.finalURL, "/user/") {
		if u, err := url.Parse(res.finalURL); err == nil {
			secUID := u.Query().Get("sec_uid")
			if secUID != "" {
				return parseUser("https://www.douyin.com/user/" + secUID)
			}
		}
		return "", errors.New("无法从短链接解析出直播间ID")
	}
	if m := dyWebRidRegex.FindStringSubmatch(res.body); m != nil {
		return m[1], nil
	}
	return "", errors.New("无法从短链接解析出直播间ID")
}

// parseUser 解析用户主页得到 uniqueId（抖音号）。
func parseUser(userURL string) (string, error) {
	ua := dyUAHTML
	nonce, err := getNonce(userURL)
	if err != nil || nonce == "" {
		nonce = generateNonce()
	}
	signed := GetACSignature(timeNowUnix(), userURL, nonce, ua)
	res, err := dyGet(userURL, map[string]string{
		"User-Agent": ua,
		"cookie":     "__ac_nonce=" + nonce + "; __ac_signature=" + signed,
	})
	if err != nil {
		return "", err
	}
	if m := dyUniqueIDRegex.FindStringSubmatch(res.body); m != nil && m[1] != "" {
		return m[1], nil
	}
	return "", nil
}

// resolveChannelInfoFromURL 对应 provider.resolveChannelInfoFromURL。
func resolveChannelInfoFromURL(channelURL string) (map[string]string, error) {
	if !matchDouyinURL(channelURL) {
		return nil, nil
	}
	var id string
	var err error
	switch {
	case strings.Contains(channelURL, "v.douyin.com"):
		id, err = resolveShortURL(channelURL)
		if err != nil {
			return nil, fmt.Errorf("解析抖音短链接失败: %w", err)
		}
	case strings.Contains(channelURL, "/user/"):
		id, err = parseUser(channelURL)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, errors.New("解析抖音用户主页失败")
		}
	default:
		if u, perr := url.Parse(channelURL); perr == nil {
			id = path.Base(u.Path)
		} else {
			return nil, perr
		}
	}
	info, err := getInfo(id, getRoomInfoOpts{})
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"id":     info.RoomID,
		"title":  info.Title,
		"owner":  info.Owner,
		"avatar": info.Avatar,
		"uid":    info.UID,
	}, nil
}
