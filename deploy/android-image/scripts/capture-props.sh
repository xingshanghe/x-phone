#!/usr/bin/env bash
# =============================================================================
# 从一台真机抓取整套属性，生成可直接使用的设备 profile
#
#   ./scripts/capture-props.sh --serial 0123456789ABCDEF --name pixel_8
#   ./scripts/capture-props.sh --serial 192.168.1.50:5555 --name sm_s911b --full
#
# 产物：
#   config/profiles/<name>.env       # 复制到 config/device.env 即可（或使用 --apply 直接覆盖）
#   config/profiles/<name>.ro.prop   # 整套 ro.* 属性，复制到 config/props/runtime-ro.prop
#
# 为什么要用真机抓而不是手编：
#   fingerprint / bootloader / baseband / security patch / incremental 之间必须自洽，
#   手编几乎必然在某处矛盾，反而更容易被检测。抓一整套是最省事也最稳的做法。
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")/.."

SERIAL=""; NAME=""; FULL=0; APPLY=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --serial) SERIAL="$2"; shift 2 ;;
    --name)   NAME="$2"; shift 2 ;;
    --full)   FULL=1; shift ;;
    --apply)  APPLY=1; shift ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done
[[ -n "$SERIAL" && -n "$NAME" ]] || { echo "需要 --serial <adb 设备> --name <profile 名>"; exit 1; }

mkdir -p config/profiles
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

echo "== 连接 $SERIAL =="
adb connect "$SERIAL" >/dev/null 2>&1 || true
adb -s "$SERIAL" wait-for-device
adb -s "$SERIAL" shell getprop > "$TMP/props.txt"
adb -s "$SERIAL" shell wm size   | tr -d '\r' > "$TMP/size.txt"
adb -s "$SERIAL" shell wm density | tr -d '\r' > "$TMP/density.txt"
echo "  抓到 $(wc -l < "$TMP/props.txt") 条属性"

# getprop 输出形如：[ro.product.model]: [Pixel 8]
getp() { grep -F "[$1]:" "$TMP/props.txt" | head -1 | sed 's/^\[.*\]: \[//; s/\]$//'; }

W=$(awk '{print $NF}' "$TMP/size.txt" | cut -dx -f1)
H=$(awk '{print $NF}' "$TMP/size.txt" | cut -dx -f2)
D=$(awk '{print $NF}' "$TMP/density.txt")

OUT_ENV="config/profiles/${NAME}.env"
cat > "$OUT_ENV" <<EOF
# 由 scripts/capture-props.sh 从真机 ${SERIAL} 抓取生成（$(date -u +%Y-%m-%dT%H:%M:%SZ)）
# 用法： cp ${OUT_ENV} config/device.env   （或 ./scripts/capture-props.sh ... --apply）
# ★ 抓取后请人工复核：序列号、GPU、分辨率、语言时区是否需要脱敏或调整。

# ---------------------------- 显示 / 渲染 -----------------------------------
WIDTH=${W:-1080}
HEIGHT=${H:-2400}
DPI=${D:-420}
FPS=60
GPU_MODE=guest
USE_MEMFD=1
SELINUX=enforcing

# ---------------------------- root 与调试 -----------------------------------
ROOT_MODE=magisk       # 抗检测推荐：属性面保持 user 版，root 交给 Magisk
VERIFIED_BOOT_STATE=$(getp ro.boot.verifiedbootstate)

# ---------------------------- 机型 / 设备指纹 -------------------------------
DEVICE_BRAND=$(getp ro.product.brand)
DEVICE_MANUFACTURER=$(getp ro.product.manufacturer)
DEVICE_MODEL=$(getp ro.product.model)
DEVICE_DEVICE=$(getp ro.product.device)
DEVICE_PRODUCT=$(getp ro.product.name)
DEVICE_BOARD=$(getp ro.product.board)
DEVICE_HARDWARE=$(getp ro.hardware)
DEVICE_RELEASE=$(getp ro.build.version.release)
DEVICE_SDK=$(getp ro.build.version.sdk)
DEVICE_ID=$(getp ro.build.id)
DEVICE_DISPLAY=$(getp ro.build.display.id)
DEVICE_BUILD_TYPE=$(getp ro.build.type)
DEVICE_BUILD_TAGS=$(getp ro.build.tags)
DEVICE_FINGERPRINT=$(getp ro.build.fingerprint)
DEVICE_SERIAL=$(getp ro.serialno)
DEVICE_GPU=$(getp ro.hardware.egl)

