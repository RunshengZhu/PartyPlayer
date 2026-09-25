#!/usr/bin/env bash
# 交叉编译产出各平台单可执行文件，并打包为 zip（内含使用说明）。
# zip 可保留可执行位——浏览器直接下载裸二进制会丢失 x 权限导致 macOS 上无法打开。
# 产物：dist/party-player-<平台>-<版本>.zip 与 Android APK
# 用法: ./build.sh   （或 bash build.sh）
set -euo pipefail
cd "$(dirname "$0")"

# 可传第 1 个参数强制版本号（用于固定链接直接替换发布）：./build.sh v0.1.0-20260925-0731
BASE_VERSION="v$(grep -E '^const Version' main.go | sed 's/.*"\(.*\)".*/\1/')"
if [ "$#" -ge 1 ]; then VERSION="$1"; else VERSION="${BASE_VERSION}-$(date +%Y%m%d-%H%M)"; fi
BUILD_TIME="$(date +%Y-%m-%dT%H:%M:%S)"
LDFLAGS="-s -w -X main.BuildVersion=$VERSION -X main.BuildTime=$BUILD_TIME"
mkdir -p dist

# 使用说明（随每个 zip 分发）
cat > "dist/README.txt" <<EOF
舞会音乐播放器 Party Player ${VERSION}（构建于 ${BUILD_TIME}）
================================================================

这是一个免安装的舞会音乐播放器：把音乐按舞种分文件夹存放，
随机生成歌单、限时播放、自动渐弱并切换下一首。

【macOS】（arm64 = Apple Silicon 芯片；amd64 = Intel 芯片）
  1. 解压 zip，得到 party-player-macos-* 文件
  2. 打开「终端」，cd 到解压目录，执行：
       chmod +x party-player-macos-*
       ./party-player-macos-*
     之后会自动打开浏览器播放页面
  3. 若提示"无法验证开发者"：在文件上右键 →「打开」→ 再点「打开」；
     或先执行:  xattr -d com.apple.quarantine party-player-macos-*

【Windows】（amd64 = 64 位；386 = 32 位老系统）
  1. 解压 zip，双击 party-player-windows-*.exe
  2. 若 SmartScreen 提示"已保护你的电脑"：点「更多信息」→「仍要运行」

【开始使用】
  1. 在自动打开的网页里点「📁 选择目录」，选择你的曲库目录：
     目录下每个子文件夹 = 一个舞种（如 华尔兹/、恰恰/），里面放 mp3/flac/wav 等音频
  2. 左侧为每个舞种设置数量 → 「🎲 生成歌单」→ 双击任意歌曲开始播放
  3. 「⚙ 设置」：单曲限时秒数（到时自动渐弱切下一首）、渐弱时长、歌曲间隔
  4. 「📋 歌单管理」：保存/载入歌单；「📲 手机接入」：手机扫码看同一页面

【数据位置】配置与歌单保存在：
  macOS: ~/Library/Application Support/PartyPlayer
  Windows: %AppData%\\PartyPlayer

【常用参数】-port 端口   -lib 曲库目录   -listen 0.0.0.0 允许手机访问   -version 版本
EOF

build() {
    local goos=$1 goarch=$2 out=$3 extra=${4:-}
    echo "==> $out"
    GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags "$LDFLAGS$extra" -o "dist/$out" .
    (cd dist && zip -q "$out.zip" "$out" README.txt && rm "$out")
}

build darwin arm64 "party-player-macos-arm64-$VERSION"
build darwin amd64 "party-player-macos-amd64-$VERSION"
build windows amd64 "party-player-windows-amd64-$VERSION.exe" " -H=windowsgui"
build windows 386  "party-player-windows-386-$VERSION.exe" " -H=windowsgui"
build linux amd64  "party-player-linux-amd64-$VERSION"

echo
echo "版本: ${VERSION}（${BUILD_TIME}）"
echo "产物列表:"
ls -lh dist/ | grep "$VERSION"
