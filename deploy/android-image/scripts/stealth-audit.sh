#!/usr/bin/env bash
# =============================================================================
# 真机化自检：站在"检测方"的视角扫描容器，输出风险项
#
#   ./scripts/stealth-audit.sh             # 摘要（只列风险）
#   ./scripts/stealth-audit.sh --verbose   # 打印每一项的实际取值
#
# 合规声明：仅用于自有云手机的合规测试与账号隔离，请勿用于欺诈或绕过风控从事违法活动。
# =============================================================================
set -uo pipefail
cd "$(dirname "$0")/.."

CONTAINER="${CONTAINER:-xphone-android-01}"
ADB_HOST="${ADB_HOST:-127.0.0.1}"
ADB_PORT="${ADB_PORT:-18120}"
VERBOSE=0; [[ "${1:-}" == "--verbose" ]] && VERBOSE=1
TARGET="${ADB_HOST}:${ADB_PORT}"

# 期望值：device.env + profiles/<XPHONE_ENV>.env（与容器内加载顺序一致）
[[ -f .env ]] && { set -a; . .env; set +a; }
XPHONE_ENV="${XPHONE_ENV:-dev}"
[[ -f config/device.env ]] && { set -a; . config/device.env; set +a; }
[[ -f "config/profiles/${XPHONE_ENV}.env" ]] && { set -a; . "config/profiles/${XPHONE_ENV}.env"; set +a; }
[[ -z "${CONTAINER:-}" ]] && CONTAINER="xphone-android-$( [[ "$XPHONE_ENV" == "prod" ]] && echo 01 || echo dev )"

RISKS=(); OKS=0
risk() { RISKS+=("$1"); printf '  \033[31m[RISK]\033[0m %s\n' "$1"; }
ok()   { OKS=$((OKS+1)); [[ $VERBOSE -eq 1 ]] && printf '  \033[32m[OK]\033[0m   %s\n' "$1"; return 0; }
h()    { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

adb connect "$TARGET" >/dev/null 2>&1 || true
sh() { adb -s "$TARGET" shell "$@" 2>/dev/null | tr -d '\r'; }
gp() { sh getprop "$1"; }

h "1. 构建属性（build 面）"
chk() { # chk <属性> <期望> <说明> <期望为空时是否风险:1/0>
  local act; act=$(gp "$1")
  if [[ "$act" == "$2" ]]; then ok "$3 = $act"
  elif [[ -z "$2" && -z "$act" && "$4" == "0" ]]; then ok "$3 为空（可接受）"
  else risk "$3：$1 期望[$2] 实际[${act:-<空>}]"; fi
}
chk ro.build.type user "构建类型" 0
chk ro.build.tags release-keys "签名标签" 0
chk ro.debuggable 0 "可调试标志" 0
chk ro.secure 1 "ro.secure" 0
chk ro.boot.verifiedbootstate "${DEVICE_BOOT_VERIFIED:-green}" "校验启动状态" 0
chk ro.boot.flash.locked 1 "bootloader 锁" 0
chk ro.boot.vbmeta.device_state locked "vbmeta 状态" 0

h "2. 模拟器 / 容器痕迹"
qemu=$(gp ro.kernel.qemu); [[ "$qemu" == "0" || -z "$qemu" ]] && ok "ro.kernel.qemu=$qemu" || risk "ro.kernel.qemu=$qemu（模拟器特征）"
qcount=$(sh getprop | grep -c '^\[qemu\.' || true); [[ "${qcount:-0}" -eq 0 ]] && ok "无 qemu.* 属性" || risk "存在 ${qcount} 条 qemu.* 属性"
hw=$(gp ro.hardware); case "$hw" in *ranchu*|*goldfish*|*qemu*|*redroid*) risk "ro.hardware=$hw 含模拟器关键字";; *) ok "ro.hardware=$hw";; esac
br=$(gp ro.product.brand); case "$br" in *redroid*|*generic*|*emulator*) risk "ro.product.brand=$br 不像真机";; *) ok "ro.product.brand=$br";; esac
for dev in qemu_pipe goldfish_pipe qemu_trace; do
  sh test -e "/dev/$dev" && risk "存在设备节点 /dev/$dev" || ok "无 /dev/$dev"
done
traces=$(sh ls /system/bin /system/lib64 2>/dev/null | grep -iE 'qemu|goldfish|redroid|ndk_translation|houdini' | head -5)
[[ -z "$traces" ]] && ok "无模拟器/翻译层可执行文件" || risk "发现痕迹：$traces"

