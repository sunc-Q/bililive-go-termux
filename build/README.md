# 从源码构建 android/arm64 二进制

## 环境

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | ≥ 1.25 | 编译主程序 |
| Android NDK | r27 (27.0.12077973 已验证) | 提供 `aarch64-linux-android*-clang` |
| Node.js | ≥ 20 | 仅构建内嵌 Web UI（`src/webapp`）时需要 |
| bililive-go 源码 | ef71711 (v0.8.2) | `git clone https://github.com/bililive-go/bililive-go.git` |

## 步骤

### 1. 应用本仓库的修改

把 [`patch/`](../patch/) 下的文件按相对路径覆盖到 bililive-go 源码树：

```bash
cd bililive-go
cp -r /path/to/bililive-go-termux/patch/src/* src/
```

- `src/pkg/btoolssrv/`（新增包）：内置抖音签名服务
- `src/tools/tools_android.go`（覆盖）：Android 启动逻辑
- `src/cmd/btoolsdebug/`（新增）：命令行调试工具（可选）

### 2. 构建 Web UI（已有构建产物可跳过）

```bash
cd src/webapp && npm install && npm run build && cd ../..
```

### 3. 交叉编译

```bash
# 设置 NDK 路径（按实际安装位置调整）
export ANDROID_NDK_HOME=/path/to/android-ndk-r27

ARCH=arm64 bash build-go-android.sh /path/to/output/bililive-go-android-arm64
```

产物为 bionic 链接的静态 AArch64 可执行文件（ELF 依赖仅 `liblog`/`libdl`/`libc`），在 Termux 中 `chmod 755` 后即可直接运行。

### 4. 验证

```bash
$NDK/toolchains/llvm/prebuilt/*/bin/llvm-readelf -h bililive-go-android-arm64 | grep -E "Type|Machine"
# 应显示: Type: EXEC (可执行文件)  Machine: AArch64
```

部署到手机后检查内置签名服务：

```bash
curl -s -H "Authorization: Basic YTph" \
  "http://127.0.0.1:18110/bgo/live-info?platform=douyin&roomId=<房间号>"
```

## 发布产物清单

| 文件 | 说明 |
|---|---|
| `bililive-go-android-arm64` | 主程序（内嵌 Web UI + btoolssrv 签名服务） |
| `bililive-scheduler-linux-arm64` | 定时录制组件（上游静态编译，bionic 可直接运行） |

## 已知注意事项

- 上游 `remote-tools-config.json` 中没有 android 平台的工具条目，因此 Android 构建跳过 remotetools（`/tools/` 显示未就绪属预期）
- `/proc/net/tcp` 在部分 Android 内核上不完整，端口检测请用 `netstat -tln`（`bl` 脚本已处理）
- 抖音风控策略可能随时间变化，签名算法失效时优先更新 `pkg/btoolssrv/abogus.go` 中的参数序与 UA
