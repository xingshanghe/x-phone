# 真机化加固（抗容器 / 模拟器检测）

目标：让容器在**可观测表面**上尽量贴近一台真实手机。做法分四层，按性价比排序。

> 边界声明：以下手段用于自有云手机的合规测试、自动化与账号隔离。
> 请勿用于欺诈、刷量或绕过风控从事违法活动。
> **无法伪造的维度**（不要在这些上面浪费时间）：Play Integrity / SafetyNet 硬件级认证、基带与真实 IMEI、
> 相机与传感器真实数据、`/proc/self/mountinfo` 里的容器路径、宿主内核。

---

## 0. 架构硬约束（先决定，其余都是空谈）

| 宿主 | 结果 |
|---|---|
| **arm64**（Ampere / Graviton / 飞腾 / Apple Silicon 上的 Linux） | ✅ 镜像与真机同为 arm64：`/proc/cpuinfo`、`Build.SUPPORTED_ABIS`、指令集、浮点行为全部自洽。这是唯一能"贴近真机"的地基。 |
| x86_64 | ❌ `/proc/cpuinfo` 暴露 GenuineIntel/AuthenticAMD；要跑 arm-only APK 必须开 libndk/libhoudini 翻译层，而翻译层本身（`/system/lib64/libndk_translation.so`、`ro.dalvik.vm.native.bridge`）就是最明显的检测特征。 |

**结论：抗检测场景请把云手机节点全部换成 arm64 主机。** 官方 redroid 镜像是多架构 manifest，arm64 主机 `docker pull` 自动拿到 arm64 层，tag 不用改。

---

## 1. 属性面（能改，且是最值钱的一层）

关键机制：redroid 允许把任意 `ro.xxx=yyy` 作为 `/init` 参数注入，在 **init 阶段**写进属性系统 —— 比 Magisk 的 `resetprop` 更早、更干净，且不需要 root。

| 检测点 | 处理 | 位置 |
|---|---|---|
| `ro.build.type=userdebug` / `ro.build.tags=test-keys` | 改成 `user` / `release-keys` | `config/device.env` |
| `ro.debuggable=1` / `ro.secure=0` | `ROOT_MODE=magisk` 时保持 `1`/`0`（root 交给 Magisk，属性面伪装成 user 版） | `config/device.env` |
| `ro.boot.verifiedbootstate` / `ro.boot.flash.locked` / `ro.boot.vbmeta.device_state` | 设为 `green` / `1` / `locked` | `config/device.env` |
| `ro.bootloader`、`ro.build.expect.bootloader` | 填真机 bootloader 版本（★从真机抓） | `config/device.env` |
| `gsm.version.baseband`、`ro.build.expect.baseband` | 填真机基带版本 | `config/device.env` |
| `ro.build.version.security_patch` / `incremental` / `ro.build.date.utc` | 与指纹中的 build id 自洽 | `config/device.env` |
| `ro.product.first_api_level` | 真机 30+，容器常缺失或为 0 | `config/device.env` |
| `ro.hardware` / `ro.boot.hardware` | 改成真机板名，不能出现 `ranchu`/`goldfish`/`redroid` | `config/device.env` |
| `ro.kernel.qemu`、`qemu.*` | 必须为 0 / 不存在 | entrypoint 强制注入 `ro.kernel.qemu=0` |
| `ro.product.<分区>.*` 与 `ro.product.*` 不一致 | 两套都写（Android 8+ 分区属性） | entrypoint 自动展开 |
| 指纹与 model/device/brand 不自洽 | `stealth-audit.sh` 会校验一致性 | 校验脚本 |

**不要手工编 fingerprint**：整套属性必须互相自洽，手编几乎必然露馅。用
`./scripts/capture-props.sh --serial <真机>` 从真机一次性抓取，生成可直接用的 profile（见 scripts 说明）。

### 1.1 最容易漏的一条：build.prop 也要同步改

只把属性注入运行时（`/init` 参数）是不够的 —— 检测会**直接读 `/system/build.prop` 文件**再与
`getprop` 结果交叉比对。只改一边，等于自己留了一个 100% 命中的特征。

entrypoint 会把本次注入的全部 `ro.*` 同步写回 `/system/build.prop`（`ro.vendor.*` 写 `/vendor/build.prop`），
用 `# >>> XPHONE-CUSTOM-BEGIN / END` 标记块保证幂等，每次启动先删旧块再写新块。

## 2. 文件与设备节点痕迹（能改）

