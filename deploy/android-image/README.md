# x-phone · Docker 中运行高度定制 Android：定制镜像方案

基于 **redroid（AOSP in Docker）** 的云手机基础镜像方案。单架构基线：**amd64 / Android 14**。
所有定制点都做到「改配置 → 重启容器」即可生效，不需要重建镜像，也不需要重编 AOSP。

---

## 1. 交付物与目录结构

```
deploy/android-image/
├── Dockerfile                  # 分层构建（base → prune → fonts → sysapps → media → scripts）
├── docker-compose.yml          # 公共层（多实例复制改端口与数据目录）
├── docker-compose.dev.yml      # dev 覆盖层：本地测试（小资源、弱加固）
├── docker-compose.prod.yml     # prod 覆盖层：Linux 生产（大资源、always 重启）
├── docker-compose.gpu.yml      # 可选附加层：/dev/dri GPU 直通
├── .env.example                # 端口 / 目录 / 资源上限 / XPHONE_ENV
├── .dockerignore
├── entrypoint.sh               # 容器 PID 1：开机前定制 + 组装 init 参数，最后 exec /init
├── scripts/
│   ├── up.sh                   # ★ 按环境启动：./scripts/up.sh dev|prod [--gpu]（带预检）
│   ├── post-boot.sh            # 开机后定制（由 init.rc 在 sys.boot_completed=1 触发）
│   ├── host-setup.sh           # 宿主机内核模块 / binderfs / GPU 检查与准备
│   ├── build.sh                # 可复现构建（支持 --flatten 真正瘦身）
│   ├── verify.sh               # 验证清单自动化（PASS/FAIL 计数，非 0 退出）
│   ├── stealth-audit.sh        # 真机化自检：站在检测方视角列出风险项
│   ├── capture-props.sh        # 从真机抓取整套属性，生成自洽的机型 profile
│   ├── test-preload.sh         # 二进制层单实例验证：冒烟 -> 开启 -> 看门狗 -> 失败自动回滚
│   └── make-bootanimation.sh   # 生成 bootanimation.zip
├── native/xphone-fake/         # 二进制层 hook 库（自研，LD_PRELOAD；默认关闭）
├── docs/stealth.md             # 真机化加固：检测面清单与逐项对策
└── config/                     # ★ 持久化挂载的配置目录（整体挂到 /xphone，只读挂载即可）
    ├── device.env              # 机型/指纹/分辨率/语言/时区/root/加固开关（主要改这里）
    ├── prune-apps.txt          # 精简系统应用清单
    ├── prune-traces.txt        # 容器 / 模拟器痕迹清理清单
    ├── bootanimation.zip       # 开机动画（占位，★替换成自己的）
    ├── props/
    │   ├── runtime-ro.prop     # 运行时注入的 ro.* 属性（改完 restart 生效）
    │   └── build-extra.prop    # 构建期写入 build.prop 的属性（可选）
    ├── permissions/            # 系统能力声明（hasSystemFeature 数据来源）
    ├── fake/                   # 二进制层伪造素材：meminfo / cpuinfo / props.map / cmdline
    ├── profiles/               # 环境分层 + 机型 profile 库
    │   ├── dev.env             # dev：弱加固、小分辨率、adb root、permissive
    │   ├── prod.env            # prod：真机化全开、enforcing、magisk、1080×2400
    │   └── <机型>.env          # 机型 profile 库（capture-props.sh 产出，可再叠加）
    ├── native/<arch>/          # 编译好的 hook 库（放这里才会在启动时被装进 /system/lib64）
    ├── apps/system/            # 系统级预装 APK（不可卸载）+ expect-packages.txt
    ├── apps/user/              # 用户级预装 APK（pm install，可卸载）
    ├── fonts/                  # 中文字体（缺失时放这里）
    └── init.d/                 # 开机后自定义钩子脚本
```

---

## 2. 调研与选型

### 2.1 候选方案对比

