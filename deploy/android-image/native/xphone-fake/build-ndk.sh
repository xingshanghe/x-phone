#!/usr/bin/env bash
# =============================================================================
# 用 Android NDK 编译二进制层 hook 库（无需 AOSP 源码树）
#
#   NDK=~/Library/Android/sdk/ndk/27.0.12077973 ./native/xphone-fake/build-ndk.sh
#
# 产物：config/native/aarch64/libxphone_fake.so
#       （entrypoint 会在启动时把它复制到 /system/lib64/，配合 STEALTH_PRELOAD=1 生效）
#
# ★ 架构必须与镜像一致：arm64 镜像编 aarch64，x86_64 镜像编 x86_64（--arch 指定）
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")"

NDK="${NDK:-${ANDROID_NDK_HOME:-}}"
ARCH="${ARCH:-aarch64}"
API="${API:-34}"
OUT_DIR="../../config/native/${ARCH}"

if [[ -z "$NDK" || ! -d "$NDK" ]]; then
  echo "需要 NDK 路径：NDK=/path/to/android-ndk ./build-ndk.sh"
  exit 1
fi

case "$(uname -s)-${ARCH}" in
  Darwin-aarch64) HOST=darwin-x86_64; TRIPLE=aarch64-linux-android ;;
  Darwin-x86_64)  HOST=darwin-x86_64; TRIPLE=x86_64-linux-android  ;;
  Linux-aarch64)  HOST=linux-x86_64;  TRIPLE=aarch64-linux-android ;;
  Linux-x86_64)   HOST=linux-x86_64;  TRIPLE=x86_64-linux-android  ;;
  *) echo "不支持的组合: $(uname -s) ${ARCH}"; exit 1 ;;
esac

# macOS 的 NDK 只有 darwin-x86_64 一套预编译工具链（官方 host tag 如此，含最新版）。
# Apple Silicon 上靠 Rosetta 2 运行；没装就先装：
#   softwareupdate --install-rosetta --agree-to-license
if [[ "$(uname -s)" == "Darwin" && "$(uname -m)" == "arm64" ]]; then
  /usr/bin/pgrep -q oahd >/dev/null 2>&1 \
    || echo "提示：Apple Silicon 上 NDK 工具链为 x86_64，需 Rosetta 2：softwareupdate --install-rosetta"
fi

CC="${NDK}/toolchains/llvm/prebuilt/${HOST}/bin/${TRIPLE}${API}-clang"
[[ -x "$CC" ]] || { echo "找不到编译器: $CC（检查 NDK 版本与 API）"; exit 1; }

mkdir -p "$OUT_DIR"
"$CC" -shared -fPIC -O2 -Wall -std=c11 \
  -o "${OUT_DIR}/libxphone_fake.so" xphone_fake.c -ldl

echo "产物：${OUT_DIR}/libxphone_fake.so"
ls -l "${OUT_DIR}/libxphone_fake.so"
${TRIPLE}-readelf -d "${OUT_DIR}/libxphone_fake.so" 2>/dev/null | head -5 || true

cat <<EOF

下一步（单实例验证流水线，勿跳步）：
  1) ./scripts/test-preload.sh           # 免重启冒烟：单进程 LD_PRELOAD 验证
  2) ./scripts/test-preload.sh --enable  # 开 STEALTH_PRELOAD=1 + 重启 + 等开机 + 验证；失败自动回滚
  3) ./scripts/verify.sh                 # 第 9 节校验 hook 是否全局生效
紧急恢复：touch config/fake/disable && docker restart <容器>（保险丝让库全直通）
EOF
