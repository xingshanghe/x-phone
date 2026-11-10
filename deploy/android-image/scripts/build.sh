#!/usr/bin/env bash
# =============================================================================
# 可复现构建：x-phone 定制 Android 镜像
#
#   ./scripts/build.sh                       # 常规构建
#   ./scripts/build.sh --no-cache            # 忽略缓存
#   ./scripts/build.sh --flatten             # 构建后 export/import 扁平化（真正瘦身）
#   ./scripts/build.sh --tag 14-custom-v2    # 指定 tag
#
# 可选环境变量：
#   ANDROID_TAG=14.0.0-latest   基础镜像 tag
#   CJK_FONT_URL=<直链>         构建期下载中文字体（留空则用 config/fonts/ 手放的文件）
#   BASE_IMAGE=<镜像>           直接指定基础镜像（例如预置了 Magisk 的 redroid:14.0.0_magisk）
#   PLATFORM=linux/arm64        显式指定目标平台（跨架构构建需 QEMU binfmt，见 README 6.1）
#   --env dev|prod              环境分层：dev 镜像 tag 自动加 -dev 后缀，避免与生产镜像混用
#
# 构建机选择（详见 README 6.1）：
#   镜像里的 RUN 用的是 Android 自带的 /system/bin/sh，也就是说构建过程要能
#   「执行」镜像架构的二进制 —— 构建机 CPU 架构必须与目标镜像一致才稳。
#   Apple Silicon Mac -> 直接构建 linux/arm64（与抗检测目标一致，推荐）
#   Intel Mac        -> 构建 linux/amd64；跨编 arm64 要靠 QEMU 模拟 bionic，不推荐
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")/.."

IMAGE="${IMAGE:-xphone/android}"
TAG="${TAG:-14-custom}"
ANDROID_TAG="${ANDROID_TAG:-14.0.0-latest}"
BASE_IMAGE="${BASE_IMAGE:-}"
CJK_FONT_URL="${CJK_FONT_URL:-}"
PLATFORM="${PLATFORM:-}"      # 例：linux/arm64 / linux/amd64；留空则跟随构建机
FLATTEN=0
BUILD_ARGS=()
XPHONE_ENV="${XPHONE_ENV:-}"  # dev|prod，与运行时 config/profiles/<env>.env 对应
TAG_EXPLICIT=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-cache) BUILD_ARGS+=(--no-cache); shift ;;
    --flatten)  FLATTEN=1; shift ;;
    --tag)      TAG="$2"; TAG_EXPLICIT=1; shift 2 ;;
    --env)      XPHONE_ENV="$2"; shift 2 ;;
    *)          echo "unknown arg: $1"; exit 1 ;;
  esac
done

# 环境分层：只影响镜像 tag（dev 自动加 -dev 后缀，避免与生产镜像混用）。
# 行为差异全部在 config/profiles/<env>.env，构建产物本身是同一份 Dockerfile。
if [[ -n "$XPHONE_ENV" ]]; then
  case "$XPHONE_ENV" in
    dev|prod) ;;
    *) echo "--env 只支持 dev|prod（当前：$XPHONE_ENV）"; exit 1 ;;
  esac
  [[ "$XPHONE_ENV" == "dev" && $TAG_EXPLICIT -eq 0 && "$TAG" != *-dev ]] && TAG="${TAG}-dev"
  if [[ "$XPHONE_ENV" == "prod" ]]; then
    case "$(uname -m)" in
      aarch64|arm64) : ;;
      *) echo "  ⚠ prod 建议用 arm64 构建机（当前 $(uname -m)）：x86_64 上真机化无效（/proc/cpuinfo 暴露 Intel/AMD）" ;;
    esac
  fi
fi

HOST_ARCH="$(uname -m)"
if [[ -z "$PLATFORM" ]]; then
  case "$HOST_ARCH" in
    arm64|aarch64) PLATFORM_DEFAULT="linux/arm64" ;;
    x86_64|amd64)  PLATFORM_DEFAULT="linux/amd64" ;;
    *)             PLATFORM_DEFAULT="" ;;
  esac
  echo "== host arch : ${HOST_ARCH} -> 目标平台 ${PLATFORM_DEFAULT:-<跟随默认>} =="
  [[ "$HOST_ARCH" == "x86_64" && "${ANDROID_TAG}" == *arm* ]] \
    && echo "  ⚠ x86 构建机 + arm 基础镜像：RUN 需 QEMU 模拟 Android 二进制，可能失败"
else
  echo "== host arch : ${HOST_ARCH}，指定目标平台 ${PLATFORM} =="
  [[ "$PLATFORM" == "linux/arm64" && "$HOST_ARCH" == "x86_64" ]] \
    && echo "  ⚠ 跨架构（x86 -> arm64）：RUN 需 QEMU 模拟 bionic 二进制，失败率高，建议换 arm64 构建机"
  BUILD_ARGS+=(--platform "$PLATFORM")
fi

echo "== build =="
echo "  image       : ${IMAGE}:${TAG}"
echo "  env         : ${XPHONE_ENV:-<未指定>}"
echo "  base        : ${BASE_IMAGE:-redroid/redroid:${ANDROID_TAG}}"
echo "  cjk font    : ${CJK_FONT_URL:-<skip; use config/fonts/>}"

# 空目录占位，保证 COPY 不会因为目录不存在而失败
mkdir -p config/apps/system config/apps/user config/fonts config/init.d config/props
touch config/apps/system/.gitkeep config/apps/user/.gitkeep config/fonts/.gitkeep config/init.d/.gitkeep

DOCKER_BUILDKIT=1 docker build \
  "${BUILD_ARGS[@]}" \
  --build-arg ANDROID_TAG="${ANDROID_TAG}" \
  --build-arg BASE_IMAGE="${BASE_IMAGE}" \
  --build-arg CJK_FONT_URL="${CJK_FONT_URL}" \
  --build-arg BUILDKIT_INLINE_CACHE=1 \
  --label "xphone.build.time=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t "${IMAGE}:${TAG}" \
  .

echo
echo "== image size =="
docker images "${IMAGE}:${TAG}" --format '{{.Repository}}:{{.Tag}}  {{.Size}}'

if [[ $FLATTEN -eq 1 ]]; then
  echo
  echo "== flatten (export/import) =="
  echo "  说明：删除文件在 overlayfs 里只会产生 whiteout，镜像体积不会下降；"
  echo "        export/import 会压平所有层，让精简真正生效（代价：丢失构建缓存与层复用）。"
  TMP_C=$(docker create "${IMAGE}:${TAG}" true)
  docker export "${TMP_C}" \
    | docker import \
        --change 'ENTRYPOINT ["/opt/xphone/entrypoint.sh"]' \
        --change 'CMD []' \
        --change 'EXPOSE 5555' \
        --change 'ENV XPHONE_IMAGE_DIR=/opt/xphone' \
        --change 'ENV XPHONE_CONFIG_DIR=/xphone' \
        - "${IMAGE}:${TAG}-flat"
  docker rm "${TMP_C}" >/dev/null
  docker images "${IMAGE}:${TAG}-flat" --format '{{.Repository}}:{{.Tag}}  {{.Size}}'
fi

cat <<EOF

== next ==
  cd deploy/android-image
  cp .env.example .env         # 按需修改端口与目录
  docker compose up -d
  ./scripts/verify.sh
EOF