| 维度 | **redroid（选型）** | Damru（akwin1234/damru） | Waydroid | budtmo/docker-android | Anbox / Anbox Cloud | Cuttlefish / 官方模拟器容器 |
|---|---|---|---|---|---|---|
| 架构 | Android userspace 直接跑在宿主内核的 Docker 容器，**无 QEMU、无 KVM** | 不是容器方案：redroid 之上的 Python 自动化层（Playwright + CDP 驱动真机 Chrome） | LXC 容器 + Wayland 窗口集成 | QEMU 模拟器 + X11 + noVNC | LXC + 自研 HAL | QEMU + 大量虚拟 HAL |
| 内核依赖 | `binder_linux`（或 binderfs）；ashmem 可选（≥5.18 用 memfd 替代） | 同 redroid（它依赖 redroid） | binder + ashmem + Wayland 合成器 | **KVM 必需** | binder + 特定内核补丁 | KVM + 虚拟化全套 |
| Android 版本 | 8.1 – 16（官方镜像在维护）；**14 为稳定甜点** | 固定 Android 14 | LineageOS 13/14 | 9 – 16（x86_64） | 10 – 13 | 跟随 AOSP 最新 |
| 性能开销 | 无指令翻译；单实例空闲约 0.6–1.2 GB；冷启动 20–60 s；CPU 接近原生 | 同 redroid + 浏览器自动化开销 | 桌面体验最好，但不面向多实例云化 | 重：冷启动 1–3 min，内存 4–8 GB | 中 | 重 |
| 二次定制难度 | **低**：`/system` 在容器层可写；支持 `ro.xxx=` 命令行注入（init 阶段生效）与 `androidboot.redroid_*`；社区有 redroid-script 注入 Magisk/GApps/libndk | 低（用它的 API），但**只能按它的场景用** | 中：需改 `/var/lib/waydroid` 系统镜像 | 中：环境变量配设备皮肤，改系统要重打镜像 | 高 | 高（需 AOSP 构建能力） |
| 维护 / 授权 | 活跃，Apache-2.0 | 活跃但 **Beta**，**PolyForm Noncommercial 1.0.0（商用需另行授权）** | 活跃（桌面场景） | 活跃（偏测试） | 桌面版 2023 已废弃；Cloud 为 Canonical 商业版 | Google 官方活跃 |

### 2.2 结论

**选型：以 `redroid/redroid:14.0.0-latest` 为基座，自研定制层（本目录）。**

理由：

1. **Docker 原生**——与 x-phone 现有「Go 控制面 + Python 节点工具 + 每实例一个 ADB 端口」的架构天然契合，多实例就是多容器。
2. **定制成本最低**——redroid 把 init 读取内核 cmdline 的来源改成 `/proc/self/cmdline`，任何 `ro.xxx=yyy` 容器参数都会在 **init 阶段**写入属性系统。机型、品牌、指纹、ABI、GPU 名全部运行时可配，**不需要 Magisk、不需要 resetprop、不需要重建镜像**。
3. **无 KVM 依赖**——共享宿主内核，不做指令虚拟化，密度与启动速度都优于 QEMU 系。
4. **生态成熟**——redroid-script 可注入 Magisk / GApps / libndk / Widevine；scrcpy 生态成熟，画面与控制都有现成方案。

### 2.3 对 Damru 的借鉴与规避

Damru 的三层思路值得借鉴，但不能直接依赖：

| Damru 手段 | 本方案做法 | 说明 |
|---|---|---|
| OS 层属性伪装（`resetprop` 改 `ro.*`） | **用 redroid 的 `ro.*` cmdline 注入** | 更早（init 阶段）、更稳，且不依赖 Magisk |
| 155 个设备 profile | `config/device.env` + 每实例一份 | 与本仓库 `profiles/*.json` 字段对齐（brand/model/device/product/fingerprint/width/height/dpi/locale/timezone） |
| 二进制层（`LD_PRELOAD` libfakemem.so、替换 GPU `.so`） | **不内置** | 稳定性与合规风险高；确有需要时作为可选扩展，用 `config/init.d/` 钩子挂载自己的 `.so` |
| CDP / Playwright 自动化 | 不在镜像职责内 | 由 x-phone 控制面与节点工具负责 |

⚠ **授权红线**：Damru 为 PolyForm Noncommercial 1.0.0，**商用必须另行取得授权**。本方案只参考其公开思路，不引用其代码，也不安装其包。

### 2.4 深度定制：Damru 三层对照与本方案的落点

| Damru 层 | 做法 | 本方案等价实现 | 位置 |
|---|---|---|---|
| **OS 属性层** | Magisk `resetprop` 改 `ro.*` | redroid 的 `ro.*` cmdline 注入（**init 阶段生效，比 resetprop 更早，且不依赖 Magisk**）+ `/system/build.prop` 同步回写 | `entrypoint.sh` §2.3 / §3.5 |
| **二进制 / 驱动层** | 替换 GPU `.so`、`LD_PRELOAD libfakemem.so` | 自研 hook 库：改 GLES 渲染器字符串、重定向 `/proc/meminfo`、`/proc/cpuinfo`、`sysconf`、native 属性读取 | `native/xphone-fake/`（默认关闭） |
| **硬件规格层** | CPU / 内存 / 触摸点伪装 | `XFAKE_CPU_COUNT`、`XFAKE_MEM_MB`、`config/fake/meminfo`、`cpuinfo`；GPU 走 `ro.hardware.egl` + GLES hook | `config/device.env`、`config/fake/` |
| **CDP / 浏览器层** | Playwright 接管 Chrome | 不在镜像职责内，由 x-phone 控制面负责；**必须与镜像用同一份机型 profile**，否则浏览器与 OS 指纹互相矛盾 | 控制面 |

