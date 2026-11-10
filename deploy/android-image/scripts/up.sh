#!/usr/bin/env bash
# =============================================================================
# 按环境启动容器：dev（本地测试） / prod（Linux 生产）
#
#   ./scripts/up.sh dev            # 开发测试实例（弱加固、省资源、重启不自动拉起）
#   ./scripts/up.sh prod           # 生产实例（预检 Linux+binder+arm64，真机化全开）
#   ./scripts/up.sh prod --gpu     # 额外叠加 /dev/dri 直通
#   ./scripts/up.sh dev --build    # 附带重建镜像（其余参数透传给 docker compose up）
#
# 做的事：写 .env 的 XPHONE_ENV/DATA_DIR -> 预检 -> 组合 compose 文件 -> up -d
# =============================================================================
set -uo pipefail
cd "$(dirname "$0")/.."

ENV_NAME="${1:-}"
shift 2>/dev/null || true
case "$ENV_NAME" in
  dev|prod) ;;
  *) echo "用法：$0 <dev|prod> [--gpu] [其他 docker compose up 参数]"; exit 2 ;;
esac

GPU=0
EXTRA=()
for a in "$@"; do
  case "$a" in --gpu) GPU=1 ;; *) EXTRA+=("$a") ;; esac
done

ok()   { printf '  \033[32m[OK]\033[0m   %s\n' "$*"; }
warn() { printf '  \033[33m[WARN]\033[0m %s\n' "$*"; }
die()  { printf '  \033[31m[FAIL]\033[0m %s\n' "$*"; exit 1; }

# ------------------------------------------------- 1. 生成/更新 .env 的环境变量
if [[ ! -f .env ]]; then
  cp .env.example .env && echo "已由 .env.example 生成 .env"
fi
set_env_var() { # set_env_var KEY VALUE
  if grep -q "^$1=" .env; then
    sed -i.bak "s|^$1=.*|$1=$2|" .env && rm -f .env.bak
  else
    printf '%s=%s\n' "$1" "$2" >> .env
  fi
}
set_env_var XPHONE_ENV "$ENV_NAME"
# 两个环境用不同的 /data 目录，避免 dev 的脏数据污染生产实例
if ! grep -q "^DATA_DIR=" .env || [[ "$ENV_NAME" == "dev" ]]; then
  case "$ENV_NAME" in
    dev)  set_env_var DATA_DIR ./runtime/data-dev ;;
    prod) grep -q "^DATA_DIR=" .env || set_env_var DATA_DIR /var/lib/xphone/data ;;
  esac
fi
ok ".env: XPHONE_ENV=${ENV_NAME}"

# ------------------------------------------------------------- 2. 环境预检
HOST_OS="$(uname -s)"
HOST_ARCH="$(uname -m)"
echo "  宿主：${HOST_OS} / ${HOST_ARCH}"

if [[ "$ENV_NAME" == "prod" ]]; then
  [[ "$HOST_OS" == "Linux" ]] \
    || die "prod 必须在 Linux 上运行（当前 ${HOST_OS}）。macOS 的 Docker Desktop 内核没有 binder，容器起不来。"
  # binder：/dev/binder 存在，或 binderfs 已挂载，或模块已加载
  if ! { [[ -e /dev/binder ]] || [[ -d /dev/binderfs ]] || lsmod 2>/dev/null | grep -q binder_linux; }; then
    die "未检测到 binder（/dev/binder 缺失）。先执行：sudo ./scripts/host-setup.sh --persist"
  fi
  ok "binder 就绪"
  [[ "$HOST_ARCH" == "aarch64" || "$HOST_ARCH" == "arm64" ]] \
    || warn "宿主为 ${HOST_ARCH}：抗检测场景必须是 arm64（x86_64 的 /proc/cpuinfo 会暴露），真机化效果打折"
  [[ -f config/native/aarch64/libxphone_fake.so ]] \
    || warn "config/native/aarch64/libxphone_fake.so 不存在，但 profiles/prod.env 里 STEALTH_PRELOAD=1（会打 WARN 并跳过）"
else
  [[ "$HOST_OS" == "Darwin" ]] && warn "macOS 不能运行本容器（无 binder）：当前只能用于构建/编辑，dev 实例也要跑在 Linux 上"
fi

# ------------------------------------------------------- 3. 组合 compose 文件
FILES=(-f docker-compose.yml -f "docker-compose.${ENV_NAME}.yml")
[[ $GPU -eq 1 ]] && FILES+=(-f docker-compose.gpu.yml)

echo "== docker compose up -d (${ENV_NAME}${GPU:+,gpu}) =="
docker compose "${FILES[@]}" up -d "${EXTRA[@]}" \
  || die "compose up 失败（docker compose logs 查看原因）"

cat <<EOF

== next ==
  docker compose ${FILES[*]} ps          # 等 healthcheck = healthy
  ./scripts/verify.sh                    # 验证清单
EOF
[[ "$ENV_NAME" == "prod" ]] && cat <<EOF
  ./scripts/stealth-audit.sh --verbose   # 真机化自检
  ./scripts/test-preload.sh --enable     # 若 hook 库尚未启用，走这条流水线
EOF
exit 0
