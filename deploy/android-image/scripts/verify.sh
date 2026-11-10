#!/usr/bin/env bash
# =============================================================================
# 验证清单：容器可启动 / ADB 可连接 / 预装应用生效 / 定制参数生效
#
#   ./scripts/verify.sh                 # 用默认参数（读 config/device.env 作为期望值）
#   ./scripts/verify.sh --shot /tmp/s.png   # 额外截一张屏，确认渲染管线正常
#
# 期望值来源：config/device.env（与本目录 docker-compose 使用的是同一份）
# =============================================================================
set -uo pipefail
cd "$(dirname "$0")/.."

CONTAINER="${CONTAINER:-xphone-android-01}"
ADB_HOST="${ADB_HOST:-127.0.0.1}"
ADB_PORT="${ADB_PORT:-18120}"
SHOT=""
[[ "${1:-}" == "--shot" ]] && SHOT="${2:-/tmp/xphone-screen.png}"

PASS=0; FAIL=0
ok()   { printf '  \033[32m[PASS]\033[0m %s\n' "$*"; PASS=$((PASS+1)); }
bad()  { printf '  \033[31m[FAIL]\033[0m %s\n' "$*"; FAIL=$((FAIL+1)); }
warn() { printf '  \033[33m[WARN]\033[0m %s\n' "$*"; }
h()    { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

# 载入期望值：device.env（基线）+ profiles/<XPHONE_ENV>.env（环境覆盖），顺序必须与容器内一致
[[ -f .env ]] && { set -a; . .env; set +a; }
XPHONE_ENV="${XPHONE_ENV:-dev}"
if [[ -f config/device.env ]]; then set -a; . config/device.env; set +a; fi
if [[ -f "config/profiles/${XPHONE_ENV}.env" ]]; then set -a; . "config/profiles/${XPHONE_ENV}.env"; set +a; fi
echo "  环境：${XPHONE_ENV}"

# 容器名默认按环境区分（dev=xphone-android-dev / prod=xphone-android-01）
[[ -z "${CONTAINER:-}" ]] && CONTAINER="xphone-android-$( [[ "$XPHONE_ENV" == "prod" ]] && echo 01 || echo dev )"

TARGET="${ADB_HOST}:${ADB_PORT}"
adb() { command adb "$@"; }

h "1. 容器可启动"
state=$(docker inspect -f '{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo missing)
if [[ "$state" == "running" ]]; then ok "容器 ${CONTAINER} 状态 running"; else bad "容器状态=${state}（用 docker logs ${CONTAINER} 查原因）"; fi
health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}n/a{{end}}' "${CONTAINER}" 2>/dev/null || echo n/a)
[[ "$health" == "healthy" || "$health" == "n/a" ]] && ok "健康检查=${health}" || warn "健康检查=${health}（容器可能仍在首次开机）"

h "2. ADB 可连接"
adb disconnect "${TARGET}" >/dev/null 2>&1 || true
adb connect "${TARGET}" >/dev/null 2>&1 || true
for i in $(seq 1 30); do
  st=$(adb -s "${TARGET}" get-state 2>/dev/null || echo unknown)
  [[ "$st" == "device" ]] && break
  sleep 2
done
if [[ "$st" == "device" ]]; then ok "adb 状态 device（${TARGET}）"; else bad "adb 状态=${st}"; fi

h "3. 开机完成 & root 权限"
boot=$(adb -s "${TARGET}" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')
[[ "$boot" == "1" ]] && ok "sys.boot_completed=1" || bad "sys.boot_completed=${boot:-<空>}"
uid=$(adb -s "${TARGET}" shell id -u 2>/dev/null | tr -d '\r')
case "${ROOT_MODE:-adb}" in
  adb)
    if [[ "$uid" == "0" ]]; then ok "adb shell 为 root（uid=0）"; else bad "adb shell uid=${uid}（ROOT_MODE=adb 未生效？）"; fi ;;
  magisk)
    # 属性面伪装成 user 版，adb shell 本来就该是非 root，root 由 su 提供
    suid=$(adb -s "${TARGET}" shell "su -c id -u" 2>/dev/null | tr -d '\r' | tail -1)
    if [[ "$suid" == "0" ]]; then ok "Magisk su 可用（adb shell uid=${uid}，属性面保持 user）"
    else warn "su 不可用（输出=${suid:-<空>}）：基础镜像是否已预置 Magisk？没有就先把 ROOT_MODE 改回 adb"; fi ;;
  none)
    if [[ "$uid" == "0" ]]; then warn "ROOT_MODE=none 但 adb shell 是 root（属性未生效）"; else ok "非 root（符合 ROOT_MODE=none，uid=${uid}）"; fi ;;