两处容易踩的坑，本方案已处理：

1. **`/system/build.prop` 必须与运行时属性双写** —— 检测会读文件再和 `getprop` 交叉比对，只改一边必被抓。
2. **`LD_PRELOAD` 必须在 `on early-init` 设置** —— 晚于这个时机 zygote 已经启动，子进程不会继承，hook 等于没生效。

### 2.5 真机化加固（抗容器 / 模拟器检测）

完整检测面清单与逐项对策见 **[docs/stealth.md](docs/stealth.md)**，自检脚本 `./scripts/stealth-audit.sh`。

⚠ **架构硬约束**：要做到"贴近真机"，**宿主必须是 arm64**。x86_64 主机上 `/proc/cpuinfo` 永远暴露
Intel/AMD，`Build.SUPPORTED_ABIS` 与伪装机型自相矛盾，改多少属性都兜不住。

⚠ **合规边界**：本方案用于自有云手机的合规测试、自动化与账号隔离；不得用于欺诈、刷量或绕过风控从事违法活动。
无法伪造的维度（Play Integrity 硬件级认证、真实基带与 IMEI、相机与传感器、`/proc/self/mountinfo` 容器路径）已在文档中明确列出，请勿做无效投入。

---

## 3. 定制内容与修改位置

| # | 定制项 | 修改位置 | 生效时机 | 备注 |
|---|---|---|---|---|
| 1 | **root 权限** | `config/device.env` 的 `ROOT_MODE` → 注入 `ro.secure=0 / ro.adb.secure=0 / ro.debuggable=1` | 重启容器 | `adb shell` 直接是 uid 0；应用侧需要 `su` 时用 `ROOT_MODE=magisk`（见 3.1） |
| 2 | **精简系统应用** | `config/prune-apps.txt`（相对 `/system` 的路径） | 构建期 + 每次启动前 | 删除只减运行时占用，不减镜像体积（见 6.5 的 --flatten） |
| 3 | **替换系统应用** | 删除 `priv-app/Launcher3` + 把自家 APK 放 `config/apps/system/`，并设 `DEFAULT_LAUNCHER` | 重启容器 | 默认桌面由 `cmd package set-home-activity` 设置 |
| 4 | **开机动画** | `config/bootanimation.zip` → `/system/media/bootanimation.zip` | 容器启动前复制 | 用 `scripts/make-bootanimation.sh` 生成；删除该文件则用系统默认动画 |
| 5 | **预装 APK（不可卸载）** | `config/apps/system/*.apk` → `/system/priv-app/<Name>/<Name>.apk` | 容器启动前复制 | 也可在构建期烤进镜像层（`BAKE_SYSTEM_APPS=1`） |
| 6 | **预装 APK（可卸载）** | `config/apps/user/*.apk` → 开机后 `pm install -r -g` | 开机完成后 | 幂等；`-g` 自动授予运行时权限 |
| 7 | **机型 / 指纹** | `config/device.env`（`DEVICE_*`）→ `ro.product.*` / `ro.build.fingerprint` 等，**同时写 `ro.product.*` 与 `ro.product.<分区>.*` 两套** | 重启容器 | Android 8+ 分区属性与聚合属性都写，避免 `Build.*` 与 `getprop` 不一致 |
| 8 | **分辨率 / DPI** | `config/device.env` 的 `WIDTH / HEIGHT / DPI / FPS` → `androidboot.redroid_*` | 重启容器 | 也可用 `adb shell wm size/density` 临时改 |
| 9 | **GPU 型号字符串** | `DEVICE_GPU` → `ro.hardware.egl` | 重启容器 | 仅影响属性面；应用内 GLES 查询可能仍返回宿主渲染器 |
| 10 | **中文字体** | `config/fonts/*.ttf|otf|ttc` → `/system/fonts/`；换字体需改 `/system/etc/fonts.xml` | 重启容器 | AOSP 通常已自带 NotoSansCJK，先确认再放 |
| 11 | **时区 / 语言默认值** | `config/device.env` 的 `TIMEZONE / LOCALE` → `persist.sys.timezone`、`persist.sys.locale`，由生成的 `/system/etc/init/xphone.rc` 在 `post-fs-data` 设置 | 重启容器 | `persist.*` 不能用 cmdline 注入，故走 init.rc；首次开机前设置才完全生效 |
| 12 | **代理 / 云手机行为** | `HTTP_PROXY_HOST/PORT`、`KEEP_SCREEN_ON`、`DISABLE_LOCKSCREEN`、`DISABLE_ANIMATION` | 开机后（post-boot） | 也可放 `config/init.d/*.sh` 做任意扩展 |

