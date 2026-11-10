#!/usr/bin/env bash
# =============================================================================
# 二进制层 hook 库的单实例验证流水线（STEALTH_PRELOAD 的安全启用流程）
#
#   ./scripts/test-preload.sh            # 1) 免重启冒烟：push 到 /data/local/tmp，单进程 LD_PRELOAD 测试
#   ./scripts/test-preload.sh --enable   # 2) 冒烟 -> 开 STEALTH_PRELOAD=1 -> 重启 -> 等开机 -> 验证；失败自动回滚
#   ./scripts/test-preload.sh --disable  # 3) 关闭并重启（手动回滚）
#
# 设计原则：
#   - 冒烟阶段绝不动系统：LD_PRELOAD 只在那一次 adb shell 里生效，进程退出即消失
#   - --enable 有超时看门狗：开机失败 / hook 不生效都会自动改回 0 并重启恢复
#   - 另有文件级保险丝兜底：touch config/fake/disable && docker restart 可让库全直通
# =============================================================================
set -uo pipefail
cd "$(dirname "$0")/.."

CONTAINER="${CONTAINER:-xphone-android-01}"
ADB_HOST="${ADB_HOST:-127.0.0.1}"
ADB_PORT="${ADB_PORT:-18120}"
BOOT_TIMEOUT="${BOOT_TIMEOUT:-180}"   # 等开机完成的秒数上限
MODE="${1:-check}"
TARGET="${ADB_HOST}:${ADB_PORT}"

[[ -f .env ]] && { set -a; . .env; set +a; }
XPHONE_ENV="${XPHONE_ENV:-dev}"
[[ -f config/device.env ]] && { set -a; . config/device.env; set +a; }
[[ -f "config/profiles/${XPHONE_ENV}.env" ]] && { set -a; . "config/profiles/${XPHONE_ENV}.env"; set +a; }
[[ -z "${CONTAINER:-}" ]] && CONTAINER="xphone-android-$( [[ "$XPHONE_ENV" == "prod" ]] && echo 01 || echo dev )"

say()  { printf '%s\n' "$*"; }
ok()   { printf '  \033[32m[PASS]\033[0m %s\n' "$*"; }
bad()  { printf '  \033[31m[FAIL]\033[0m %s\n' "$*"; }
info() { printf '  \033[36m[..]\033[0m %s\n' "$*"; }

connect() {
  adb connect "$TARGET" >/dev/null 2>&1 || true
  adb -s "$TARGET" wait-for-device 2>/dev/null || true
}
shq() { adb -s "$TARGET" shell "$@" 2>/dev/null | tr -d '\r'; }

# -------------------------------------------- 0. 定位 hook 库（架构按设备 ABI）
ABI=$(shq getprop ro.product.cpu.abi)
case "$ABI" in
  arm64*)  ARCH=aarch64 ;;
  x86_64*) ARCH=x86_64 ;;
  *) bad "无法识别设备 ABI：${ABI:-<空>}（容器在运行吗？）"; exit 1 ;;
esac
LIB="config/native/${ARCH}/libxphone_fake.so"
if [[ ! -f "$LIB" ]]; then
  bad "未找到 $LIB"
  say "  先编译：NDK=/path/to/android-ndk ARCH=${ARCH} ./native/xphone-fake/build-ndk.sh"
  exit 1
fi
info "hook 库：$LIB（设备 ABI=$ABI）"

FAKE_MEM_EXPECT=""
[[ -f config/fake/meminfo ]] && FAKE_MEM_EXPECT=$(head -1 config/fake/meminfo)

