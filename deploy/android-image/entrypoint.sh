#!/system/bin/sh
# =============================================================================
# x-phone Android 容器入口脚本（PID 1，最后 exec 到 Android 的 /init）
#
# 职责边界：
#   [开机前] 本脚本完成 —— 复制开机动画/字体/系统应用、生成 init.rc、组装 init 参数
#   [开机后] /opt/xphone/post-boot.sh 完成 —— 安装用户级 APK、设置语言时区与代理
#
# 关键机制（redroid 特有，务必理解）：
#   1) redroid 把 init 读取内核 cmdline 的来源改成 /proc/self/cmdline，
#      因此传给 /init 的任何 "ro.xxx=yyy" 参数都会被直接写入 Android 属性系统，
#      且发生在 init 阶段 —— 不需要 Magisk / resetprop 即可改机型、指纹、品牌。
#   2) 分辨率 / DPI / 帧率 / GPU 模式通过 androidboot.redroid_* 参数控制。
#   3) 只有 ro.* 前缀能这样注入；persist.* 走 init.rc 的 setprop（见下方生成逻辑）。
#
# 参数优先级（后出现者覆盖先出现者）：
#   内置默认  <  /xphone/device.env  <  /xphone/props/runtime-ro.prop  <  容器命令行 CMD
# =============================================================================

set -u

IMG_DIR="${XPHONE_IMAGE_DIR:-/opt/xphone}"
CFG_DIR="${XPHONE_CONFIG_DIR:-/xphone}"
SYS=/system
LIB_ARCH=$(uname -m 2>/dev/null || echo aarch64)   # aarch64 / x86_64
INIT_BIN="${XPHONE_INIT_BIN:-/init}"
ARG_FILE=/dev/.xphone-init-args.$$
RC_FILE="${SYS}/etc/init/xphone.rc"

log() { echo "[xphone:entry] $*"; }

# SELinux 标签：enforcing 下我们新增的所有文件都必须带 system_file 标签，
# 否则 init/servicemanager 会拒绝加载。chcon 不可用（宿主文件系统不支持
# security.* xattr）时静默跳过 —— 由下方的 canary 探测统一决定是否降级。
label() { chcon u:object_r:system_file:s0 "$@" 2>/dev/null || true; }

# ---------------------------------------------------------------- 1. 载入配置
if [ -n "${XPHONE_DEVICE_ENV:-}" ] && [ -f "${XPHONE_DEVICE_ENV}" ]; then
  log "load env: ${XPHONE_DEVICE_ENV}"
  . "${XPHONE_DEVICE_ENV}"
elif [ -f "${CFG_DIR}/device.env" ]; then
  log "load env: ${CFG_DIR}/device.env"
  . "${CFG_DIR}/device.env"
fi

# 1b. 环境分层（dev=本地测试 / prod=Linux 生产）：profile 覆盖 device.env 的同名变量。
#     XPHONE_ENV 由 compose 的 environment 传入；容器默认值取 dev（弱加固，最不容易起不来），
#     生产必须显式 XPHONE_ENV=prod —— 用 ./scripts/up.sh prod 会自动写入 .env。
XPHONE_ENV="${XPHONE_ENV:-dev}"
PROFILE_FILE="${CFG_DIR}/profiles/${XPHONE_ENV}.env"
if [ -f "${PROFILE_FILE}" ]; then
  log "env=${XPHONE_ENV}: load ${PROFILE_FILE}"
  . "${PROFILE_FILE}"
else
  log "env=${XPHONE_ENV}: no profile at ${PROFILE_FILE}, keep device.env only"
fi

# ------------------------------------------- 2. 组装传给 /init 的参数（有序）
: > "${ARG_FILE}"
add_arg() {
  # 空值跳过；值含空格也没问题（逐行读取，不做单词切分）
  [ -n "${2:-}" ] && printf '%s=%s\n' "$1" "$2" >> "${ARG_FILE}" || true
}
add_raw() {
  [ -n "${1:-}" ] && printf '%s\n' "$1" >> "${ARG_FILE}" || true
}