### 3.1 root 的三种模式

| ROOT_MODE | 做法 | 获得什么 | 代价 |
|---|---|---|---|
| `adb`（默认） | 注入 `ro.secure=0 / ro.adb.secure=0 / ro.debuggable=1` | `adb shell` 即 root，`adb root` 可用，容器内所有 shell 操作无障碍 | 应用内 `Runtime.exec("su")` 不可用（`/system` 挂载 nosuid，简单放个 su 二进制没用） |
| `magisk` | 先用 redroid-script 生成带 Magisk 的基础镜像，再把它作为 `BASE_IMAGE` | 应用级 `su`、Zygisk 模块、LSPosed、`resetprop`（运行时改 `ro.*`） | 需要额外构建步骤，镜像更大，开机略慢 |
| `none` | 保持 `ro.secure=1 / ro.debuggable=0` | 对 Play Integrity / 应用反作弊更友好 | 无 root |

生成 Magisk 基础镜像（一次性，可复用）：

```sh
git clone https://github.com/ayasa520/redroid-script && cd redroid-script
pip3 install -r requirements.txt
python3 redroid.py -a 14.0.0 -m            # -m=Magisk，还可加 -n(libndk) -w(Widevine L3)
# 假设产出 redroid/redroid:14.0.0_magisk
cd ../../deploy/android-image
BASE_IMAGE=redroid/redroid:14.0.0_magisk ./scripts/build.sh --tag 14-magisk
```

---

## 4. 宿主机前置条件

### 4.1 必须

| 项 | 要求 | 检查 / 准备 |
|---|---|---|
| 操作系统 | **Linux（Ubuntu 22.04 / 24.04）x86_64** | `uname -srm` |
| ⚠ macOS / Windows | **不可行** | Docker Desktop 的内核 VM 没有 binder/binderfs。开发机是 macOS 时，必须把镜像部署到 Linux 主机（云主机 / 物理机 / 带自定义内核的 WSL2） |
| 内核模块 | `binder_linux` | `sudo apt install linux-modules-extra-$(uname -r)`；`sudo modprobe binder_linux devices="binder,hwbinder,vndbinder"` |
| binder 设备 | `/dev/binder`、`/dev/hwbinder`、`/dev/vndbinder` | Ubuntu 22.04+ 通常没有节点，需挂载 binderfs：`sudo mount -t binder binder /dev/binderfs` |
| ashmem | 可选 | 内核 ≥ 5.18 已移除，用 `androidboot.use_memfd=1`（本方案默认开启） |
| Docker | 20.10+ | `docker version` |
| 资源 | 每实例建议 2 vCPU / 2–4 GB / 8 GB 磁盘 | `nproc`、`free -h`、`df -h /var/lib/docker` |

一键准备：`./scripts/host-setup.sh`（加 `--persist` 会写入 modules-load.d 与 binderfs 的 systemd 单元，开机自动生效）。

### 4.2 硬件加速（重要澄清）

- **redroid 不需要 KVM。** 它共享宿主内核，不做指令虚拟化；`/dev/kvm` 是否存在都无所谓。只有 QEMU 系方案（docker-android、Cuttlefish、官方模拟器）才依赖 KVM。
- **GPU 直通**：`GPU_MODE=host` + 挂载 `/dev/dri`（compose 里打开对应 volume），渲染由宿主 Mesa/ Vulkan 驱动完成。
  - Intel / AMD：通常可用，性能明显好于软件渲染。
  - **NVIDIA 独显常见花屏 / 黑屏**，建议保持 `GPU_MODE=guest`。
- **软件渲染**：`GPU_MODE=guest`（默认，ANGLE / SwiftShader），任何机器都能跑，30 fps 足够云手机自动化使用。
- **x86 主机跑 arm-only APK**：需镜像内含 libndk / libhoudini，并在 `device.env` 打开 `NATIVE_BRIDGE=libndk_translation.so` 及相应 ABI 列表。

---

## 5. ★ 需要你按自身需求替换的参数