| 检测点 | 处理 |
|---|---|
| `/dev/qemu_pipe`、`/dev/goldfish_pipe`、`/dev/qemu_trace` | entrypoint 启动前删除 |
| `/system/bin/*qemu*`、`*goldfish*`、`redroid-*` 等可执行文件 | `config/prune-traces.txt` 列出并删除 |
| libndk / libhoudini 翻译层 | arm64 宿主上根本不需要，不要开 `NATIVE_BRIDGE` |
| `/system/build.prop` 里的模拟器残留 | 用 `config/props/build-extra.prop` 覆盖关键项 |

发现痕迹的命令（在跑起来的容器里执行，把结果填进 `prune-traces.txt`）：

```sh
adb shell ls /dev | grep -iE 'qemu|goldfish'
adb shell ls /system/bin /system/lib64 | grep -iE 'qemu|goldfish|redroid|ndk_translation|houdini'
adb shell getprop | grep -iE 'qemu|goldfish|ranchu|redroid|emulator|virtual'
```

## 3. 系统能力声明（能改，但要注意副作用）

`PackageManager.hasSystemFeature()` 靠 `/system/etc/permissions/*.xml` 声明。AOSP 容器默认缺一大堆
（`android.hardware.wifi`、`telephony`、`bluetooth`、`camera`…），应用一查就露馅。

提供 `config/permissions/xphone-features.xml`，entrypoint 会复制到 `/system/etc/permissions/`。
默认只开**不会导致崩溃**的项（网络/电话/蓝牙/触摸屏/屏幕等）；
**camera / sensor 类默认注释**：声明了却没有真实 HAL，应用真去调用时会崩。

## 3.5 二进制层（LD_PRELOAD / GPU 字符串，能改但风险最高）

属性层再干净，应用只要调一次 `glGetString(GL_RENDERER)` 就会拿到 `SwiftShader`，
或读一次 `/proc/meminfo` 就看到宿主内存 —— 这一层必须在二进制层拦。

自研实现：`native/xphone-fake/`（C 源码 + NDK 编译脚本），hook 点：

| hook | 抹掉的特征 |
|---|---|
| `glGetString` | SwiftShader / ANGLE 渲染器字符串 |
| `open/open64/openat/fopen/fopen64` | `/proc/meminfo`、`/proc/cpuinfo`、`/proc/stat`、`/proc/version`、`/sys/class/net/eth0` |
| `sysconf` | `_SC_NPROCESSORS_*`、`_SC_PHYS_PAGES` |
| `__system_property_get` | native 侧属性读取（`config/fake/props.map` 覆盖任意 key） |

对应 Damru 的「GPU 驱动 `.so` 替换 + `LD_PRELOAD libfakemem.so`」，但代码自研、不引入第三方二进制。
默认关闭（`STEALTH_PRELOAD=0`），启用走 `scripts/test-preload.sh`（免重启冒烟 → 开启 →
看门狗等开机 → 失败自动回滚），库内置文件/环境两道保险丝（`config/fake/disable`），
详见 `native/xphone-fake/README.md`。

## 4. 运行时行为（能改）

| 检测点 | 处理 | 位置 |
|---|---|---|
| 电池常年 100% + charging | `dumpsys battery set level 78` + `unplug` + `status 3`（放电） | post-boot |
| 开发者选项开启 | `development_settings_enabled=0`（保留 `adb_enabled=1`） | post-boot |
| SELinux = permissive | 默认 `enforcing`；entrypoint 用 canary 文件实测 chcon，xattr 不可用时自动降级为 permissive 并告警 | `device.env` |
| 网络接口名 `eth0`、MAC OUI `02:42:` | 可选改名 `wlan0` / 自定义 MAC（`STEALTH_RENAME_NET`、`STEALTH_MAC`）**需实测 netd 兼容性** | entrypoint |
| 出口 IP 与语言/时区不一致 | 代理、时区、locale 三者必须同源配置 | `device.env` |
| 已装应用过少（纯 AOSP） | 补装浏览器/商店/输入法等常见应用 | `config/apps/` |
| GSF / android_id 缺失 | 需要 GApps（redroid-script 加 MindTheGapps），非必需 | README 3.1 |

## 5. 消除不了的（Know your limits）

- `/proc/self/mountinfo`：容器 rootfs 是 overlay，路径含 `/var/lib/docker/overlay2/...`，无法在容器内抹掉。
- 传感器列表、相机、NFC、GPS、真实基带与 IMEI。
- 宿主内核版本与容器 cgroup。
- Play Integrity 强校验（需要硬件背书 + GMS 认证链）。

## 6. 自检

```sh
./scripts/stealth-audit.sh           # 逐项打分，列出 RISK
./scripts/stealth-audit.sh --verbose # 打印实际取值
```

建议每次换机型 profile 后跑一遍，把 RISK 项当作待办清单。