## 2.1 显示与渲染（运行时可配，改完 docker restart 生效）
add_arg "androidboot.redroid_width"  "${WIDTH:-1080}"
add_arg "androidboot.redroid_height" "${HEIGHT:-2400}"
add_arg "androidboot.redroid_dpi"    "${DPI:-420}"
add_arg "androidboot.redroid_fps"    "${FPS:-30}"
add_arg "androidboot.redroid_gpu_mode" "${GPU_MODE:-guest}"     # guest=软件渲染 / host=GPU 直通 / auto
[ -n "${GPU_NODE:-}" ] && add_arg "androidboot.redroid_gpu_node" "${GPU_NODE}"
# 内核 >= 5.18 已移除 ashmem，必须用 memfd，否则开机卡死
add_arg "androidboot.use_memfd" "${USE_MEMFD:-1}"
# 真机为 enforcing（permissive 本身就是一个检测特征），默认 enforcing。
# 但 enforcing 要求我们新增的文件都带正确 SELinux 标签；宿主文件系统若不支持
# security.* xattr，chcon 会失败、文件将处于无标签状态并被 init 拒绝。
# 用 canary 文件实测一次：失败则自动降级为 permissive 并告警，优先保证能开起来。
SELINUX_MODE="${SELINUX:-enforcing}"
if [ "$SELINUX_MODE" = "enforcing" ]; then
  _probe="${SYS}/.xphone-selinux-probe"
  if touch "$_probe" 2>/dev/null && chcon u:object_r:system_file:s0 "$_probe" 2>/dev/null; then
    rm -f "$_probe"
    log "selinux: enforcing（chcon canary 通过）"
  else
    rm -f "$_probe" 2>/dev/null
    log "WARN: chcon 不可用（宿主文件系统不支持 security.* xattr），enforcing 会让定制文件无法加载，自动降级为 permissive"
    SELINUX_MODE=permissive
  fi
fi
add_arg "androidboot.selinux" "$SELINUX_MODE"
add_arg "androidboot.serialno" "${DEVICE_SERIAL:-}"
add_arg "androidboot.hardware" "${DEVICE_HARDWARE:-}"
add_arg "androidboot.verifiedbootstate" "${VERIFIED_BOOT_STATE:-orange}"

## 2.2 root 与调试属性
# ROOT_MODE=adb     -> ro.secure=0，adb shell 直接是 root（默认，改动最小）
# ROOT_MODE=magisk  -> 由镜像内的 Magisk 提供 su（需用 redroid-script 预置基础镜像）
# ROOT_MODE=none    -> 保持 user 版属性（对 Play Integrity 友好，但无 root）
case "${ROOT_MODE:-adb}" in
  adb)
    add_arg "ro.secure"      "0"
    add_arg "ro.adb.secure"  "0"
    add_arg "ro.debuggable"  "1"
    ;;
  none)
    add_arg "ro.secure"      "1"
    add_arg "ro.adb.secure"  "1"
    add_arg "ro.debuggable"  "0"
    ;;
  magisk)
    # 属性面伪装成 user 版，root 由 Magisk 提供（抗检测场景的正确组合）
    add_arg "ro.secure"      "1"
    add_arg "ro.adb.secure"  "0"
    add_arg "ro.debuggable"  "0"
    ;;
esac