| 参数 | 位置 | 默认 | 说明 |
|---|---|---|---|
| `DEVICE_MODEL / BRAND / MANUFACTURER / DEVICE / PRODUCT / BOARD` | `config/device.env` | Pixel 8 系列 | 机型伪装，决定 `Build.*` 与 `getprop ro.product.*` |
| `DEVICE_FINGERPRINT` | `config/device.env` | google/shiba/… | 设备指纹，一行定生死，必须与 model/device/brand 自洽 |
| `DEVICE_SERIAL` | `config/device.env` | R8CTX0A0A0A | `adb devices` 与 `ro.serialno` 可见 |
| `DEVICE_GPU` | `config/device.env` | Mali-G715 | `ro.hardware.egl` |
| `WIDTH / HEIGHT / DPI / FPS` | `config/device.env` | 1080 / 2400 / 420 / 30 | 分辨率与帧率 |
| `TIMEZONE / LOCALE` | `config/device.env` | Asia/Shanghai / zh-CN | 已预置中文默认值 |
| `DEVICE_ABILIST*` / `NATIVE_BRIDGE` | `config/device.env` | x86_64 优先 | 跑 arm APK 时调整 |
| `ROOT_MODE` | `config/device.env` | adb | 见 3.1 |
| `XPHONE_ENV` | `.env` | dev | `dev`=本地测试 / `prod`=Linux 生产；决定加载 `config/profiles/<env>.env` 与 compose 覆盖层，见 6.2 |
| `ADB_PORT` / `ADB_BIND_ADDR` | `.env` | 18120 / 127.0.0.1 | x-phone 端口规范：ADB 用 18120–18219 |
| `DATA_DIR` | `.env` | ./runtime/data | Android `/data`，务必持久化 |
| `CPU_LIMIT / MEM_LIMIT` | `.env` | 2.0 / 4g | 按宿主规格调 |
| `IMAGE / TAG` | `.env` | xphone/android:14-custom | 镜像名 |
| `ANDROID_TAG / BASE_IMAGE` | `scripts/build.sh` 环境变量 | 14.0.0-latest | 换 Android 版本或换成 Magisk 基础镜像 |
| `CJK_FONT_URL` | `scripts/build.sh` 环境变量 | 空 | 构建期下载中文字体；留空则用 `config/fonts/` |
| `config/bootanimation.zip` | 文件替换 | 占位动画 | 用 `scripts/make-bootanimation.sh` 生成自己的 |
| `config/prune-apps.txt` | 行内增删 | 保守清单 | 逐个打开验证 |
| `config/apps/system|user/expect-packages.txt` | 新增文件 | 无 | 供 `verify.sh` 校验包名 |
| `DEVICE_BOOTLOADER` / `DEVICE_BASEBAND` / `DEVICE_SECURITY_PATCH` / `DEVICE_INCREMENTAL` | `config/device.env` | 示例值 | **建议用 `capture-props.sh` 从真机抓**，手编必然自相矛盾 |
| `XFAKE_MEM_MB` / `XFAKE_CPU_COUNT` / `config/fake/meminfo` / `cpuinfo` | `config/device.env` + `config/fake/` | 8GB / 8 核 | 三个数字必须互相对齐 |
| `STEALTH_PRELOAD` | `config/device.env` | 0 | 二进制层开关；启用流程：`build-ndk.sh` → `test-preload.sh` → `test-preload.sh --enable` |
| `SELINUX` | `config/device.env` | enforcing | permissive 本身是检测特征；xattr 不可用时自动降级并告警 |

---

## 6. 构建与启动（可复现）

### 6.1 在哪台机器上构建：Mac 还是 Linux？

三件事要分开看，**只有运行必须在 Linux**，构建和交叉编译 Mac 都能做：

| 环节 | Mac（Apple Silicon） | Mac（Intel） | Linux（arm64，推荐） | 原因 |
|---|---|---|---|---|
| ① NDK 编译 hook 库 `.so` | ✅ 推荐 | ✅ | ✅ | 纯交叉编译，不需要 Docker / 内核。NDK macOS 版 host tag 是 `darwin-x86_64`，Apple Silicon 需 Rosetta 2（`softwareupdate --install-rosetta`）；脚本会检测并提示 |
| ② `docker build` 镜像 | ✅ 可构建 **arm64** 镜像（Docker Desktop 的 VM 是 arm64） | ⚠ 只能稳构建 **amd64** 镜像 | ✅ | Dockerfile 里 `SHELL ["/system/bin/sh","-c"]`，所有 RUN 都在 **Android rootfs 内执行**，构建机 CPU 必须跑得动镜像架构的二进制 |
| ③ `docker run` 跑容器 | ❌ 不行 | ❌ 不行 | ✅ 必须 | Docker Desktop 用的是 linuxkit VM 内核，**无法加载 `binder_linux` / `ashmem` 模块**，`/dev/binder` 不存在，Android init 直接卡死 |

补充条件：**构建机上不需要任何内核模块**（build 阶段不启动 Android），只需要 Docker 20.10+（BuildKit）+ 约 8 GB 空闲磁盘（redroid 基础镜像 1 GB+，`--flatten` 再留一倍）。
跨架构构建（`PLATFORM=linux/arm64` 在 x86 机器上）要走 QEMU 模拟 bionic 二进制，失败率高，**不推荐**；需要 arm64 镜像就用 arm64 机器构建。