h "3. 真机专有字段"
for p in ro.bootloader gsm.version.baseband ro.build.version.security_patch ro.product.first_api_level; do
  v=$(gp "$p"); [[ -n "$v" && "$v" != "unknown" ]] && ok "$p=$v" || risk "$p 为空/unknown（真机必有值）"
done

h "4. 指纹自洽性"
fp=$(gp ro.build.fingerprint); dev=$(gp ro.product.device); bid=$(gp ro.build.id)
[[ -n "$fp" && -n "$dev" && "$fp" == *"/${dev}/"* ]] && ok "指纹包含 device($dev)" || risk "指纹与 ro.product.device 不一致：fp=$fp device=$dev"
[[ -n "$bid" && "$fp" == *"/${bid}"* ]] && ok "指纹包含 build.id($bid)" || risk "指纹与 ro.build.id 不一致：fp=$fp id=$bid"

h "5. 架构与 CPU"
abi=$(gp ro.product.cpu.abilist); cpu=$(sh grep -m1 -E 'Hardware|model name|Processor' /proc/cpuinfo)
case "$abi" in
  arm64*)
    if sh grep -qiE 'intel|amd|genuineintel|authenticamd' /proc/cpuinfo; then
      risk "ABI 声明 arm64，但 /proc/cpuinfo 暴露 x86 CPU（$cpu）—— x86 宿主无法伪装成真机"
    else ok "arm64 ABI 与 CPU 一致（$cpu）"; fi ;;
  x86_64*) risk "ABI=$abi：x86 宿主，/proc/cpuinfo 会暴露（$cpu），抗检测请换 arm64 宿主" ;;
esac

h "6. 运行时环境"
enf=$(sh getenforce); [[ "$enf" == "Enforcing" ]] && ok "SELinux=Enforcing" || risk "SELinux=${enf:-<空>}（真机为 Enforcing）"
feat=$(sh pm list features 2>/dev/null | wc -l | tr -d ' '); [[ "${feat:-0}" -ge 20 ]] && ok "系统 feature 数=${feat}" || risk "系统 feature 数=${feat} 偏少（真机通常 40+，补 config/permissions/xphone-features.xml）"
sensors=$(sh dumpsys sensorservice 2>/dev/null | grep -c 'sensor' || true); [[ "${sensors:-0}" -ge 1 ]] && ok "存在传感器" || risk "无传感器（无法伪造，属已知限制）"
bat=$(sh dumpsys battery 2>/dev/null | grep -m1 'level' | tr -d ' '); [[ -n "$bat" ]] && ok "电池：${bat}" || risk "读取电池信息失败（云手机常年满电会被判定为异常）"
devopt=$(sh settings get global development_settings_enabled | tr -d '\r'); [[ "$devopt" == "0" ]] && ok "开发者选项关闭" || risk "development_settings_enabled=${devopt}（真机默认 0）"
iface=$(sh ls /sys/class/net 2>/dev/null | tr '\n' ' '); case "$iface" in *wlan0*) ok "存在 wlan0（$iface）";; *) risk "网络接口为 $iface，真机应为 wlan0/p2p0";; esac
mac=$(sh cat /sys/class/net/wlan0/address 2>/dev/null || sh cat /sys/class/net/eth0/address 2>/dev/null)
case "$mac" in 02:42:*) risk "MAC=$mac 使用 Docker OUI，建议 STEALTH_MAC 覆盖";; *) ok "MAC=${mac:-<未知>}";; esac

h "7. 无法消除项（仅供参考，不计入风险）"
echo "  - /proc/self/mountinfo 中的容器路径：$(sh grep -m1 -o 'overlay2[^ ]*' /proc/self/mountinfo 2>/dev/null | head -1 || echo '<需容器内查看>')"
echo "  - 相机/GPS/基带真实数据、Play Integrity 硬件级认证：不可伪造"

h "结果"
echo "  OK=${OKS}  RISK=${#RISKS[@]}"
if [[ ${#RISKS[@]} -eq 0 ]]; then
  echo "  \033[32m未发现明显风险项\033[0m"
else
  echo "  \033[33m风险项清单（按 docs/stealth.md 对应章节处理）：\033[0m"
  for r in "${RISKS[@]}"; do echo "    - $r"; done
fi
exit 0
