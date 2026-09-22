# bililive-go-termux

> [bililive-go](https://github.com/bililive-go/bililive-go) 的 **Android / Termux 原生移植版** —— 在任意 64 位安卓手机的 Termux 里一条命令安装，把旧手机变成直播录制机。

**支持平台：抖音（含签名服务）、哔哩哔哩** 等全部 bililive-go 上游支持的直播平台。

## 特性

- 📱 **Termux 原生运行**：NDK 交叉编译的 bionic 可执行文件（android/arm64），无需 root、无需 proot 容器、无需 APK
- 🔥 **内置抖音签名服务**：将 [biliLive-tools](https://github.com/kira1928/biliLive-tools) 的抖音签名（SM3 / a_bogus / `__ac_signature`）以纯 Go 算法重写并编译进主程序（`pkg/btoolssrv`），监听 `127.0.0.1:18110`，无任何 Node.js / 外部进程依赖
- ⏰ **定时录制**：集成 [bililive-scheduler](https://github.com/Joftal/bililive-scheduler)（静态编译），主程序自动拉起并守护，`/scheduler/` Web UI 可用
- 🎬 **自动转码**：Web 控制台一键调用 Termux ffmpeg 转封装 MP4
- 🛠 **一键管理**：`bl start|stop|restart|status|log`，支持 `bl start -f` 抢占被其他实例占用的端口
- 💾 **录制输出到手机共享存储**：`/storage/emulated/0/bililive-go/<平台>/<主播>/`，随时用文件管理器查看

## 一键安装

在 **Termux** 中执行（不支持，也不需要 root）：

**国内网络（推荐，实测可用）：**

```bash
bash -c "$(curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/sunc-Q/bililive-go-termux/main/install.sh)"
```

**国际网络 / 直连：**

```bash
bash -c "$(curl -fsSL https://raw.githubusercontent.com/sunc-Q/bililive-go-termux/main/install.sh)"
```

安装脚本会自动：

1. 安装 ffmpeg（用于录制与转码）
2. 从 Release 下载 android/arm64 主程序与 scheduler
3. 写入默认配置（Web 端口 `0.0.0.0:22290`、输出目录共享存储、ffmpeg 路径）
4. 安装 `bl` 管理命令、开启 wake-lock 防休眠
5. 启动服务并打印局域网访问地址

> 国内网络不畅时脚本会自动尝试多个 GitHub 加速镜像；也可手动指定：`MIRROR=ghfast.top bash -c "$(curl -fsSL ...)"`

安装完成后，**建议**在系统设置中把 Termux 的电池优化设为「无限制」，否则息屏后进程可能被系统杀死。

## 使用

浏览器打开安装完成时打印的地址（如 `http://192.168.x.x:22290`）：

1. 首次使用在 Web 控制台「设置」中授权共享存储（录制输出目录）
2. 添加直播间链接（抖音 / B 站等），开播自动录制
3. 录制文件在手机存储根目录 `bililive-go/` 下按「平台/主播」归档

SSH 进 Termux 或直接在手机 Termux 里管理：

| 命令 | 说明 |
|---|---|
| `bl start` | 启动（端口被占时会拒绝并提示） |
| `bl start -f` | 强制启动：停掉占用 22290 的其他实例后接管 |
| `bl stop` / `bl restart` | 停止 / 重启（连同 ffmpeg 子进程一起收干净） |
| `bl status` | 查看运行状态与端口监听 |
| `bl log [行数]` | 查看日志尾部 |

## 与上游的差异

基于上游 `bililive-go@ef71711`（v0.8.2），本仓库包含以下 Android 专属修改（完整源码见 [`patch/`](patch/)，构建方式见 [build/README.md](build/README.md)）：

| 修改 | 说明 |
|---|---|
| `pkg/btoolssrv`（新增） | 纯 Go 实现的 btools 等价服务：SM3 国密哈希、ABogus 签名、`__ac_signature` 计算、抖音房间/取流 API、多通道负载均衡，HTTP 契约与 [kira1928/biliLive-tools](https://github.com/kira1928/biliLive-tools) 的 `/bgo/*` 接口逐字兼容（`Basic YTph` 鉴权） |
| `tools/tools_android.go`（重写） | Android 构建下不再启动 Node 版外部工具，改为启动内置 `btoolssrv`；自动拉起并守护 `bililive-scheduler` |
| `android/build-go-android.sh` | NDK 交叉编译脚本，产出 bionic 链接的 arm64 可执行文件（内嵌 Web UI） |

上游的 Tools Web UI（`/tools/`，remotetools 工具管理器）在 Android 构建中刻意不启用：它管理的 glibc 工具无法在 bionic 环境运行，其核心职能（抖音签名、ffmpeg 兜底、调度）已分别由内置签名服务、Termux ffmpeg、独立 scheduler 覆盖，页面显示"未就绪"属预期行为。

## 从源码构建

见 [build/README.md](build/README.md)：NDK 27 + Go 1.25，一条脚本产出 android/arm64 二进制。

## 致谢

- [bililive-go](https://github.com/bililive-go/bililive-go) —— 直播录制核心（GPL-3.0）
- [kira1928/biliLive-tools](https://github.com/kira1928/biliLive-tools)（[renmu123/biliLive-tools](https://github.com/renmu123/biliLive-tools) 分叉）—— 抖音签名算法来源（GPL-3.0），本仓库 `pkg/btoolssrv` 为其 TS 实现的 Go 移植
- [Joftal/bililive-scheduler](https://github.com/Joftal/bililive-scheduler) —— 定时录制
- [kira1928/remotetools](https://github.com/kira1928/remotetools) —— 上游工具管理框架

## 许可证

本项目沿用上游 **GPL-3.0** 协议，详见 [LICENSE](LICENSE)。本项目为独立移植，与上游 bililive-go 官方无关；请仅用于录制授权范围内允许保存的内容，勿用于商业用途。