# ---------------------------- CPU ABI --------------------------------------
DEVICE_ABILIST=$(getp ro.product.cpu.abilist)
DEVICE_ABILIST64=$(getp ro.product.cpu.abilist64)
DEVICE_ABILIST32=$(getp ro.product.cpu.abilist32)

# ---------------------------- 真机化加固 -----------------------------------
STEALTH=1
DEVICE_BOOTLOADER=$(getp ro.bootloader)
DEVICE_BASEBAND=$(getp gsm.version.baseband)
DEVICE_SECURITY_PATCH=$(getp ro.build.version.security_patch)
DEVICE_INCREMENTAL=$(getp ro.build.version.incremental)
DEVICE_FIRST_API_LEVEL=$(getp ro.product.first_api_level)
DEVICE_BUILD_DATE_UTC=$(getp ro.build.date.utc)
DEVICE_REVISION=$(getp ro.revision)
DEVICE_BOOT_VERIFIED=$(getp ro.boot.verifiedbootstate)
DEVICE_FLASH_LOCKED=1
DEVICE_VBMETA_STATE=locked
STEALTH_RENAME_NET=0
STEALTH_MAC=
STEALTH_BATTERY=1
BATTERY_LEVEL=78
BATTERY_STATUS=3

# ---------------------------- 语言 / 时区 ----------------------------------
LOCALE=$(getp persist.sys.locale)
LANGUAGE_CODE=$(getp persist.sys.language)
COUNTRY_CODE=$(getp persist.sys.country)
TIMEZONE=$(getp persist.sys.timezone)

# ---------------------------- 云手机行为 -----------------------------------
KEEP_SCREEN_ON=1
DISABLE_LOCKSCREEN=1
DISABLE_ANIMATION=1
EOF
echo "生成：$OUT_ENV"

# ---- 整套 ro.* 属性 ----
OUT_PROP="config/profiles/${NAME}.ro.prop"
{
  echo "# 由 scripts/capture-props.sh 从真机 ${SERIAL} 抓取（$(date -u +%Y-%m-%dT%H:%M:%SZ)）"
  echo "# 用法： cp ${OUT_PROP} config/props/runtime-ro.prop"
  echo "# 这些属性会在 init 阶段通过 /init 参数注入，改完只需重启容器。"
  if [[ $FULL -eq 1 ]]; then
    grep -E '^\[(ro\.|gsm\.version)' "$TMP/props.txt" \
      | sed 's/^\[//; s/\]: \[/=/; s/\]$//' \
      | grep -vE '=[[:space:]]*$'
  else
    grep -E '^\[(ro\.(product|build|boot|hardware|board|revision|serialno|bootloader|config|opengles|sf|telephony|system|vendor|odm)|gsm\.version)' "$TMP/props.txt" \
      | sed 's/^\[//; s/\]: \[/=/; s/\]$//' \
      | grep -vE '=[[:space:]]*$'
  fi
} > "$OUT_PROP"
echo "生成：$OUT_PROP（$(grep -vc '^#' "$OUT_PROP") 条属性）"

if [[ $APPLY -eq 1 ]]; then
  cp "$OUT_ENV" config/device.env
  cp "$OUT_PROP" config/props/runtime-ro.prop
  echo "已应用到 config/device.env 与 config/props/runtime-ro.prop"
fi

cat <<EOF

下一步：
  docker compose restart
  ./scripts/verify.sh
  ./scripts/stealth-audit.sh
EOF
