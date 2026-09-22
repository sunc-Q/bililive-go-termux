#!/usr/bin/env bash
# 交叉编译 bililive-go 为 Android 二进制（arm64 / x86_64）。
#
# 关键点（踩坑记录）：
#   1. 必须 CGO_ENABLED=1 —— Android 上 Go 的纯 Go 解析器无法工作
#      （Go 源码 net/conf.go: goosPrefersCgo() 里 android 返回 true，
#       但该分支被 `if !cgoAvailable { return }` 挡住；CGO 关闭时
#       mustUseGoResolver 强制返回 true，而 Android 没有 /etc/resolv.conf，
#       于是 DNS 全部失败）。开启 cgo 后走 bionic 的 getaddrinfo → netd。
#   2. CC 必须用 clang.exe + --target，不能用 aarch64-linux-android21-clang
#      （那是 shell 脚本，Go 在 Windows 上没法执行）。
#   3. CC 里的路径必须是 Windows 风格（E:/...），Git Bash 的 /e/... 形式
#      clang.exe 认不出来，会报 "stdlib.h file not found"。
#   4. 必须显式 -tags=release，前端资源通过 //go:embed 打进二进制。
#
# 用法：  bash android/build-go-android.sh [输出路径] [arm64|amd64]
#   或：  ARCH=amd64 bash android/build-go-android.sh
set -euo pipefail

case "$ARCH" in
  arm64) GOARCH_VAL=arm64;  CLANG_TARGET="aarch64-linux-android"; JLIB_DIR="arm64-v8a" ;;
  amd64) GOARCH_VAL=amd64;  CLANG_TARGET="x86_64-linux-android";  JLIB_DIR="x86_64" ;;
  *) echo "不支持的架构: $ARCH（可选 arm64 / amd64）" >&2; exit 1 ;;
esac

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$REPO_ROOT/android/app/src/main/jniLibs/$JLIB_DIR/libbililive.so}"

NDK_VER="${NDK_VER:-27.0.12077973}"
NDKW="E:/Android/Sdk/ndk/${NDK_VER}/toolchains/llvm/prebuilt/windows-x86_64"
API="${API:-21}"

if [ ! -x "$NDKW/bin/clang.exe" ]; then
  echo "找不到 NDK clang.exe: $NDKW/bin/clang.exe" >&2
  echo "可用版本：" >&2
  ls -d /e/Android/Sdk/ndk/*/ 2>/dev/null >&2
  exit 1
fi

export CC="$NDKW/bin/clang.exe --target=${CLANG_TARGET}${API} --sysroot=$NDKW/sysroot"
export CXX="$NDKW/bin/clang++.exe --target=${CLANG_TARGET}${API} --sysroot=$NDKW/sysroot"
export AR="$NDKW/bin/llvm-ar.exe"
export RANLIB="$NDKW/bin/llvm-ranlib.exe"
export GOOS=android GOARCH="$GOARCH_VAL" CGO_ENABLED=1

GO_BIN="${GO_BIN:-$HOME/go/go1.25.0/bin/go}"
command -v "$GO_BIN" >/dev/null 2>&1 || GO_BIN=go

cd "$REPO_ROOT"
VER="$(git describe --tags --always 2>/dev/null || echo dev)"
HASH="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
# 注意：-ldflags 的值不能再含裸空格，否则 go 会把它当成多个 flag
NOW="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
PKG="github.com/bililive-go/bililive-go/src/consts"

echo "==> 目标      : android/$GOARCH_VAL (API $API, CGO=1)"
echo "==> NDK       : $NDK_VER"
echo "==> 版本      : $VER ($HASH)"
echo "==> 输出      : $OUT"

mkdir -p "$(dirname "$OUT")"

"$GO_BIN" build -tags=release \
  -ldflags="-s -w -X ${PKG}.AppVersion=${VER} -X ${PKG}.BuildTime=${NOW} -X ${PKG}.GitHash=${HASH}" \
  -o "$OUT" ./src/cmd/bililive/

echo "==> 构建完成"
ls -la "$OUT"

# 校验：必须是 android/arm64、动态链接 libc.so（证明 cgo 生效）
READELF="$NDKW/bin/llvm-readelf.exe"
if [ -x "$READELF" ]; then
  echo "==> 链接校验"
  "$READELF" -h "$OUT" | grep -E "Machine|Type" || true
  "$READELF" -d "$OUT" | grep -E "NEEDED|SONAME" || echo "  !! 没有 NEEDED，说明 cgo 没生效"
  "$READELF" -l "$OUT" | grep -A1 INTERP | tail -1 || true
fi