## 2.2b 真机化加固（STEALTH=1 时开启，详见 docs/stealth.md）
if [ "${STEALTH:-0}" = "1" ]; then
  log "stealth hardening enabled"
  # 必须抹掉的模拟器痕迹
  add_arg "ro.kernel.qemu" "0"
  add_arg "ro.boot.qemu" "0"
  # 启动状态与解锁状态：真机为 green / locked
  add_arg "ro.boot.verifiedbootstate" "${DEVICE_BOOT_VERIFIED:-green}"
  add_arg "ro.boot.flash.locked"      "${DEVICE_FLASH_LOCKED:-1}"
  add_arg "ro.boot.vbmeta.device_state" "${DEVICE_VBMETA_STATE:-locked}"
  add_arg "ro.boot.veritymode" "enforcing"
  add_arg "ro.bootmode" "normal"
  add_arg "ro.boot.mode" "normal"
  add_arg "ro.boot.selinux" "enforcing"
  # bootloader / baseband：检测最常读的两个"真机才有"的字段
  [ -n "${DEVICE_BOOTLOADER:-}" ] && add_arg "ro.bootloader" "${DEVICE_BOOTLOADER}"
  [ -n "${DEVICE_BOOTLOADER:-}" ] && add_arg "ro.build.expect.bootloader" "${DEVICE_BOOTLOADER}"
  [ -n "${DEVICE_BASEBAND:-}" ]   && add_arg "ro.build.expect.baseband" "${DEVICE_BASEBAND}"
  # 构建信息自洽（与 fingerprint 的 build id 同代）
  [ -n "${DEVICE_SECURITY_PATCH:-}" ] && add_arg "ro.build.version.security_patch" "${DEVICE_SECURITY_PATCH}"
  [ -n "${DEVICE_SECURITY_PATCH:-}" ] && add_arg "ro.vendor.build.security_patch" "${DEVICE_SECURITY_PATCH}"
  [ -n "${DEVICE_INCREMENTAL:-}" ] && add_arg "ro.build.version.incremental" "${DEVICE_INCREMENTAL}"
  [ -n "${DEVICE_INCREMENTAL:-}" ] && add_arg "ro.system.build.version.incremental" "${DEVICE_INCREMENTAL}"
  [ -n "${DEVICE_INCREMENTAL:-}" ] && add_arg "ro.vendor.build.version.incremental" "${DEVICE_INCREMENTAL}"
  [ -n "${DEVICE_FIRST_API_LEVEL:-}" ] && add_arg "ro.product.first_api_level" "${DEVICE_FIRST_API_LEVEL}"
  [ -n "${DEVICE_BUILD_DATE_UTC:-}" ] && add_arg "ro.build.date.utc" "${DEVICE_BUILD_DATE_UTC}"
  [ -n "${DEVICE_REVISION:-}" ] && add_arg "ro.revision" "${DEVICE_REVISION}"
fi

## 2.3 机型 / 设备指纹（Android 8+ 建议同时写 ro.product.* 与 ro.product.<分区>.* 两套）
if [ -n "${DEVICE_MODEL:-}" ]; then
  for k in ro.product.model ro.product.system.model ro.product.vendor.model ro.product.product.model ro.product.odm.model ro.product.system_ext.model; do
    add_arg "$k" "${DEVICE_MODEL}"
  done
fi
if [ -n "${DEVICE_BRAND:-}" ]; then
  for k in ro.product.brand ro.product.system.brand ro.product.vendor.brand ro.product.product.brand ro.product.odm.brand ro.product.system_ext.brand; do
    add_arg "$k" "${DEVICE_BRAND}"
  done
fi
if [ -n "${DEVICE_MANUFACTURER:-}" ]; then
  for k in ro.product.manufacturer ro.product.system.manufacturer ro.product.vendor.manufacturer ro.product.product.manufacturer; do
    add_arg "$k" "${DEVICE_MANUFACTURER}"
  done
fi
if [ -n "${DEVICE_DEVICE:-}" ]; then
  for k in ro.product.device ro.product.system.device ro.product.vendor.device ro.product.product.device ro.build.product; do
    add_arg "$k" "${DEVICE_DEVICE}"
  done
fi
if [ -n "${DEVICE_PRODUCT:-}" ]; then
  for k in ro.product.name ro.product.system.name ro.product.vendor.name ro.product.product.name; do
    add_arg "$k" "${DEVICE_PRODUCT}"
  done
fi
if [ -n "${DEVICE_BOARD:-}" ]; then
  for k in ro.product.board ro.product.vendor.board ro.board.platform; do
    add_arg "$k" "${DEVICE_BOARD}"
  done
fi
if [ -n "${DEVICE_FINGERPRINT:-}" ]; then
  for k in ro.build.fingerprint ro.system.build.fingerprint ro.vendor.build.fingerprint \
           ro.product.build.fingerprint ro.odm.build.fingerprint ro.system_ext.build.fingerprint \
           ro.bootimage.build.fingerprint; do
    add_arg "$k" "${DEVICE_FINGERPRINT}"
  done
