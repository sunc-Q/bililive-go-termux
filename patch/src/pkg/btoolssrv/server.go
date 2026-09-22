package btoolssrv

// btools 等价 HTTP 服务：与 bililive-tools(bgo 构建) 的 /bgo 路由契约一致，
// 供 src/live/douyin/douyin_btools.go 客户端透明调用。
// 端口 18110 与鉴权 "Basic YTph" 与 tools.BToolsPort/BToolsAuthToken 对应。

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"
)

const (
	btoolsPort      = 18110
	btoolsAuthToken = "Basic YTph"
)

func randIntn(n int) int { return rand.Intn(n) }

func timeNowUnix() int64 { return time.Now().Unix() }

// ServeHTTP 实现 bgo.js 三个路由。导出以便测试。
func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != btoolsAuthToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.URL.Path {
	case "/bgo/channel-info":
		handleChannelInfo(w, r)
	case "/bgo/live-info":
		handleLiveInfo(w, r)
	case "/bgo/stream-info":
		handleStreamInfo(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"marshal failed"}`))
		return
	}
	_, _ = w.Write(b)
}

func writeError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	writeJSON(w, map[string]string{"error": err.Error()})
}

// handleChannelInfo GET /bgo/channel-info?url=<直播间链接>
// 响应: {id,title,owner,avatar,uid}
func handleChannelInfo(w http.ResponseWriter, r *http.Request) {
	channelURL := r.URL.Query().Get("url")
	info, err := resolveChannelInfoFromURL(channelURL)
	if err != nil {
		writeError(w, err)
		return
	}
	if info == nil {
		_, _ = w.Write([]byte(`null`))
		return
	}
	writeJSON(w, info)
}

// handleLiveInfo GET /bgo/live-info?platform=douyin&roomId=<id>
// 响应: {title,owner,living}
func handleLiveInfo(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Query().Get("platform"); p != "douyin" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "Platform not supported"})
		return
	}
	roomID := r.URL.Query().Get("roomId")
	info, err := getInfo(roomID, getRoomInfoOpts{API: "balance"})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"title":  info.Title,
		"owner":  info.Owner,
		"living": info.Living,
	})
}

// handleStreamInfo GET /bgo/stream-info?platform=douyin&roomId=<id>
// 响应: {stream:"<flv url>"}
func handleStreamInfo(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Query().Get("platform"); p != "douyin" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "Platform not supported"})
		return
	}
	roomID := r.URL.Query().Get("roomId")
	info, err := getStream(getStreamOpts{
		ChannelID:        roomID,
		Quality:          0,
		FormatPriorities: []string{"flv"},
		DoubleScreen:     true,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"stream": info.VideoURL})
}

// ResolveChannelInfoFromURLPublic 供调试入口直接调用（绕过 HTTP 层）。
func ResolveChannelInfoFromURLPublic(channelURL string) (map[string]string, error) {
	return resolveChannelInfoFromURL(channelURL)
}

// GetStreamInfoPublic 供调试入口直接调用（绕过 HTTP 层）。
func GetStreamInfoPublic(roomID string) (map[string]string, error) {
	info, err := getStream(getStreamOpts{
		ChannelID:        roomID,
		Quality:          0,
		FormatPriorities: []string{"flv"},
		DoubleScreen:     true,
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"stream": info.VideoURL}, nil
}

// DebugRawEnter 打印 enter API 原始响应（调试用）。
func DebugRawEnter(webRoomID string) ([]byte, error) {
	return debugRawEnter(webRoomID)
}

// Start 在本机 127.0.0.1:18110 启动 btools 等价服务。
// 非阻塞；启动失败（如端口被占）仅记录日志，不影响主流程。
func Start() {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", btoolsPort))
	if err != nil {
		log.Printf("[btoolssrv] 启动失败（端口 %d 可能被占用）: %v", btoolsPort, err)
		return
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(ServeHTTP),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[btoolssrv] 服务退出: %v", err)
		}
	}()
}

// DebugHomeLiveRooms 调试用：获取首页推荐直播房间列表。
func DebugHomeLiveRooms() ([]string, error) { return debugHomeLiveRooms() }