推荐工作流（Mac 开发 + Linux 服务器运行）：

```sh
# A. 在 Mac 上：只做交叉编译与配置，不跑容器
NDK=~/Library/Android/sdk/ndk/27*/ ./native/xphone-fake/build-ndk.sh   # 产出 config/native/aarch64/libxphone_fake.so
git add -A && git commit -m "..." && git push

# B. 在 arm64 Linux 服务器上：构建 + 运行
git pull && ./scripts/build.sh && docker compose up -d && ./scripts/verify.sh
```

不想在服务器构建时，也可以在 Mac 上建好后传过去（buildkit 缓存不会丢，但传的是整层，约 1–2 GB）：

```sh
PLATFORM=linux/arm64 ./scripts/build.sh          # Apple Silicon 上构建 arm64
docker save xphone/android:14-custom | gzip | ssh <server> 'gunzip | docker load'
```

★ 大多数改动**根本不用重建镜像**：`config/` 是只读挂载进去的，改机型 / 指纹 / 分辨率 / 语言 / APK / 开机动画后 `docker compose restart` 即可。只有 `config/prune-apps.txt`、`config/apps/system`（要 bake 进镜像层）、`entrypoint.sh` / `post-boot.sh` 变更才需要 `build.sh`。

### 6.2 运行环境分层：dev / prod

同一份镜像、同一份 Dockerfile，用 `XPHONE_ENV` 切成两种运行形态。**Linux 是生产环境（prod），测试实例用 dev**——两者可以并存（不同容器名、不同 `/data`、不同端口）。

| 维度 | `dev`（本地测试） | `prod`（Linux 生产） |
|---|---|---|
| 配置来源 | `config/device.env` + `config/profiles/dev.env` | `config/device.env` + `config/profiles/prod.env` |
| 真机化 `STEALTH` | 0 | 1 |
| 二进制层 `STEALTH_PRELOAD` | 0（绝不注入，避免伪造值干扰排障） | 1（须先跑 `test-preload.sh --enable`） |
| `ROOT_MODE` | `adb`（`adb shell` 直接 root） | `magisk`（属性面保持 user 版） |
| `SELINUX` | `permissive` | `enforcing`（xattr 不可用自动降级） |
| `GPU_MODE` / 分辨率 | `guest` / 720×1280@15（省资源） | `auto` / 1080×2400@30 |
| compose 覆盖层 | `docker-compose.dev.yml`（1 vCPU / 2 GB / `unless-stopped`） | `docker-compose.prod.yml`（2 vCPU / 4 GB / `always`） |
| `/data` 目录 | `./runtime/data-dev` | `/var/lib/xphone/data` |
| 镜像 tag | `xphone/android:14-custom-dev` | `xphone/android:14-custom` |

用法（推荐用 `up.sh`，它会写 `.env`、预检环境、组合 compose 文件）：

```sh
cp .env.example .env

./scripts/up.sh dev                 # 测试实例：预检宽松，弱加固
./scripts/up.sh prod                # 生产实例：强制预检 Linux + binder + arm64，不满足直接报错退出
./scripts/up.sh prod --gpu          # 叠加 /dev/dri 直通（宿主有 GPU 时）
./scripts/up.sh dev --build         # 其余参数透传给 docker compose up

./scripts/build.sh --env dev        # dev 镜像 tag 自动带 -dev 后缀
./scripts/build.sh --env prod
```

手工等价命令：

```sh
docker compose -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.gpu.yml up -d
```

`prod` 的预检是硬门槛：非 Linux、无 `/dev/binder`（且 `lsmod` 无 `binder_linux`）会**直接退出**，不会带着坏配置起容器；宿主是 x86_64 时给出「抗检测打折」告警而不是拦死（功能仍可用）。

★ 切换环境不需要重建镜像，只影响运行时加载的 profile 与 compose 覆盖层。

### 6.3 准备宿主（Linux 主机）

```sh
cd deploy/android-image
chmod +x entrypoint.sh scripts/*.sh
sudo ./scripts/host-setup.sh --persist
```

### 6.4 构建镜像

```sh
# 常规
./scripts/build.sh

# 指定 Android 版本 / 基础镜像 / 中文字体直链
ANDROID_TAG=14.0.0-latest \
CJK_FONT_URL="https://example.com/NotoSansSC-Regular.otf" \
./scripts/build.sh --tag 14-custom

# 无缓存 + 扁平化瘦身（真正减小体积，代价：丢层缓存）
./scripts/build.sh --no-cache --flatten
```

等价的原生命令：

```sh
DOCKER_BUILDKIT=1 docker build \
  --build-arg ANDROID_TAG=14.0.0-latest \
  --build-arg CJK_FONT_URL="" \
  -t xphone/android:14-custom .
```

