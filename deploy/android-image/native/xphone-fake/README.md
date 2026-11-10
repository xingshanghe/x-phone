# 二进制层 hook 库（自研，对应 Damru 的「binary / driver 层」思路）

## 为什么需要这一层

只改 Android 属性挡不住在 native 层取证的检测。典型取证点：

| 取证点 | 不处理时的暴露内容 |
|---|---|
| `glGetString(GL_RENDERER)` | `SwiftShader` / `ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device…))` —— 一读就知道是软件渲染容器 |
| `/proc/meminfo`、`/proc/cpuinfo` | 宿主真实内存与 CPU（Intel/AMD、宿主核数），与伪装机型直接矛盾 |
| `sysconf(_SC_NPROCESSORS_CONF)` | 宿主核数 |
| `__system_property_get()` | 绕过 Java `Build`，直接在 native 读属性区；`ro.*` 注入同样可见，但非 `ro` 属性与兜底逻辑挡不住 |

Damru 的做法是「替换 GPU 驱动 `.so` + `LD_PRELOAD libfakemem.so`」。本方案用同一个思路，
但**自己实现**，规避其 PolyForm Noncommercial 授权限制，也不引入不受控的第三方二进制。

## 编译

```sh
NDK=~/Library/Android/sdk/ndk/27.0.12077973 ./native/xphone-fake/build-ndk.sh
# 产物：config/native/aarch64/libxphone_fake.so
```

★ 架构必须与镜像一致（arm64 镜像 → `ARCH=aarch64`；x86_64 镜像 → `ARCH=x86_64`）。
若要编进 AOSP/镜像层，改用同目录的 `Android.mk`。

## 生效链路

1. 库文件放 `config/native/<arch>/libxphone_fake.so`
2. `config/device.env` 设 `STEALTH_PRELOAD=1`
3. entrypoint 启动时把库复制到 `/system/lib64/libxphone_fake.so`
4. entrypoint 生成的 `xphone.rc` 在 `on early-init` 执行
   `setenv LD_PRELOAD /system/lib64/libxphone_fake.so` —— 必须在这个时机，
   zygote 与其全部子进程（含 adbd）才会继承
5. 同时 entrypoint 会把 `device.env` 里的 `XFAKE_*` 变量 `setenv` 进 init 全局环境
   —— 这一步不能省：device.env 只被 entrypoint 这个 shell 读取，Android 进程
   原本完全看不到这些变量
6. 伪造素材从 `config/fake/`（由 `XFAKE_DIR` 指定）读取

## 安全启用流程（勿跳步）

```sh
./scripts/test-preload.sh            # 1. 免重启冒烟：单进程 LD_PRELOAD，无副作用
./scripts/test-preload.sh --enable   # 2. 开启+重启+等开机+验证；失败自动回滚为 0
./scripts/test-preload.sh --disable  # 3. 手动回滚
```

## 保险丝（kill switch）

库内置两道保险，触发后**全部 hook 变为纯直通**（失效但不崩溃）：

- **文件保险丝**：`config/fake/disable` 存在即直通。出问题在宿主执行
  `touch config/fake/disable && docker restart <容器>` 即可恢复，不用改任何配置。
- **环境保险丝**：`XFAKE_DISABLE=1`（在 device.env 里加，entrypoint 会透传）。

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `XFAKE_DIR` | `/xphone/fake` | 伪造素材目录（`meminfo` / `cpuinfo` / `props.map`） |
| `XFAKE_CPU_COUNT` | 8 | `sysconf` 返回的核数 |
| `XFAKE_MEM_MB` | 8192 | `sysconf(_SC_PHYS_PAGES)` 换算来源 |
| `XFAKE_GL_VENDOR` / `XFAKE_GL_RENDERER` / `XFAKE_GL_VERSION` | Qualcomm / Adreno (TM) 740 | GLES 字符串 |
| `XFAKE_DEBUG` | 未设置 | `=1` 时把每次重定向打到 stderr（排障用） |

## 验证

开启后 adbd 也继承了 LD_PRELOAD，因此直接在 adb shell 里就能验：

```sh
adb shell head -1 /proc/meminfo            # 应等于 config/fake/meminfo 的 MemTotal
adb shell cat /proc/cpuinfo | grep -c processor
adb shell cat /proc/cpuinfo | grep Hardware
adb shell getprop ro.hardware.egl
./scripts/verify.sh                        # 第 9 节会自动校验 hook 是否生效
# 应用内 GLES 查询（可用任意 GL 检测类 App 或 glinfo 工具确认不再是 SwiftShader）
```

## ⚠ 风险与回滚

- 该库注入到系统**全部** native 进程（含 zygote、system_server）。任何缺陷都可能造成开机循环。
- **默认关闭**。用 `scripts/test-preload.sh --enable` 做单实例验证（带看门狗与自动回滚），
  上量前再跑满一个完整业务链路。
- 出问题立即：`./scripts/test-preload.sh --disable`，或文件保险丝
  `touch config/fake/disable && docker restart <容器>`。
- 若 `/system/lib64` 下已有同名文件，会被覆盖；来源始终是 `config/native/<arch>/`。

## 已知覆盖不到的点

- `/proc/self/maps` 中会暴露 `libxphone_fake.so` 自身（hook 了自己反而更显眼，故不处理 maps）。
- `/proc/self/mountinfo` 的容器路径。
- Vulkan `vkGetPhysicalDeviceProperties`（未 hook，需要时自行扩展）。