esac
enf=$(adb -s "${TARGET}" shell getenforce 2>/dev/null | tr -d '\r')
if [[ "${SELINUX:-enforcing}" == "enforcing" ]]; then
  if [[ "$enf" == "Enforcing" ]]; then ok "SELinux=Enforcing";
  else warn "SELinux=${enf:-<空>}（期望 Enforcing；若日志有 chcon WARN 说明已自动降级，属预期）"; fi
else
  warn "SELinux=${enf:-<空>}（device.env 配置为 permissive；真机化场景建议 enforcing）"
fi

h "4. 定制参数生效（设备身份）"
chk() { # chk <属性> <期望值> <说明>
  local act
  act=$(adb -s "${TARGET}" shell getprop "$1" 2>/dev/null | tr -d '\r')
  if [[ -n "$2" && "$act" == "$2" ]]; then ok "$3：$1=$act"; 
  elif [[ -z "$2" ]]; then warn "跳过 $1（未配置期望值）";
  else bad "$3：$1 期望[$2] 实际[${act:-<空>}]"; fi
}
chk ro.product.model        "${DEVICE_MODEL:-}"        机型
chk ro.product.brand        "${DEVICE_BRAND:-}"        品牌
chk ro.product.manufacturer "${DEVICE_MANUFACTURER:-}" 制造商
chk ro.product.device       "${DEVICE_DEVICE:-}"       设备名
chk ro.product.name         "${DEVICE_PRODUCT:-}"      产品名
chk ro.build.fingerprint    "${DEVICE_FINGERPRINT:-}"  指纹
chk ro.build.version.release "${DEVICE_RELEASE:-}"     Android 版本
chk ro.hardware.egl         "${DEVICE_GPU:-}"          GPU 名称
chk ro.serialno             "${DEVICE_SERIAL:-}"       序列号

h "5. 定制参数生效（显示 / 地区）"
size=$(adb -s "${TARGET}" shell wm size 2>/dev/null | tr -d '\r' | awk '{print $NF}')
if [[ "$size" == "${WIDTH:-1080}x${HEIGHT:-2400}" ]]; then ok "分辨率 ${size}"; else bad "分辨率 期望[${WIDTH:-1080}x${HEIGHT:-2400}] 实际[${size}]"; fi
dens=$(adb -s "${TARGET}" shell wm density 2>/dev/null | tr -d '\r' | awk '{print $NF}')
if [[ "$dens" == "${DPI:-420}" ]]; then ok "DPI ${dens}"; else bad "DPI 期望[${DPI:-420}] 实际[${dens}]"; fi
chk persist.sys.timezone "${TIMEZONE:-}" 时区
chk persist.sys.locale   "${LOCALE:-}"   语言
tz=$(adb -s "${TARGET}" shell date +%Z 2>/dev/null | tr -d '\r')
warn "当前 date 输出时区：${tz}（首次开机前修改才完全生效，运行中改动需 docker restart）"

