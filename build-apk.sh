#!/usr/bin/env bash
# 构建 Android APK（arm64）。
# 依赖：Go、JDK 17+、Android SDK（平台 35 + build-tools 35）、Gradle 8.9+（或使用 gradle wrapper）。
# 产物带时间版本号：dist/party-player-android-arm64-<版本>.apk
# 用法: ./build-apk.sh
set -euo pipefail
cd "$(dirname "$0")"

# 可传第 1 个参数强制版本号（用于固定链接直接替换发布）：./build.sh v0.1.0-20260925-0731
BASE_VERSION="v$(grep -E '^const Version' main.go | sed 's/.*"\(.*\)".*/\1/')"
if [ "$#" -ge 1 ]; then VERSION="$1"; else VERSION="${BASE_VERSION}-$(date +%Y%m%d-%H%M)"; fi
BUILD_TIME="$(date +%Y-%m-%dT%H:%M:%S)"

# ---- 0. 环境探测 ----
if [ -z "${ANDROID_HOME:-}" ] && [ -z "${ANDROID_SDK_ROOT:-}" ]; then
    for cand in "$HOME/Library/Android/sdk" /opt/homebrew/share/android-commandlinetools; do
        if [ -d "$cand" ]; then
            export ANDROID_HOME="$cand"
            break
        fi
    done
fi
if [ -n "${ANDROID_HOME:-}" ] && [ ! -f android/local.properties ]; then
    echo "sdk.dir=$ANDROID_HOME" > android/local.properties
fi
[ -n "${ANDROID_HOME:-}" ] || { echo "未找到 Android SDK，请设置 ANDROID_HOME"; exit 1; }

# Homebrew 的 openjdk@17 未注册到系统，手动指定 JAVA_HOME
if [ -z "${JAVA_HOME:-}" ]; then
    for cand in "$(brew --prefix 2>/dev/null)/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home" \
                "$(/usr/libexec/java_home 2>/dev/null)"; do
        if [ -n "$cand" ] && [ -x "$cand/bin/java" ]; then
            export JAVA_HOME="$cand"
            break
        fi
    done
fi
[ -n "${JAVA_HOME:-}" ] || { echo "未找到 JDK 17+，请设置 JAVA_HOME"; exit 1; }
export PATH="$JAVA_HOME/bin:$PATH"

# ---- 1. 编译 Go 服务器为 android/arm64，放入 jniLibs ----
echo "==> 编译 Go 服务器 (android/arm64) $VERSION"
mkdir -p android/app/src/main/jniLibs/arm64-v8a
CGO_ENABLED=0 GOOS=android GOARCH=arm64 \
    go build -trimpath -ldflags "-s -w -X main.BuildVersion=$VERSION -X main.BuildTime=$BUILD_TIME" \
    -o android/app/src/main/jniLibs/arm64-v8a/libpartyplayer.so .

# ---- 2. Gradle 构建（versionName 注入时间版本号）----
cd android
if [ -x "./gradlew" ]; then
    GRADLE_CMD="./gradlew"
else
    GRADLE_CMD="gradle"
fi
echo "==> Gradle 构建 APK ($GRADLE_CMD)"
$GRADLE_CMD --no-daemon -PappVersionName="$VERSION" assembleDebug

APK="app/build/outputs/apk/debug/app-debug.apk"
cd ..
mkdir -p dist
cp "android/$APK" "dist/party-player-android-arm64-$VERSION.apk"
echo
echo "APK 产物: dist/party-player-android-arm64-${VERSION}.apk（${BUILD_TIME}）"
ls -lh "dist/party-player-android-arm64-$VERSION.apk"
