#!/usr/bin/env bash
# =============================================================================
# 宿主机前置条件：检查 + 准备 Android 容器所需的内核能力
# 适用：Ubuntu 22.04 / 24.04（x86_64）。幂等，可重复执行。
#
#   ./scripts/host-setup.sh            # 只检查并加载内核模块
#   ./scripts/host-setup.sh --persist  # 额外写入开机自动加载配置（modules-load.d + binderfs 挂载）
# =============================================================================
set -euo pipefail

PERSIST=0
[[ "${1:-}" == "--persist" ]] && PERSIST=1

ok()   { printf '  \033[32m[OK]\033[0m   %s\n' "$*"; }
warn() { printf '  \033[33m[WARN]\033[0m %s\n' "$*"; }
bad()  { printf '  \033[31m[FAIL]\033[0m %s\n' "$*"; }
h()    { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

h "1. 操作系统与内核"
ARCH=$(uname -m); KREL=$(uname -r)
echo "  arch=$ARCH kernel=$KREL"
case "$(uname -s)" in
  Linux) ok "Linux 主机" ;;
  *)     bad "必须是 Linux 主机；macOS / Windows 的 Docker Desktop 无法运行（其内核 VM 没有 binder/binderfs）"; exit 1 ;;
esac
[[ "$ARCH" == "x86_64" ]] && ok "x86_64（目标架构）" || warn "架构为 $ARCH，镜像基线是 amd64，需换成对应的 redroid 镜像"

h "2. Docker"
if command -v docker >/dev/null 2>&1; then
  docker version --format '  docker {{.Server.Version}}' 2>/dev/null || true
  ok "docker 可用"
else
  bad "未安装 docker：https://docs.docker.com/engine/install/ubuntu/"
  exit 1
fi

h "3. 内核模块：binder（必需）"
sudo apt-get install -y "linux-modules-extra-${KREL}" >/dev/null 2>&1 || warn "linux-modules-extra-${KREL} 安装失败（可能已内置或源不可用）"
if sudo modprobe binder_linux devices="binder,hwbinder,vndbinder" 2>/dev/null; then
  ok "binder_linux 已加载"
else
  bad "binder_linux 加载失败 —— 内核未开启 CONFIG_ANDROID_BINDER_IPC，需更换内核"
fi
lsmod | grep -q binder_linux && ok "lsmod 中可见 binder_linux" || true

h "4. binder 设备节点 / binderfs（必需）"
if ls /dev/binder /dev/hwbinder /dev/vndbinder >/dev/null 2>&1; then
  ok "/dev/binder /dev/hwbinder /dev/vndbinder 已存在"
else
  warn "没有 binder 设备节点，尝试挂载 binderfs（Ubuntu 22.04+ 常见）"
  sudo mkdir -p /dev/binderfs
  if sudo mount -t binder binder /dev/binderfs 2>/dev/null; then
    ok "binderfs 已挂载到 /dev/binderfs"
    ls -l /dev/binderfs | sed 's/^/    /'
  else
    bad "binderfs 挂载失败 —— 内核缺少 CONFIG_ANDROID_BINDERFS；可尝试升级内核到 5.18+"
  fi
fi

h "5. ashmem（可选，内核 >= 5.18 已移除）"
if sudo modprobe ashmem_linux 2>/dev/null; then
  ok "ashmem_linux 已加载"
else
  warn "ashmem 不可用（正常）—— 请在启动参数中使用 androidboot.use_memfd=1（本方案默认已开启）"
fi

h "6. GPU / 渲染"
if ls /dev/dri/renderD* >/dev/null 2>&1; then
  ok "存在 render 节点，可用 GPU 直通：$(ls /dev/dri/renderD* | tr '\n' ' ')"
  echo "    -> 启动参数用 androidboot.redroid_gpu_mode=host 并把 /dev/dri 挂进容器"
  echo "    -> NVIDIA 独显常出现渲染花屏，建议保持 gpu_mode=guest"
else
  warn "未发现 /dev/dri/renderD*，只能软件渲染：androidboot.redroid_gpu_mode=guest"
fi

h "7. KVM（澄清）"
if [[ -e /dev/kvm ]]; then ok "/dev/kvm 存在，但 redroid 不使用 KVM（它共享宿主内核，不做指令虚拟化）"; else warn "没有 /dev/kvm —— 不影响 redroid，仅 QEMU 类方案（budtmo/docker-android、Cuttlefish）需要"; fi

h "8. 资源"
echo "  CPU: $(nproc) 核  内存: $(awk '/MemTotal/{printf "%.1f GB", $2/1024/1024}' /proc/meminfo)  可用磁盘: $(df -h /var/lib/docker 2>/dev/null | awk 'NR==2{print $4}' || df -h / | awk 'NR==2{print $4}')"
echo "  建议：每实例 2 vCPU / 2–4 GB 内存 / 8 GB 磁盘（首次镜像解压后）"

if [[ $PERSIST -eq 1 ]]; then
  h "9. 写入持久化配置"
  echo "binder_linux" | sudo tee /etc/modules-load.d/xphone-android.conf >/dev/null
  sudo tee /etc/modprobe.d/xphone-binder.conf >/dev/null <<'EOF'
options binder_linux devices=binder,hwbinder,vndbinder
EOF
  if ! ls /dev/binder >/dev/null 2>&1; then
    sudo mkdir -p /opt/xphone/binderfs
    sudo tee /etc/systemd/system/xphone-binderfs.service >/dev/null <<'EOF'
[Unit]
Description=Mount binderfs for Android containers
DefaultDependencies=no
Before=docker.service

[Service]
Type=oneshot
ExecStart=/bin/mkdir -p /dev/binderfs
ExecStart=/bin/mount -t binder binder /dev/binderfs
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF
    sudo systemctl daemon-reload
    sudo systemctl enable --now xphone-binderfs.service
    ok "已创建 xphone-binderfs.service 并开机自启"
  fi
  ok "modules-load.d / modprobe.d 已写入"
fi

h "完成"
cat <<'EOF'
下一步：
  1) 构建镜像：  ./scripts/build.sh
  2) 启动容器：  docker compose up -d
  3) 验证：      ./scripts/verify.sh
EOF