h "6. 预装应用生效"
if [[ -f config/apps/system/expect-packages.txt ]]; then
  while read -r pkg; do
    [[ -z "$pkg" || "$pkg" == \#* ]] && continue
    path=$(adb -s "${TARGET}" shell pm path "$pkg" 2>/dev/null | tr -d '\r')
    if [[ -n "$path" ]]; then ok "系统应用已安装：$pkg -> $path"; else bad "系统应用缺失：$pkg"; fi
  done < config/apps/system/expect-packages.txt
else
  warn "未找到 config/apps/system/expect-packages.txt，跳过系统应用校验（建议补一行一个包名）"
fi
if [[ -f config/apps/user/expect-packages.txt ]]; then
  while read -r pkg; do
    [[ -z "$pkg" || "$pkg" == \#* ]] && continue
    path=$(adb -s "${TARGET}" shell pm path "$pkg" 2>/dev/null | tr -d '\r')
    if [[ -n "$path" ]]; then ok "用户应用已安装：$pkg -> $path"; else bad "用户应用缺失：$pkg"; fi
  done < config/apps/user/expect-packages.txt
else
  warn "未找到 config/apps/user/expect-packages.txt，跳过用户应用校验"
fi

h "7. 字体 / 开机动画"
fonts=$(adb -s "${TARGET}" shell ls /system/fonts 2>/dev/null | tr -d '\r' | grep -iE 'noto.*(cjk|sc)|NotoSansSC|CJK' | head -5)
if [[ -n "$fonts" ]]; then ok "中文字体存在：$(echo "$fonts" | tr '\n' ' ')"; else warn "/system/fonts 未发现 CJK 字体（AOSP 通常自带 NotoSansCJK；如缺失请在 config/fonts 放置并重建）"; fi
ba=$(adb -s "${TARGET}" shell ls -l /system/media/bootanimation.zip 2>/dev/null | tr -d '\r')
if [[ -n "$ba" && "$ba" != *"No such file"* ]]; then ok "开机动画：${ba}"; else warn "未发现 /system/media/bootanimation.zip（将使用系统默认动画）"; fi

h "8. 渲染与画面"
sf=$(adb -s "${TARGET}" shell dumpsys SurfaceFlinger 2>/dev/null | grep -m1 -i 'display' | tr -d '\r')
[[ -n "$sf" ]] && ok "SurfaceFlinger 有输出：${sf:0:80}" || warn "SurfaceFlinger 无输出"
if [[ -n "$SHOT" ]]; then
  adb -s "${TARGET}" exec-out screencap -p > "$SHOT" 2>/dev/null || true
  if [[ -s "$SHOT" ]]; then ok "截图成功：${SHOT}（$(wc -c < "$SHOT") bytes）"; else bad "截图失败，渲染可能未就绪"; fi
fi

h "9. 二进制层 hook（STEALTH_PRELOAD=1 时）"
if [[ "${STEALTH_PRELOAD:-0}" == "1" ]]; then
  # 开启后 adbd 继承 LD_PRELOAD，adb shell 里读到的就应是伪造值
  memline=$(adb -s "${TARGET}" shell head -1 /proc/meminfo 2>/dev/null | tr -d '\r')
  expect_mem=$(head -1 config/fake/meminfo 2>/dev/null || echo "")
  if [[ -n "$expect_mem" && "$memline" == "$expect_mem" ]]; then
    ok "hook 生效：/proc/meminfo = $memline"
  elif [[ -n "$expect_mem" ]]; then
    bad "hook 未生效：/proc/meminfo 期望[$expect_mem] 实际[$memline]（跑 scripts/test-preload.sh 排查）"
  else
    warn "缺 config/fake/meminfo，无法校验"
  fi
else
  warn "STEALTH_PRELOAD=0，跳过（启用流程见 native/xphone-fake/README.md）"
fi

h "结果"
echo "  PASS=${PASS}  FAIL=${FAIL}"
[[ $FAIL -eq 0 ]] && echo "  \033[32m全部通过\033[0m" || echo "  \033[31m存在失败项，请按上方 FAIL 逐条排查\033[0m"
exit $((FAIL > 0))
