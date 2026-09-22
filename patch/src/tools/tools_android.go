//go:build android

// Package tools 的 Android 实现。
//
// Android 平台无法运行 remotetools 管理的外部工具：ffmpeg / node / dotnet /
// bililive-recorder / biliLive-tools 都依赖桌面级的可执行文件与「可写且可执行」的目录，
// 而 Android 的 app 私有目录挂载了 noexec。因此本文件为 tools 包提供一套最小空实现：
//
//   - 不下载、不启动任何外部工具（避免在手机上产生无意义的下载与后台重启循环）
//   - FFmpeg 完全依赖 config.yml 里显式指定的 ffmpeg_path（由 APK 内嵌的 libffmpeg.so 提供）
//   - 平台依赖检查一律视为「就绪」，让直播间走正常请求路径；真正失败时给出明确错误，
//     而不是让用户一直看到「准备中」
package tools

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bililive-go/bililive-go/src/pkg/btoolssrv"

	"github.com/bililive-go/bililive-go/src/configs"
	blog "github.com/bililive-go/bililive-go/src/log"

	"github.com/kira1928/remotetools/pkg/tools"
)

const (
	// BToolsPort bililive-tools 本地 HTTP 服务监听端口（Android 上由内置
	// 的 btoolssrv 服务监听，见 AsyncInit）
	BToolsPort = 18110
	// BToolsAuthToken 访问 bililive-tools 本地 HTTP 服务所需的 Authorization 头
	BToolsAuthToken = "Basic YTph"
)

// FFmpegStatus FFmpeg 就绪状态（与桌面版结构一致，供 SSE / WebUI 复用）
type FFmpegStatus struct {
	State   string `json:"state"`             // checking / downloading / ready / not_found / error
	Message string `json:"message,omitempty"` // 人读消息
	Source  string `json:"source,omitempty"`  // system / remotetools
}

var (
	ffmpegStatusVal       atomic.Value // stores FFmpegStatus
	ffmpegStatusMu        sync.Mutex
	ffmpegCbMu            sync.RWMutex
	ffmpegCallbacks       []func(FFmpegStatus)
	ffmpegStatusChangedMu sync.Mutex
	ffmpegStatusChangedCh = make(chan struct{})
	ffmpegInitRunning     atomic.Bool
)

func init() {
	ffmpegStatusVal.Store(FFmpegStatus{State: "checking"})
}

func setFFmpegStatus(s FFmpegStatus) {
	ffmpegStatusMu.Lock()
	defer ffmpegStatusMu.Unlock()

	ffmpegStatusVal.Store(s)
	ffmpegStatusChangedMu.Lock()
	close(ffmpegStatusChangedCh)
	ffmpegStatusChangedCh = make(chan struct{})
	ffmpegStatusChangedMu.Unlock()

	ffmpegCbMu.RLock()
	cbs := make([]func(FFmpegStatus), len(ffmpegCallbacks))
	copy(cbs, ffmpegCallbacks)
	ffmpegCbMu.RUnlock()
	for _, cb := range cbs {
		cb(s)
	}
}

// GetFFmpegStatus 返回当前 FFmpeg 状态
func GetFFmpegStatus() FFmpegStatus {
	v := ffmpegStatusVal.Load()
	if v == nil {
		return FFmpegStatus{State: "checking"}
	}
	return v.(FFmpegStatus)
}

// IsFFmpegReady 返回 FFmpeg 是否已就绪
func IsFFmpegReady() bool {
	return GetFFmpegStatus().State == "ready"
}

// FFmpegStatusChanged 返回一个在下一次 FFmpeg 状态变化时关闭的 channel
func FFmpegStatusChanged() <-chan struct{} {
	ffmpegStatusChangedMu.Lock()
	defer ffmpegStatusChangedMu.Unlock()
	return ffmpegStatusChangedCh
}

// WaitFFmpegAsyncInitDone 等待 FFmpeg 异步初始化进入终态。
// Android 上的检测是瞬时的（只做一次 os.Stat），通常立即返回。
func WaitFFmpegAsyncInitDone(ctx context.Context, stop <-chan struct{}) error {
	for {
		changed := FFmpegStatusChanged()
		state := GetFFmpegStatus().State
		if state != "checking" && state != "downloading" {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return errors.New("等待 FFmpeg 就绪时被中断")
		}
	}
}

// OnFFmpegStatusChange 注册 FFmpeg 状态变化回调
func OnFFmpegStatusChange(fn func(FFmpegStatus)) {
	ffmpegCbMu.Lock()
	ffmpegCallbacks = append(ffmpegCallbacks, fn)
	ffmpegCbMu.Unlock()
}

// ForceFFmpegStatus 强制设置 FFmpeg 状态
func ForceFFmpegStatus(s FFmpegStatus) {
	setFFmpegStatus(s)
}