fi
[ -n "${DEVICE_RELEASE:-}" ] && add_arg "ro.build.version.release" "${DEVICE_RELEASE}"
[ -n "${DEVICE_SDK:-}" ]     && add_arg "ro.build.version.sdk"     "${DEVICE_SDK}"
[ -n "${DEVICE_ID:-}" ]      && add_arg "ro.build.id"              "${DEVICE_ID}"
[ -n "${DEVICE_DISPLAY:-}" ] && add_arg "ro.build.display.id"      "${DEVICE_DISPLAY}"
[ -n "${DEVICE_BUILD_TYPE:-}" ] && add_arg "ro.build.type" "${DEVICE_BUILD_TYPE}"
[ -n "${DEVICE_BUILD_TAGS:-}" ] && add_arg "ro.build.tags" "${DEVICE_BUILD_TAGS}"
[ -n "${DEVICE_GPU:-}" ]     && add_arg "ro.hardware.egl"          "${DEVICE_GPU}"
[ -n "${DEVICE_ABILIST:-}" ] && add_arg "ro.product.cpu.abilist"   "${DEVICE_ABILIST}"
[ -n "${DEVICE_ABILIST64:-}" ] && add_arg "ro.product.cpu.abilist64" "${DEVICE_ABILIST64}"
[ -n "${DEVICE_ABILIST32:-}" ] && add_arg "ro.product.cpu.abilist32" "${DEVICE_ABILIST32}"

## 2.4 ARM 翻译层（x86 主机跑 arm-only APK 时开启，镜像需含 libndk/libhoudini）
[ -n "${NATIVE_BRIDGE:-}" ] && add_arg "ro.dalvik.vm.native.bridge" "${NATIVE_BRIDGE}"
[ -n "${NATIVE_BRIDGE:-}" ] && add_arg "ro.enable.native.bridge.exec" "1"
[ -n "${NATIVE_BRIDGE:-}" ] && add_arg "ro.vendor.enable.native.bridge.exec" "1"
[ -n "${NATIVE_BRIDGE:-}" ] && add_arg "ro.vendor.enable.native.bridge.exec64" "1"

## 2.5 追加属性文件（每行一条 ro.xxx=yyy）
if [ -f "${CFG_DIR}/props/runtime-ro.prop" ]; then
  log "append props: ${CFG_DIR}/props/runtime-ro.prop"
  while IFS= read -r line; do
    case "$line" in ''|'#'*) continue ;; esac
    add_raw "$line"
  done < "${CFG_DIR}/props/runtime-ro.prop"
fi

## 2.6 命令行传入的额外参数（优先级最高）
for a in "$@"; do printf '%s\n' "$a" >> "${ARG_FILE}"; done

# ------------------------------------------- 3. 开机前的文件系统定制（幂等）
## 3.1 开机动画
if [ -f "${CFG_DIR}/bootanimation.zip" ]; then
  log "install bootanimation -> ${SYS}/media/bootanimation.zip"
  cp -f "${CFG_DIR}/bootanimation.zip" "${SYS}/media/bootanimation.zip"
  chmod 0644 "${SYS}/media/bootanimation.zip"
  label "${SYS}/media/bootanimation.zip"
fi

