#!/system/bin/sh
# =============================================================================
# 开机后定制脚本
# 由 /system/etc/init/xphone.rc 在 "sys.boot_completed=1" 时以 root 触发。
# 幂等：重复执行不会重复安装 / 重复授权。
#
# 覆盖内容：
#   1) 用户级预装 APK（/xphone/apps/user/*.apk，pm install -r -g）
#   2) 语言 / 时区 / 键盘 / 屏幕常亮 等系统设置
#   3) HTTP 代理（云手机常用）
#   4) 默认桌面（替换 Launcher3 时用）
#   5) 运行时权限授予（config/grants.txt）
#   6) 自定义钩子（/xphone/init.d/*.sh）
#   7) 写入 /data/xphone/boot-complete 供健康检查与编排判断
# =============================================================================

set -u
CFG_DIR="${XPHONE_CONFIG_DIR:-/xphone}"
MARK=/data/xphone/boot-complete
LOG_TAG="xphone:postboot"

log() { echo "[$LOG_TAG] $*"; }

# ------------------------------------------------- 0. 等待 PackageManager 就绪
i=0
while [ $i -lt 90 ]; do
  if pm path android >/dev/null 2>&1; then break; fi
  sleep 2
  i=$((i + 1))
done
log "package manager ready after ~$((i * 2))s"

# ------------------------------------------------- 1. 用户级预装 APK（可卸载）
if [ -d "${CFG_DIR}/apps/user" ]; then
  for apk in "${CFG_DIR}"/apps/user/*.apk; do
    case "$apk" in *\*) continue ;; esac
    n=$(basename "$apk" .apk)
    log "install user app: $n"
    # -r 覆盖安装  -g 授予所有运行时权限  --bypass-low-target-sdk-block 兼容低 targetSdk
    out=$(pm install -r -g --bypass-low-target-sdk-block "$apk" 2>&1) \
      || out=$(pm install -r -g "$apk" 2>&1) \
      || true
    log "  -> $out"
  done
fi

# ------------------------------------------------- 2. 语言 / 地区 / 时区
if [ -n "${LOCALE:-}" ]; then
  setprop persist.sys.locale "${LOCALE}"
  settings put system system_locales "${LOCALE}" 2>/dev/null || true
fi
if [ -n "${TIMEZONE:-}" ]; then
  setprop persist.sys.timezone "${TIMEZONE}"
  # 通知系统服务重载时区（不同版本可用性不同，失败忽略）
  setprop persist.sys.timezone.update 1 2>/dev/null || true
fi
if [ -n "${DEFAULT_IME:-}" ]; then
  settings put secure default_input_method "${DEFAULT_IME}" 2>/dev/null || true
  ime enable "${DEFAULT_IME}" 2>/dev/null || true
fi

# ------------------------------------------------- 3. 云手机常用系统设置
if [ "${KEEP_SCREEN_ON:-1}" = "1" ]; then
  settings put system screen_off_timeout 2147483647 2>/dev/null || true
  settings put global stay_on_while_plugged_in 7 2>/dev/null || true
fi
if [ "${DISABLE_LOCKSCREEN:-1}" = "1" ]; then
  settings put global lockscreen.disabled 1 2>/dev/null || true
  settings put secure lockscreen.disabled 1 2>/dev/null || true
fi
# 关闭动画可显著降低 CPU 占用、提升自动化稳定性
if [ "${DISABLE_ANIMATION:-1}" = "1" ]; then
  settings put global window_animation_scale 0.0 2>/dev/null || true
  settings put global transition_animation_scale 0.0 2>/dev/null || true
  settings put global animator_duration_scale 0.0 2>/dev/null || true
fi
settings put global adb_enabled 1 2>/dev/null || true
# 开发者选项开启本身是可疑信号：默认关闭，但保留 USB 调试可用（adb_enabled=1）
if [ "${STEALTH:-0}" = "1" ]; then
  settings put global development_settings_enabled 0 2>/dev/null || true
  settings put global adb_wifi_enabled 0 2>/dev/null || true
  settings put global bugreport_in_power_menu 0 2>/dev/null || true
  settings put secure user_setup_complete 1 2>/dev/null || true
else
  settings put global development_settings_enabled 1 2>/dev/null || true
fi

# ---------------------------------------------- 3a2. 固定设备标识
if [ -n "${ANDROID_ID:-}" ]; then
  settings put secure android_id "${ANDROID_ID}" 2>/dev/null || true
  log "android_id fixed: ${ANDROID_ID}"
fi

# ---------------------------------------------- 3b. 电池伪装（云手机常年满电很可疑）
if [ "${STEALTH_BATTERY:-0}" = "1" ]; then
  dumpsys battery set level "${BATTERY_LEVEL:-78}" >/dev/null 2>&1 || true
  dumpsys battery unplug >/dev/null 2>&1 || true
  dumpsys battery set status "${BATTERY_STATUS:-3}" >/dev/null 2>&1 || true
  log "battery spoofed: level=${BATTERY_LEVEL:-78} status=${BATTERY_STATUS:-3}"
fi

# ------------------------------------------------- 4. HTTP 代理（可空）
if [ -n "${HTTP_PROXY_HOST:-}" ] && [ -n "${HTTP_PROXY_PORT:-}" ]; then
  log "set http_proxy=${HTTP_PROXY_HOST}:${HTTP_PROXY_PORT}"
  settings put global http_proxy "${HTTP_PROXY_HOST}:${HTTP_PROXY_PORT}" 2>/dev/null || true
else
  settings delete global http_proxy 2>/dev/null || true
fi

# ------------------------------------------------- 5. 默认桌面（替换 Launcher 时用）
if [ -n "${DEFAULT_LAUNCHER:-}" ]; then
  log "set home activity: ${DEFAULT_LAUNCHER}"
  cmd package set-home-activity "${DEFAULT_LAUNCHER}" 2>/dev/null || true
fi

# ------------------------------------------------- 6. 运行时授权 config/grants.txt
# 每行格式：<包名> <权限名>
if [ -f "${CFG_DIR}/grants.txt" ]; then
  while IFS= read -r line; do
    case "$line" in ''|'#'*) continue ;; esac
    pkg=$(echo "$line" | awk '{print $1}')
    perm=$(echo "$line" | awk '{print $2}')
    [ -n "$pkg" ] && [ -n "$perm" ] && pm grant "$pkg" "$perm" 2>/dev/null || true
    log "grant: $pkg $perm"
  done < "${CFG_DIR}/grants.txt"
fi

# ------------------------------------------------- 7. 自定义钩子（最后执行）
if [ -d "${CFG_DIR}/init.d" ]; then
  for hook in "${CFG_DIR}"/init.d/*.sh; do
    case "$hook" in *\*) continue ;; esac
    log "run hook: $(basename "$hook")"
    chmod 0755 "$hook" 2>/dev/null || true
    /system/bin/sh "$hook" 2>&1 | sed 's/^/  hook: /' || true
  done
fi

# ------------------------------------------------- 8. 标记开机完成
mkdir -p /data/xphone
chmod 0771 /data/xphone
echo "$(date)" > "${MARK}"
chmod 0664 "${MARK}"
log "boot-complete marker written: ${MARK}"