### 6.5 启动

```sh
cp .env.example .env          # 改端口 / 目录 / 资源 / XPHONE_ENV

./scripts/up.sh dev           # 推荐：写 .env + 预检 + 组合覆盖层（见 6.2）
# 或 ./scripts/up.sh prod [--gpu]

# 直接用 compose（不带预检）：
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
docker compose -f docker-compose.yml -f docker-compose.dev.yml ps   # 等 healthy
docker compose -f docker-compose.yml -f docker-compose.dev.yml logs -f   # 首次开机 30–90s
```

等价的原生命令：

```sh
docker run -itd --name xphone-android-01 \
  --privileged \
  -v "$(pwd)/runtime/data:/data" \
  -v "$(pwd)/config:/xphone:ro" \
  -p 127.0.0.1:18120:5555 \
  --cpus=2 --memory=4g \
  xphone/android:14-custom \
  androidboot.redroid_width=1080 \
  androidboot.redroid_height=2400 \
  androidboot.redroid_dpi=420 \
  androidboot.redroid_gpu_mode=guest \
  ro.product.model="Pixel 8"
```

> 体积说明：`docker build` 里删除文件只会产生 overlayfs whiteout，**镜像体积不会下降**。
> 要真正瘦身，用 `./scripts/build.sh --flatten`（export/import 压平所有层）。
> 同一宿主上的多个容器共享同一份镜像层，不会重复占磁盘。

### 6.6 多实例

```sh
for i in 1 2 3; do
  mkdir -p runtime/inst-$i
  cp -r config runtime/inst-$i/config
  sed -i "s/ADB_PORT=18120/ADB_PORT=1812$i/" runtime/inst-$i/.env   # 或逐份写 .env
done
# 每个实例：不同 CONTAINER_NAME / ADB_PORT(18120–18219) / DATA_DIR / config 目录
```

---

## 7. 持久化与配置目录

| 挂载点 | 用途 | 是否必须 | 说明 |
|---|---|---|---|
| `/data` | Android 数据分区（用户数据、已装应用、设置、账号） | **必须** | 不挂载 → 每次重建都重新初始化系统；镜像内置的 `/data` 内容会被覆盖，所以预装要走 `/system` 或 post-boot |
| `/xphone`（= `config/`） | 定制配置与素材：device.env、APK、字体、开机动画、init.d 钩子、props | 建议（只读挂载即可） | 改这里的任何内容，`docker compose restart` 即生效，**不用重建镜像** |
| `/dev/dri` | GPU 直通 | 可选 | 仅 `GPU_MODE=host` 时需要 |

运行时生成物：

- `/system/etc/init/xphone.rc` —— entrypoint 每次生成（勿手改），负责 `persist.*` 与触发 post-boot
- `/data/xphone/boot-complete` —— 开机完成标记，healthcheck 依赖它
- `/system/priv-app/<Name>/` —— 系统级预装应用落点

---

## 8. ADB 端口与远程画面

### 8.1 ADB

- 容器内固定 **5555/tcp**，宿主端口由 `.env` 的 `ADB_PORT` 映射（默认 `127.0.0.1:18120`）。
- **不要把 5555 暴露到公网**：ADB 未鉴权即可拿到 root shell，等同于把宿主暴露出去。
- 远程方式：SSH 隧道（`ssh -L 18120:127.0.0.1:18120 host`）、Tailscale / WireGuard，或只在内网监听。

```sh
adb connect 127.0.0.1:18120
adb -s 127.0.0.1:18120 shell id     # uid=0(root)
```

### 8.2 远程画面（三种，按推荐度）

1. **scrcpy（客户端，推荐）** —— 宿主机或运维机安装，走 ADB，不需要在容器里装东西：
   ```sh
   scrcpy -s 127.0.0.1:18120 --no-audio --max-size 1080 --window-title xphone-01
   ```
2. **ws-scrcpy（浏览器）** —— 适合给控制台做「设备墙」：
   ```sh
   git clone https://github.com/NetrisTV/ws-scrcpy && cd ws-scrcpy && npm install && npm start
   # 浏览器打开 http://<host>:8080，连接 <host>:18120
   ```
3. **容器内 VNC（可选）** —— 预装 DroidVNC-NG 之类的 APK 作为系统应用，暴露 5900 + 宿主侧 noVNC。
   注意：Android 10+ 的屏幕采集走 MediaProjection，需要用户授权或系统级签名，云手机无交互场景下**需自行实测**，不作为默认方案。

---

## 9. 验证清单

```sh
./scripts/verify.sh                       # 跑完整清单（PASS/FAIL 计数，失败非 0 退出）
./scripts/verify.sh --shot /tmp/s.png     # 额外截图，确认渲染管线
```