## 3.2 字体（覆盖式；fonts.xml 的改法见 README）
if [ -d "${CFG_DIR}/fonts" ]; then
  for f in "${CFG_DIR}"/fonts/*; do
    case "$f" in *\*) continue ;; esac
    case "$f" in *.ttf|*.otf|*.ttc)
      log "install font: $(basename "$f")"
      cp -f "$f" "${SYS}/fonts/"
      chmod 0644 "${SYS}/fonts/$(basename "$f")"
      label "${SYS}/fonts/$(basename "$f")"
      ;;
    esac
  done
fi

## 3.3 系统级预装 APK（不可卸载；放在 /xphone/apps/system 即可，无需重建镜像）
if [ -d "${CFG_DIR}/apps/system" ]; then
  for apk in "${CFG_DIR}"/apps/system/*.apk; do
    case "$apk" in *\*) continue ;; esac
    n=$(basename "$apk" .apk)
    log "install system app: $n"
    mkdir -p "${SYS}/priv-app/$n"
    cp -f "$apk" "${SYS}/priv-app/$n/$n.apk"
    chmod 0755 "${SYS}/priv-app/$n"
    chmod 0644 "${SYS}/priv-app/$n/$n.apk"
    label "${SYS}/priv-app/$n" "${SYS}/priv-app/$n/$n.apk"
  done
fi

## 3.4 运行时精简系统应用（与构建期 prune-apps.txt 同样逻辑，这里用于热调整）
if [ -f "${CFG_DIR}/prune-apps.txt" ]; then
  while IFS= read -r p; do
    case "$p" in ''|'#'*) continue ;; esac
    if [ -e "${SYS}/$p" ]; then log "runtime prune: /system/$p"; rm -rf "${SYS}/$p"; fi
  done < "${CFG_DIR}/prune-apps.txt"
fi

## 3.4b 清理容器 / 模拟器痕迹（config/prune-traces.txt）
if [ "${STEALTH:-0}" = "1" ]; then
  # 设备节点：qemu / goldfish 系列
  for n in qemu_pipe goldfish_pipe goldfish_fb qemu_trace; do
    [ -e "/dev/$n" ] && { log "remove dev node: /dev/$n"; rm -f "/dev/$n"; }
  done
  # 文件清单
  if [ -f "${CFG_DIR}/prune-traces.txt" ]; then
    while IFS= read -r p; do
      case "$p" in ''|'#'*) continue ;; esac
      if [ -e "/$p" ]; then log "remove trace: /$p"; rm -rf "/$p"; fi
    done < "${CFG_DIR}/prune-traces.txt"
  fi
  # 系统能力声明（PackageManager.hasSystemFeature）
  if [ -f "${CFG_DIR}/permissions/xphone-features.xml" ]; then
    log "install feature declarations"
    cp -f "${CFG_DIR}/permissions/xphone-features.xml" "${SYS}/etc/permissions/xphone-features.xml"
    chmod 0644 "${SYS}/etc/permissions/xphone-features.xml"
    label "${SYS}/etc/permissions/xphone-features.xml"
  fi
  # 网络外形：接口改名 + MAC（改动会影响 netd，默认关闭，先在单实例验证）
  if [ "${STEALTH_RENAME_NET:-0}" = "1" ] && [ -e /sys/class/net/eth0 ]; then
    log "rename net iface eth0 -> wlan0"
    ip link set eth0 down 2>/dev/null || true
    ip link set eth0 name wlan0 2>/dev/null || true
    ip link set wlan0 up 2>/dev/null || true
  fi
  # 二进制层 hook 库（需自行用 NDK 编译，见 native/xphone-fake/README.md）
  if [ -f "${CFG_DIR}/native/${LIB_ARCH}/libxphone_fake.so" ]; then
    log "install hook lib: ${LIB_ARCH}/libxphone_fake.so"
    cp -f "${CFG_DIR}/native/${LIB_ARCH}/libxphone_fake.so" "${SYS}/lib64/libxphone_fake.so"
    chmod 0644 "${SYS}/lib64/libxphone_fake.so"
    label "${SYS}/lib64/libxphone_fake.so"
  fi
  # 伪造 /proc/cmdline（best-effort：多数环境不允许 bind 到 procfs 文件，失败可忽略）
  if [ "${STEALTH_FAKE_CMDLINE:-0}" = "1" ] && [ -f "${CFG_DIR}/fake/cmdline" ]; then
    if mount --bind "${CFG_DIR}/fake/cmdline" /proc/cmdline 2>/dev/null; then
      log "cmdline spoofed"
    else
      log "cmdline spoof skipped (not permitted)"
    fi
  fi
  if [ -n "${STEALTH_MAC:-}" ]; then
    ifc=$(ls /sys/class/net 2>/dev/null | grep -E '^(eth0|wlan0)$' | head -1)
    if [ -n "$ifc" ]; then
      log "set MAC ${STEALTH_MAC} on ${ifc}"
      ip link set "$ifc" down 2>/dev/null || true
      ip link set dev "$ifc" address "${STEALTH_MAC}" 2>/dev/null || true
      ip link set "$ifc" up 2>/dev/null || true
    fi
  fi
fi

## 3.5 同步 /system/build.prop（文件面与运行时属性必须一致）
# 很多检测不是只读 getprop，而是直接读 /system/build.prop 与运行时结果做交叉比对。
# 只改运行时、不改文件，等于自己给自己留了一个 100% 命中的特征。
# 用标记块保证幂等：每次启动先把上一次写入的块删掉再重写。
sync_build_prop() {
  _prop_file="$1"; _prefix="$2"
  [ -f "$_prop_file" ] || return 0
  sed -i '/# >>> XPHONE-CUSTOM-BEGIN/,/# <<< XPHONE-CUSTOM-END/d' "$_prop_file" 2>/dev/null || true
  {
    echo "# >>> XPHONE-CUSTOM-BEGIN (由 entrypoint 生成，勿手改)"
    while IFS= read -r a; do
      case "$a" in "$_prefix"*) echo "$a" ;; esac
    done < "${ARG_FILE}"
    echo "# <<< XPHONE-CUSTOM-END"
  } >> "$_prop_file"
  log "synced: $_prop_file"
}
sync_build_prop "${SYS}/build.prop" "ro."
[ -f "/vendor/build.prop" ] && sync_build_prop "/vendor/build.prop" "ro.vendor."

## 3.6 生成 init.rc：负责 persist.* 属性 + 开机后脚本
log "generate ${RC_FILE}"
mkdir -p "${SYS}/etc/init"
# LD_PRELOAD 二进制层（默认关闭，用 scripts/test-preload.sh 验证后再打开）：
# 必须在 early-init 设置，zygote 与其子进程（含 adbd）才会继承。
# 注意：device.env 是 entrypoint 这个 shell 读到的，Android 进程完全看不到；
# XFAKE_* 必须在这里显式 setenv 进 init 的全局环境，hook 库才读得到。
PRELOAD_LINE=""
ENV_LINES=""
add_setenv() {
  # init.rc 支持双引号包裹含空格/括号的参数；剥离值中的双引号防注入
  _v=$(printf '%s' "${2:-}" | tr -d '"')
  [ -n "$_v" ] && ENV_LINES="${ENV_LINES}
    setenv $1 \"${_v}\""
}
if [ "${STEALTH_PRELOAD:-0}" = "1" ]; then
  if [ -f "${CFG_DIR}/native/${LIB_ARCH}/libxphone_fake.so" ]; then
    log "enable LD_PRELOAD hook: ${LIB_ARCH}/libxphone_fake.so"
    PRELOAD_LINE="setenv LD_PRELOAD /system/lib64/libxphone_fake.so"
    add_setenv XFAKE_DIR        "${XFAKE_DIR:-/xphone/fake}"
    add_setenv XFAKE_CPU_COUNT  "${XFAKE_CPU_COUNT:-}"
    add_setenv XFAKE_MEM_MB     "${XFAKE_MEM_MB:-}"
    add_setenv XFAKE_GL_VENDOR  "${XFAKE_GL_VENDOR:-}"
    add_setenv XFAKE_GL_RENDERER "${XFAKE_GL_RENDERER:-}"
    add_setenv XFAKE_GL_VERSION "${XFAKE_GL_VERSION:-}"
    [ "${XFAKE_DEBUG:-0}" = "1" ] && ENV_LINES="${ENV_LINES}
    setenv XFAKE_DEBUG 1"
  else
    log "WARN: STEALTH_PRELOAD=1 但 config/native/${LIB_ARCH}/libxphone_fake.so 不存在，跳过（先跑 native/xphone-fake/build-ndk.sh）"
  fi
fi

cat > "${RC_FILE}" <<EOF
# 由 x-phone entrypoint.sh 生成，每次容器启动覆盖；请勿手改
on early-init
    ${PRELOAD_LINE}${ENV_LINES}

on post-fs-data
    mkdir /data/xphone 0771 system system
    setprop persist.sys.timezone ${TIMEZONE:-Asia/Shanghai}
    setprop persist.sys.locale ${LOCALE:-zh-CN}
    setprop persist.sys.language ${LANGUAGE_CODE:-zh}
    setprop persist.sys.country ${COUNTRY_CODE:-CN}
    setprop persist.xphone.managed 1

on boot
    setprop persist.sys.timezone ${TIMEZONE:-Asia/Shanghai}
    setprop persist.sys.locale ${LOCALE:-zh-CN}

on property:sys.boot_completed=1
    exec - root root -- /system/bin/sh ${IMG_DIR}/post-boot.sh
EOF
chmod 0644 "${RC_FILE}"
label "${RC_FILE}"
label "${IMG_DIR}/post-boot.sh"

# ------------------------------- 4. 重建位置参数，exec 交棒给 Android init
set --
while IFS= read -r a; do
  [ -n "$a" ] && set -- "$@" "$a"
done < "${ARG_FILE}"
rm -f "${ARG_FILE}"

log "handing over to ${INIT_BIN} with $# args"
exec "${INIT_BIN}" "$@"