// FFmpegAsyncInit 检测配置里的 ffmpeg_path。
// Android 上不存在「下载」这条路径：FFmpeg 由 APK 内嵌，必须通过 config.yml 指定。
func FFmpegAsyncInit(ctx context.Context) {
	if !ffmpegInitRunning.CompareAndSwap(false, true) {
		return
	}
	setFFmpegStatus(FFmpegStatus{State: "checking"})

	go func() {
		defer ffmpegInitRunning.Store(false)

		cfg := configs.GetCurrentConfig()
		if cfg == nil {
			setFFmpegStatus(FFmpegStatus{State: "not_found", Message: "配置尚未加载"})
			return
		}

		path := ""
		if cfg.FfmpegPath != "" {
			path = cfg.FfmpegPath
		}
		if path == "" {
			blog.GetLogger().Warn("Android: ffmpeg_path 未配置，录制功能不可用")
			setFFmpegStatus(FFmpegStatus{
				State:   "not_found",
				Message: "Android 上需要在 config.yml 中指定 ffmpeg_path",
			})
			return
		}

		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			blog.GetLogger().Infof("Android: FFmpeg 已就绪: %s", path)
			setFFmpegStatus(FFmpegStatus{State: "ready", Source: "system"})
			return
		}

		blog.GetLogger().Warnf("Android: ffmpeg_path 无效（不存在或为目录）: %s", path)
		setFFmpegStatus(FFmpegStatus{
			State:   "not_found",
			Message: "配置的 ffmpeg_path 不存在或不是文件: " + path,
		})
	}()
}

// PlatformDependenciesReady Android 上恒为就绪。
// 桌面版此处的目的是等 bililive-tools 起来后再请求抖音直播间；Android 上
// bililive-tools 无法运行，若返回未就绪会让直播间永远停在「准备中」，
// 因此直接放行，让请求按正常路径失败并给出明确错误。
func PlatformDependenciesReady(platformKey string) (bool, string) {
	return true, ""
}

// IsBToolsReady 检查 bililive-tools 是否已就绪（Android 上恒为 false）
func IsBToolsReady() bool {
	return false
}

// IsBToolsStarting 检查 bililive-tools 是否正在启动（Android 上恒为 false）
func IsBToolsStarting() bool {
	return false
}

// Cleanup 关闭 tools 包管理的资源。
// Android 上没有 remotetools WebUI，只需终止本进程派生并登记过的子进程
// （如 ffmpeg 与 bililive-scheduler）。
func Cleanup() {
	schedulerShutting.Store(true)
	KillAllProcesses()
}

// DownloaderAvailability 包含各下载器的可用状态
type DownloaderAvailability struct {
	FFmpegAvailable           bool   `json:"ffmpeg_available"`
	FFmpegPath                string `json:"ffmpeg_path,omitempty"`
	NativeAvailable           bool   `json:"native_available"`
	BililiveRecorderAvailable bool   `json:"bililive_recorder_available"`
	BililiveRecorderPath      string `json:"bililive_recorder_path,omitempty"`
}

// GetDownloaderAvailability 返回下载器可用状态。
// Android 上只有内置解析器和（配置了路径的）FFmpeg 可用。
func GetDownloaderAvailability() DownloaderAvailability {
	result := DownloaderAvailability{NativeAvailable: true}
	if cfg := configs.GetCurrentConfig(); cfg != nil && cfg.FfmpegPath != "" {
		if fi, err := os.Stat(cfg.FfmpegPath); err == nil && !fi.IsDir() {
			result.FFmpegAvailable = true
			result.FFmpegPath = cfg.FfmpegPath
		}
	}
	return result
}

// Init 在 Android 环境下不初始化 remotetools
func Init() error {
	blog.GetLogger().Info("Android 环境：跳过 RemoteTools 初始化")
	return nil
}

// WaitForToolsInit Android 上无需等待任何工具初始化
func WaitForToolsInit(ctx context.Context) error {
	return nil
}

// AsyncInit 在 Android 环境下不启动 remotetools 管理的外部工具，但会启动：
//   - 内置的 btools 等价服务（Go 版抖音解析，见 pkg/btoolssrv），供抖音平台
//     通过 127.0.0.1:18110 的 /bgo/* 接口取房间信息与流地址；
//   - 预置的 bililive-scheduler（静态 Go 二进制，位于 ~/bililive/tools/），
//     为 Web UI 的 /scheduler/ 提供定时录制调度与 Web 界面。
func AsyncInit() {
	// 内置 btools 等价服务（替代 Node 版 biliLive-tools）
	btoolssrv.Start()
	// bililive-scheduler（存在则启动，不存在则静默跳过）
	go superviseScheduler()
}

// ---------- bililive-scheduler（Android 静态二进制版） ----------