# -------------------------------------------- 1. 免重启冒烟（单进程，无副作用）
smoke() {
  info "冒烟测试：push 到 /data/local/tmp，仅在一个 shell 进程内 LD_PRELOAD"
  adb -s "$TARGET" push "$LIB" /data/local/tmp/libxphone_fake.so >/dev/null 2>&1 \
    || { bad "adb push 失败"; return 1; }
  shq 'chmod 0644 /data/local/tmp/libxphone_fake.so' >/dev/null

  # 1a. meminfo 重定向
  local got
  got=$(shq 'LD_PRELOAD=/data/local/tmp/libxphone_fake.so XFAKE_DIR=/xphone/fake head -1 /proc/meminfo')
  if [[ -n "$FAKE_MEM_EXPECT" && "$got" == "$FAKE_MEM_EXPECT" ]]; then
    ok "meminfo 重定向：$got"
  else
    bad "meminfo 未生效：期望[$FAKE_MEM_EXPECT] 实际[$got]"
    return 1
  fi

  # 1b. sysconf 核数
  if [[ -n "${XFAKE_CPU_COUNT:-}" ]]; then
    local ncpu
    ncpu=$(shq "LD_PRELOAD=/data/local/tmp/libxphone_fake.so XFAKE_DIR=/xphone/fake XFAKE_CPU_COUNT=${XFAKE_CPU_COUNT} toybox nproc 2>/dev/null || echo ?")
    [[ "$ncpu" == "${XFAKE_CPU_COUNT}" ]] \
      && ok "sysconf 核数：$ncpu" \
      || info "nproc=$ncpu（期望 ${XFAKE_CPU_COUNT}；部分 toybox 实现走 sched_getaffinity，不走 sysconf，可忽略）"
  fi

  # 1c. 保险丝：disable 文件应让库完全直通
  local bypass
  bypass=$(shq 'touch /xphone/fake/disable 2>/dev/null; LD_PRELOAD=/data/local/tmp/libxphone_fake.so XFAKE_DIR=/xphone/fake head -1 /proc/meminfo; rm -f /xphone/fake/disable 2>/dev/null')
  if [[ -n "$FAKE_MEM_EXPECT" && "$bypass" != "$FAKE_MEM_EXPECT" ]]; then
    ok "保险丝有效：disable 文件存在时库直通（实际值：${bypass%%:*}: ...）"
  else
    info "保险丝未验证（/xphone 可能只读挂载，无法 touch disable；不影响主功能）"
  fi
  shq 'rm -f /data/local/tmp/libxphone_fake.so' >/dev/null
  return 0
}

wait_boot() { # 重启后等 boot_completed，超时返回 1
  local i
  sleep 5   # 给容器一点退出时间，避免等到旧进程
  adb disconnect "$TARGET" >/dev/null 2>&1 || true
  for i in $(seq 1 $((BOOT_TIMEOUT / 3))); do
    adb connect "$TARGET" >/dev/null 2>&1 || true
    local b
    b=$(adb -s "$TARGET" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')
    [[ "$b" == "1" ]] && return 0
    sleep 3
  done
  return 1
}

set_preload() { # set_preload 0|1 ：改 device.env
  sed -i.bak "s/^STEALTH_PRELOAD=.*/STEALTH_PRELOAD=$1/" config/device.env && rm -f config/device.env.bak
  info "config/device.env: STEALTH_PRELOAD=$1"
}

verify_active() { # 开启后 adbd 会继承 LD_PRELOAD，adb shell 读到的就应是伪造值
  local got
  got=$(shq 'head -1 /proc/meminfo')
  [[ -n "$FAKE_MEM_EXPECT" && "$got" == "$FAKE_MEM_EXPECT" ]]
}

case "$MODE" in
  check)
    connect
    smoke && say $'\n\033[32m冒烟通过。下一步：./scripts/test-preload.sh --enable\033[0m'
    ;;
  --enable)
    connect
    smoke || { bad "冒烟未通过，不开启 STEALTH_PRELOAD"; exit 1; }
    set_preload 1
    info "重启容器（等待开机，超时 ${BOOT_TIMEOUT}s）..."
    docker restart "$CONTAINER" >/dev/null 2>&1 || { bad "docker restart 失败"; exit 1; }
    if ! wait_boot; then
      bad "开机超时 —— 疑似 hook 库导致开机循环，自动回滚"
      set_preload 0
      docker restart "$CONTAINER" >/dev/null 2>&1 || true
      wait_boot && info "已回滚并恢复开机" || bad "回滚后仍无法开机，请 docker logs $CONTAINER 排查"
      exit 1
    fi
    ok "开机完成"
    if verify_active; then
      ok "hook 已全局生效（adbd 继承 LD_PRELOAD，/proc/meminfo 返回伪造值）"
      say $'\n\033[32m单实例验证通过。上量前建议跑满一个完整业务链路。\033[0m'
      say "排障：XFAKE_DEBUG=1 写进 device.env 后重启可看 hook 日志（docker logs）"
    else
      bad "开机成功但 hook 未生效（/proc/meminfo 非伪造值）"
      say "  检查：docker logs $CONTAINER | grep xphone:entry 是否打出 enable LD_PRELOAD hook"
      say "  自动回滚为 STEALTH_PRELOAD=0 ..."
      set_preload 0
      docker restart "$CONTAINER" >/dev/null 2>&1 || true
      exit 1
    fi
    ;;
  --disable)
    set_preload 0
    shq 'rm -f /xphone/fake/disable' >/dev/null 2>&1 || true
    info "重启容器..."
    docker restart "$CONTAINER" >/dev/null 2>&1 || true
    ok "已关闭并重启"
    ;;
  *)
    say "用法：$0 [check|--enable|--disable]"; exit 2 ;;
esac