| # | 验证项 | 命令 | 通过标准 |
|---|---|---|---|
| 1 | 容器可启动 | `docker ps --filter name=xphone-android-01` | 状态 `running`，healthcheck `healthy` |
| 2 | Android 开机完成 | `adb shell getprop sys.boot_completed` | `1` |
| 3 | ADB 可连接 | `adb connect 127.0.0.1:18120 && adb -s ... get-state` | `device`（不是 `offline`/`unauthorized`） |
| 4 | root 生效 | `adb shell id -u` | `0` |
| 5 | 机型/指纹生效 | `adb shell getprop ro.product.model` / `ro.build.fingerprint` | 与 `config/device.env` 一致 |
| 6 | 分辨率/DPI 生效 | `adb shell wm size` / `wm density` | `1080x2400` / `420` |
| 7 | 时区/语言生效 | `adb shell getprop persist.sys.timezone` / `persist.sys.locale` | `Asia/Shanghai` / `zh-CN` |
| 8 | 预装应用生效 | `adb shell pm path <包名>` | 有路径输出；系统应用路径含 `/system/priv-app/` |
| 9 | 字体生效 | `adb shell ls /system/fonts \| grep -iE 'cjk\|NotoSansSC'` | 有 CJK 字体；截图中文无方框 |
| 10 | 开机动画生效 | `adb shell ls -l /system/media/bootanimation.zip` | 文件存在且大小与 `config/bootanimation.zip` 一致 |
| 11 | 渲染可用 | `adb exec-out screencap -p > /tmp/s.png` | PNG 非空 |

排障入口：`docker logs xphone-android-01`、`adb shell logcat -b all | grep -iE 'fatal|crash|xphone'`、官方工具
`curl -fsSL https://raw.githubusercontent.com/remote-android/redroid-doc/master/debug.sh | sudo bash -s -- <容器>`。

---

## 9.1 生成 / 应用机型 profile（推荐流程）

```sh
# 1) 用一台目标真机抓整套属性（USB 调试或网络 ADB 均可）
./scripts/capture-props.sh --serial 0123456789ABCDEF --name pixel_8 --apply
#   产物：config/profiles/pixel_8.env（→ config/device.env）
#        config/profiles/pixel_8.ro.prop（→ config/props/runtime-ro.prop）

# 2) 重启使属性生效
docker compose restart && ./scripts/verify.sh

# 3) 站在检测方视角自检
./scripts/stealth-audit.sh --verbose
```

不要手编 fingerprint：bootloader / baseband / security patch / incremental / first_api_level 之间必须自洽。

## 10. 已知限制与取舍

- **无 Play Integrity / SafetyNet 认证**：容器环境 + root 属性基本不可能通过强完整性校验。若要跑强校验应用，需要 GApps + Magisk + Play Integrity Fix 的完整对抗链，且不稳定。
- **无相机 / GPS / 基带**：没有真实基带与 IMEI，`getprop` 层面的伪装无法骗过读取基带信息的检测。
- **GPU 型号只是属性**：`ro.hardware.egl` 只影响属性面，应用内 GLES/Vulkan 查询可能仍返回宿主渲染器（软件渲染时返回 SwiftShader/ANGLE）。
- **SELinux**：默认 `enforcing`（真机即如此，permissive 是检测特征）。entrypoint 对新增的字体/APK/rc/脚本/hook 库统一打 `u:object_r:system_file:s0` 标签，并用 canary 文件实测 chcon —— 宿主文件系统不支持 `security.*` xattr 时自动降级为 permissive 并在日志告警，优先保证能开机。
- **Magisk 非必须**：机型/指纹/分辨率等定制完全靠 redroid 的 `ro.*` 注入即可，Magisk 只在需要应用级 `su` / Zygisk 模块 / `resetprop` 时才引入。
- **首次开机慢**：`/data` 为空时要做 dexopt，30–90 s 属正常；后续启动 10–30 s。
- **性能数字为经验值**：0.6–1.2 GB 空闲内存、20–60 s 冷启动等均需在你的宿主上实测校准。
- **真机化加固的硬边界**（详见 `docs/stealth.md`）：
  - 宿主为 x86_64 时无法伪装成 arm 真机（`/proc/cpuinfo` 暴露）。
  - Play Integrity / SafetyNet 硬件级认证、真实基带与 IMEI、相机与传感器数据不可伪造。
  - `/proc/self/mountinfo` 的容器路径无法在容器内抹除。
  - `STEALTH_PRELOAD=1` 会把 hook 库注入全部 native 进程，缺陷可能导致开机循环 —— 默认关闭；用 `scripts/test-preload.sh --enable` 做单实例验证（冒烟 + 看门狗 + 失败自动回滚），另有 `config/fake/disable` 文件保险丝兜底。