var (
	schedulerPort        atomic.Int32
	schedulerSupervising atomic.Bool
	schedulerShutting    atomic.Bool
)

// schedulerBinPath 预置 scheduler 二进制的固定位置。
func schedulerBinPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "bililive", "tools", "bililive-scheduler")
}

// superviseScheduler 守护循环：进程退出后 10 秒重启，直到程序关闭。
func superviseScheduler() {
	if !schedulerSupervising.CompareAndSwap(false, true) {
		return
	}
	for {
		if schedulerShutting.Load() {
			return
		}
		cfg := configs.GetCurrentConfig()
		bin := schedulerBinPath()
		if cfg == nil || !cfg.RPC.Enable || bin == "" {
			time.Sleep(10 * time.Second)
			continue
		}
		if _, err := os.Stat(bin); err != nil {
			blog.GetLogger().Info("Android: 未找到 bililive-scheduler（~/bililive/tools/），跳过启动")
			return
		}
		startSchedulerOnce(bin, cfg)
		time.Sleep(10 * time.Second)
	}
}

// startSchedulerOnce 启动一次 scheduler 并轮询端口文件。
func startSchedulerOnce(bin string, cfg *configs.Config) {
	bind := cfg.RPC.Bind
	if bind == "" {
		bind = ":8080"
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		host, port, err = net.SplitHostPort(":" + bind)
		if err != nil {
			blog.GetLogger().WithError(err).Errorln("Android: 解析 RPC bind 地址失败")
			return
		}
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	apiURL := "http://" + net.JoinHostPort(host, port)

	dbPath := filepath.Join(cfg.AppDataPath, "db", "scheduler.db")
	os.MkdirAll(filepath.Dir(dbPath), 0o755)
	portFile := filepath.Join(filepath.Dir(dbPath), "scheduler.port")
	os.Remove(portFile)

	cmd := exec.Command(bin, "--api-url", apiURL, "--db-path", dbPath, "--port", "0")
	logDir := filepath.Join(cfg.AppDataPath, "logs")
	os.MkdirAll(logDir, 0o755)
	if lf, err := os.OpenFile(filepath.Join(logDir, "scheduler.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	if err := cmd.Start(); err != nil {
		blog.GetLogger().WithError(err).Errorln("Android: bililive-scheduler 启动失败")
		return
	}
	RegisterProcess("bililive-scheduler", cmd.Process.Pid, ProcessCategoryScheduler)
	blog.GetLogger().Infof("Android: bililive-scheduler 已启动 (pid %d)", cmd.Process.Pid)

	// 轮询端口文件（scheduler 就绪后会把自己的监听端口写进去）
	portCh := make(chan int, 1)
	go func() {
		defer close(portCh)
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			if b, err := os.ReadFile(portFile); err == nil {
				if p, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && p > 0 {
					portCh <- p
					return
				}
			}
		}
	}()
	select {
	case p := <-portCh:
		schedulerPort.Store(int32(p))
		blog.GetLogger().Infof("Android: bililive-scheduler listening on port %d", p)
	case <-time.After(35 * time.Second):
		blog.GetLogger().Warnln("Android: 等待 bililive-scheduler 端口文件超时")
	}
	cmd.Wait()
	schedulerPort.Store(0)
	UnregisterProcess("bililive-scheduler")
	blog.GetLogger().Warnln("Android: bililive-scheduler 进程退出")
}

// SyncBuiltInTools Android 上不支持同步内置工具
func SyncBuiltInTools(targetToolFolder string) error {
	return errors.New("Android 环境不支持同步内置工具")
}

// AsyncDownloadIfNecessary Android 上不下载远程工具
func AsyncDownloadIfNecessary(toolName string) {
}

// DownloadIfNecessary Android 上不支持下载远程工具
func DownloadIfNecessary(toolName string) error {
	return errors.New("Android 环境不支持远程工具下载")
}

// GetWebUIPort Android 上没有 RemoteTools WebUI
func GetWebUIPort() int {
	return 0
}

// GetSchedulerPort 返回 bililive-scheduler 的监听端口（未启动为 0）。
func GetSchedulerPort() int {
	return int(schedulerPort.Load())
}

// Get Android 上不提供 remotetools API 实例
func Get() *tools.API {
	return nil
}

// FixFlvByBililiveRecorder Android 上不可用（依赖 .NET）
func FixFlvByBililiveRecorder(ctx context.Context, fileName string) ([]string, error) {
	return nil, errors.New("Android 环境不支持 BililiveRecorder")
}

// FixFlvByBililiveRecorderWithPID Android 上不可用（依赖 .NET）
func FixFlvByBililiveRecorderWithPID(ctx context.Context, fileName string, onPID func(pid int)) ([]string, error) {
	return nil, errors.New("Android 环境不支持 BililiveRecorder")
}
