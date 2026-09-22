#!/data/data/com.termux/files/usr/bin/bash
# bililive-go-termux 一键安装脚本
# 用法（Termux 内执行）：
#   bash -c "$(curl -fsSL https://raw.githubusercontent.com/sunc-Q/bililive-go-termux/main/install.sh)"
# 可选环境变量：
#   MIRROR=ghfast.top   指定 GitHub 加速镜像（默认自动尝试多个）
#   VERSION=v0.8.2-termux.1  指定版本（默认最新 release）
set -e

REPO="sunc-Q/bililive-go-termux"
INSTALL_DIR="$HOME/bililive"
BIN="$INSTALL_DIR/bililive-go"

c_info='\033[1;32m'; c_warn='\033[1;33m'; c_err='\033[1;31m'; c_off='\033[0m'
info() { echo -e "${c_info}[安装]${c_off} $*"; }
warn() { echo -e "${c_warn}[跳过]${c_off} $*"; }
fail() { echo -e "${c_err}[失败]${c_off} $*"; exit 1; }

# ---------- 环境检查 ----------
[ -n "$TERMUX_VERSION" ] || fail "请在 Termux 中运行本脚本（未检测到 Termux 环境）"
ARCH="$(uname -m)"
[ "$ARCH" = "aarch64" ] || fail "仅支持 64 位 ARM（aarch64），当前架构: $ARCH"

# ---------- GitHub 下载（带国内镜像回退） ----------
# 原始 URL -> 依次尝试直连与多个加速镜像
GH_MIRRORS=("", "https://ghfast.top/", "https://gh-proxy.com/", "https://ghproxy.net/")
gh_fetch() { # gh_fetch <github-url> <输出文件>
  local url="$1" out="$2" rc=1
  for m in "${GH_MIRRORS[@]}"; do
    if curl -fsSL --connect-timeout 10 --max-time 600 -o "$out" "$m$url"; then
      [ -s "$out" ] && return 0
    fi
    rm -f "$out"
  done
  return 1
}

# ---------- 依赖 ----------
info "安装依赖 (ffmpeg / curl / termux-api)..."
pkg install -y ffmpeg curl termux-api >/dev/null 2>&1 || pkg install -y ffmpeg curl
command -v ffmpeg >/dev/null || fail "ffmpeg 安装失败，请手动执行: pkg install ffmpeg"

# ---------- 解析最新版本 ----------
VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
  info "获取最新版本号..."
  VERSION="$(curl -fsSL --connect-timeout 10 "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | cut -d'"' -f4)" || true
  [ -n "$VERSION" ] || VERSION="v0.8.2-termux.1"
  info "最新版本: $VERSION"
fi

# ---------- 安装目录 ----------
mkdir -p "$INSTALL_DIR/logs" "$INSTALL_DIR/appdata/db"

# ---------- 下载主程序 ----------
if [ -x "$BIN" ]; then
  warn "已存在主程序 $BIN（如需重装先删除该文件）"
else
  info "下载 bililive-go ($VERSION, android/arm64)..."
  BIN_URL="https://github.com/$REPO/releases/download/$VERSION/bililive-go-android-arm64"
  gh_fetch "$BIN_URL" "$BIN" || fail "主程序下载失败，请检查网络后重试"
  chmod 755 "$BIN"
  info "主程序安装完成: $BIN ($(du -h "$BIN" | cut -f1))"
fi

# ---------- 下载管理脚本与配置 ----------
info "安装 bl 管理脚本与默认配置..."
gh_fetch "https://raw.githubusercontent.com/$REPO/main/scripts/bl" "$PREFIX/bin/bl" \
  || fail "bl 脚本下载失败"
chmod 755 "$PREFIX/bin/bl"

if [ ! -f "$INSTALL_DIR/config.yml" ]; then
  gh_fetch "https://raw.githubusercontent.com/$REPO/main/scripts/config.yml" "$INSTALL_DIR/config.yml" \
    || fail "config.yml 下载失败"
else
  warn "保留现有配置 $INSTALL_DIR/config.yml"
fi

gh_fetch "https://raw.githubusercontent.com/$REPO/main/scripts/start.sh" "$INSTALL_DIR/start.sh" \
  || fail "start.sh 下载失败"
chmod 755 "$INSTALL_DIR/start.sh"

# ---------- scheduler（定时录制，可选组件） ----------
SCHED="$INSTALL_DIR/tools/bililive-scheduler"
if [ ! -x "$SCHED" ]; then
  info "下载 bililive-scheduler (定时录制组件)..."
  mkdir -p "$INSTALL_DIR/tools"
  SCHED_URL="https://github.com/$REPO/releases/download/$VERSION/bililive-scheduler-linux-arm64"
  if gh_fetch "$SCHED_URL" "$SCHED"; then
    chmod 755 "$SCHED"
  else
    warn "scheduler 下载失败，跳过（不影响主程序与录制）"
  fi
fi

# ---------- 防休眠 ----------
termux-wake-lock 2>/dev/null || warn "termux-wake-lock 不可用，息屏后进程可能被杀"
info "提示：建议在系统设置中将 Termux 的电池优化设为「无限制」，否则息屏可能被杀进程"

# ---------- 启动 ----------
if bl status 2>/dev/null | grep -q "运行中"; then
  warn "服务已在运行，如需应用新版本请执行: bl restart"
else
  info "启动服务..."
  bl start
fi

LAN_IP="$(ip route get 1 2>/dev/null | awk '{print $7; exit}')"
echo
echo -e "${c_info}==============================================${c_off}"
echo -e "${c_info}  bililive-go 安装完成！${c_off}"
echo -e "  Web 控制台:  http://${LAN_IP:-127.0.0.1}:22290"
echo -e "  定时录制:    http://${LAN_IP:-127.0.0.1}:22290/scheduler/"
echo -e "  管理命令:    bl start|stop|restart|status|log"
echo -e "  录制输出:    /storage/emulated/0/bililive-go"
echo -e "${c_info}==============================================${c_off}"
echo "首次使用请到 Web 控制台「设置」里授权共享存储（录制输出目录），然后添加直播间即可。"
