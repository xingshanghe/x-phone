# 二进制层伪造素材（配合 LD_PRELOAD hook 库）

| 文件 | 作用 | 生效条件 |
|---|---|---|
| `meminfo` | 覆盖 `/proc/meminfo`（内存大小/占用） | `STEALTH_PRELOAD=1` 且文件存在 |
| `cpuinfo` | 覆盖 `/proc/cpuinfo`（核数/CPU part/Hardware） | 同上 |
| `stat`、`version` | 可选，覆盖 `/proc/stat`、`/proc/version` | 同上 |
| `props.map` | native 层属性覆盖表 | 同上 |
| `cmdline` | 伪造 `/proc/cmdline` | `STEALTH_FAKE_CMDLINE=1`（多数环境不允许 bind 到 procfs，失败会打日志并跳过） |
| `disable` | **保险丝**：此文件存在时 hook 库全部直通（失效但不崩溃）。紧急恢复：`touch config/fake/disable && docker restart <容器>` | 立即（对新启动的进程） |

★ 全部需要按目标机型替换：

- `meminfo` 的 `MemTotal` 必须与 `device.env` 的 `DEVICE_*` 机型规格一致（Pixel 8 ≈ 7.4 GiB）。
- `cpuinfo` 的 `processor` 段数量 = `XFAKE_CPU_COUNT`；`CPU part` / `Hardware` 要与机型 SoC 对得上。
- 三个数字必须互相对齐，否则等于自造矛盾：
  `config/fake/meminfo:MemTotal` ≈ `XFAKE_MEM_MB` × 1024；`cpuinfo` 核数 = `XFAKE_CPU_COUNT`。

验证（hook 生效后应看到伪造值）：

```sh
adb shell cat /proc/meminfo | head -1     # 应等于本文件的 MemTotal
adb shell cat /proc/cpuinfo | grep -c processor
adb shell cat /proc/cpuinfo | grep Hardware
```
