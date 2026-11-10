# 设备墙与多控设计方案

配套 ADR 见 `docs/adr/0003-device-wall-stream-and-control.md`（Proposed 状态，尚未实施，不作为群控的前置）。

目标：把云手机列表页改成设备墙（多设备同屏画面网格），并提供多控（一次操作扇出到多台设备）。

群控的实施以第九节为准。第三节里「用档案宽高换算坐标」和第六节 M2 与第九节冲突的地方，按第九节实现。

---

## 一、现状盘点

### 1.1 可以白拿的三件事

1. **Hub 天生是 1 对 N 广播**。`internal/httpapi/stream.go` 的 `browsers map[string]map[*lockedConn]struct{}` 允许一个 instance 挂多个浏览器连接共享同一路 agent 流。设备墙的缩略图在协议层不需要改后端。
2. **协议是透传 JSON**，Hub 不解析消息体，加指令类型只需在 `tools/phone/phone/stream.py` 的分发处扩展。
3. **`-simulate` 模式在 macOS 上就能跑假帧**（`internal/agentrt/runner.go` 造 SVG，1 fps）。设备墙和多控的 UI 原型不需要 Docker 就能验证。

### 1.2 五个卡点（按严重程度）

| # | 问题 | 位置 | 后果 |
| --- | --- | --- | --- |
| 1 | 控制走 `adb shell input tap`，每次新建子进程 | `tools/phone/phone/stream.py:16,60` | 单次 200–500ms；100 台串行 30s+，多控不可用 |
| 2 | 每实例起一个 `python3 -m phone stream` 子进程 | `internal/agentrt/runner.go:314-397` | 100 台 = 100 进程，无法统一调度和降帧 |
| 3 | 坐标硬编码 360×640 | `web/src/pages/InstanceDetail.tsx:72-73` | 多机型分辨率不同，扇出必然错位 |
| 4 | `<img src={data:...}>` 换 src + 3 秒全量轮询 | `InstanceDetail.tsx:38,98`、`Instances.tsx:30-32` | N 台同时换 src 会 GC 抖动和闪烁；轮询打死后端 |
| 5 | 无分组、无批量接口 | `internal/httpapi/router.go:205-210` | 群控没有落点，批量 task 直接 `ErrNotImplemented` |

### 1.3 两个必须修的实现缺陷（与协议选型无关）

- **控制被采集阻塞**：`stream.py:45-61` 在采集返回后才 `recv` 控制消息，而 `capture_screen` 的 timeout 是 20 秒（`stream.py:31`）。一次 screencap 卡住，tap 要等它回来。
- **队头阻塞**：帧和控制共用一条 TCP 字节流，一帧 500KB 的 PNG 会让后面的小控制包排队。

---

## 二、整体架构

```
┌─ 展现层 React ─────────────────────────────────────┐
│ DeviceWall   网格虚拟化 + 选中集 + 主控屏(Leader)    │
│ StreamBus    单例：引用计数订阅 / 退避重连 / 帧分发  │
└─────────────────── WS ─────────────────────────────┘
┌─ 中继层 Go ────────────────────────────────────────┐
│ Hub   按 channel 分连接 + 每连接背压 + 慢客降级丢帧 │
│ 批量接口 /groups、/instances:batch；鉴权与控制锁    │
└─────────────────── WS ─────────────────────────────┘
┌─ 采集注入层 节点侧 ────────────────────────────────┐
│ 单进程多路 asyncio，按订阅动态调帧率                 │
│ scrcpy video socket（H.264）│ scrcpy control socket │
└────────────────────────────────────────────────────┘
```

---

## 三、五个关键设计决策

### 决策 1：订阅制 + 分级帧率

墙上几十台设备不可能都是实时流。

- 格子滚入视口才订阅，滚出即 `unsubscribe`。
- 分档：墙模式 160px 宽 JPEG q40 约 1fps；悬停预览 360px 约 5fps；聚焦单台 720p 10–15fps。
- 一个 instance 的真实帧率 = 所有订阅者里的最高档，其余共享。

### 决策 2：帧与控制分通道

- `?channel=stream`：agent → 浏览器，单向，帧**可丢**（慢消费者直接丢帧，绝不阻塞）。
- `?channel=control`：浏览器 → agent，双向，小包、**绝不丢**、低延迟。

scrcpy 自己就是 video socket 与 control socket 分开的，原因相同。

Hub 从 `agents map[string]*lockedConn` 改为 `map[string]map[channel]*lockedConn`，改动很小。

### 决策 3：控制指令归一化 + 常驻注入通道

- 协议用 **0~1 比例坐标**，或带参考宽高的绝对像素。Hub 先还原成比例，再按**该设备当前画面宽高**重编码后写入。画面尺寸优先用设备刚上报的 `video-size`（旋转后会变），没有上报时用档案宽高。档案宽高只作缺省，不作为扇出坐标。
- 通道选型对比：

| 方案 | 单条指令延迟 | 改动量 | 代价 |
| --- | --- | --- | --- |
| A. 现状 `adb shell input` 子进程 | 200–500ms | 0 | 多控不可用 |
| B. adb 长连接复用（`adbutils`） | 50–150ms | 小（~50 行） | 仍是 shell 解析 |
| **C. scrcpy control socket（采用）** | 5–20ms | 中（~300–500 行二进制协议） | 需 ADR 0003 |
| D. 设备内跑 WebSocket server（装 APK） | 50–200ms | 大 | 要装并常驻 APK；注入只能走无障碍服务，比 C 慢 |

C 与 D 都是"常驻双向长连接"，区别只是 C 用裸 TCP、D 用 WebSocket。C 更快且零安装（`app_process` 启动，不装 APK，系统级注入权限）。

D 只在设备是真机、与 agent 不在同一二层网络、需要设备主动回连时才有意义。Redroid 容器的 adb 是本机 `127.0.0.1:18120-18219`，不需要。

### 决策 4：群控模型 = Leader + Followers

- 选中 N 台 → 指定一台为主控屏（Leader）放大显示，其余是缩略图。
- 在 Leader 上的操作 → 归一化指令 → 扇出到全部 follower。
- 并发策略：**不同设备并行，同一设备串行队列**，保证单机事件顺序；指令节流（如 100ms 内合并重复 tap）；前端乐观 UI。
- 可选：对每路帧算 aHash 做同步一致性校验，墙面显示"已同步 / 漂移"。
- **必须加控制权锁**：Hub 是广播结构，多人同操作一台设备会打架。谁先操作谁持有，带 TTL 自动释放。

### 决策 5：数据模型加两个

- `Group`（id / name / instance_ids / 默认 profile）：群控的靶子。
- `BatchTask`（id / group_id / type / payload，展开成每台一条 `InstanceTask`）：复用现有 `InstanceTask` 执行，批量接口只做展开。

---

## 四、与 ws-scrcpy 的关系：拆零件，不 fork

[NetrisTV/ws-scrcpy](https://github.com/NetrisTV/ws-scrcpy) 是 **MIT**，可以放心二次开发。它提供了四种解码器（MsePlayer / Broadway / TinyH264 / **WebCodecs**）、完整的 scrcpy 控制协议实现，以及一个内置 WebSocket server 的 scrcpy-server 修改版。

但它有三个假设与本项目冲突：单机 `adb` 直连、无鉴权无加密（README 明确警告 server 监听所有网卡且断连后仍在跑）、没有群控概念。

### 三条路线对比

| 路线 | 做法 | 优点 | 缺点 |
| --- | --- | --- | --- |
| A. 并列部署（每 host 跑一个 ws-scrcpy） | 前端 iframe / 直连它的 WS | 改动最小，立刻高清 | 两套鉴权、两套设备视图；**多控没法做**；端口暴露面大 |
| B. Fork 二开 | 把它改造成多设备管控平台 | 白送 shell/文件/DevTools | 设备编排要重写到 Node；与 Go 侧长期双份维护 |
| **C. 拆零件（采用）** | 只取 jar + WebCodecs Player，协议两端自己写 | 统一鉴权、统一设备模型、多控完全可控 | 要在 Go 里实现 scrcpy 协议（~500 行） |

### 拿什么

| 零件 | 说明 |
| --- | --- |
| `scrcpy-server.jar` | **用官方上游版本，不用它的 WS 修改版**。WS 修改版的意义是让浏览器直连设备，而我们要走 Go 中继，那层 WebSocket 是多余的一跳，且监听所有网卡是负担 |
| WebCodecs Player | 移植成 React 组件。MIT，干净。**只选它**，不要 wasm 那两个——避开 Broadway / tinyh264 / h264-converter 的额外许可与约 1MB 体积 |
| control 协议的字段定义 | 当作协议文档参考，多点触控 / 滚轮 / 键位编码照抄 |

### 不拿什么

Node.js 服务端、`adbkit` 设备发现、iOS / ws-qvh / Appium、wasm 解码器、xterm shell / DevTools / 文件列表（后期再说）。

---

## 五、库选型

### 前端（现有几乎零基础设施，全要新加）

| 用途 | 选择 | 理由 / 备选 |
| --- | --- | --- |
| 网格虚拟化 | **`virtua`** | 自带 `VGrid` 二维虚拟化，React 19 兼容，体积小。备选 `@tanstack/react-virtual`（只虚拟化行，列要自己算，但更可控） |
| 全局状态 | **`zustand`** | 选中集、订阅集、连接管理器需要跨组件共享且高频更新，context + useState 必炸 |
| 帧渲染 | **原生 `<canvas>` + `createImageBitmap`** | 不引库。`<img>` 换 src 会闪 + GC 抖动；`ImageBitmap` 在 worker 里解码后 `transferToImageBitmap`，主线程只 `drawImage` |
| 连接管理 | **自研 `StreamBus`（~150 行）+ `reconnecting-websocket`** | 需要引用计数订阅、退避重连、按 instance 复用连接，这是本项目核心逻辑，不适合外包 |
| 多选 / 框选 | **自研（pointer events，~80 行）** | 需求简单（点击 / ctrl+点击 / shift 范围）。要拖拽框选再上 `react-selecto` |
| 手势 | **自研** | swipe / longpress 在 canvas 上就是 pointerdown/move/up + 定时器。要更丰富再上 `@use-gesture/react` |
| H.264 解码 | **WebCodecs `VideoDecoder`**（移植自 ws-scrcpy） | 仅 Chromium 系；非 Chromium 降级到 JPEG 帧路径 |
| 数据获取 | **暂不引** | 实时状态走同一条 WS 的 `instance-state` 通道，替掉 `Instances.tsx` 的 3 秒全量轮询 |

**明确不引**：`react-grid-layout`（拖拽布局，不是虚拟化，墙是只读网格）、任何重量级 UI 框架（已有 Mantine 9）。

### Go 后端（基本不引新库）

| 用途 | 选择 |
| --- | --- |
| WebSocket | 保持 `gorilla/websocket`（已在用）。新写推荐 `coder/websocket`，但没必要动 |
| 并发扇出 | 标准库 + `golang.org/x/sync/errgroup` |
| 背压 / 慢消费者 | 每连接带缓冲 chan，满了**丢帧不阻塞**（宁可丢帧不能拖垮 agent） |
| 缩放转码（JPEG 回退路径） | `image/jpeg` + `image/png` + `golang.org/x/image/draw` |
| 限流 | `golang.org/x/time/rate` |
| WebRTC（暂不需要） | 如需再做，`pion/webrtc/v4` |

### Python 节点侧

| 用途 | 选择 |
| --- | --- |
| M1 提速过渡 | `adbutils`（openatx）复用 adb 连接，替代每命令 subprocess |
| 生命周期集成 | `tools/phone/phone/lifecycle.py` 增加启动 / 重启 scrcpy-server |
| 并发 | `asyncio` + `websockets`（已有） |

> 帧处理（缩放、转 JPEG）建议从 Python 移到 Go agent 的回退路径里，省掉 Pillow 依赖，也与 `-simulate` 造帧路径统一。

---

## 六、落地路线

### 阶段 0：验证（前置，不通过则后续全部作废）

1. 在 Ubuntu 24.04 + Redroid 14 上 `adb push` scrcpy-server.jar + `app_process` 启动。
2. `adb forward tcp:<port> localabstract:scrcpy`。
3. Go 侧连上 video socket，拿到 SPS/PPS 与第一个 IDR 帧。
4. 连上 control socket，注入一次 tap 并验证生效。

产出：`tools/phone` 新增 `scrcpy` 子命令 + `deploy/spike/` 下的验证脚本。

### M1：设备墙 + 基础多控（不改 ADR 0001 的画面决策）

1. 新增 `web/src/pages/DeviceWall.tsx` + `StreamBus` + zustand store + virtua 虚拟化网格。
2. 帧渲染改 canvas；Hub 加 `subscribe` / `unsubscribe`；`agentrt` 合并为单进程多路。
3. 协议扩展 swipe / key / text / longpress + 比例坐标；抽出 `input.py`。
4. 去掉详情页"每次点击新建 WS"（`InstanceDetail.tsx:75-79`），复用主连接。
5. 前端去掉 3 秒全量轮询，改走 WS 的 `instance-state` 通道。

### M2：群控

按第九节实施：同档案分组、Hub 重编码扇出、控制锁、批量启停。不把 H.264 或 scrcpy 控制套接字当作这一步的前置。

### M3：高清画质（即 ADR 0003）

8. 阶段 0 验证通过后，接入 scrcpy video + control，前端移植 WebCodecs Player，JPEG 路径保留为回退。

---

## 七、风险与待验证项

| 风险 | 说明 | 应对 |
| --- | --- | --- |
| jar 版本兼容 | ws-scrcpy 的 jar 是 scrcpy v1.19（2022），对 Android 14 / Redroid 14 兼容性未知 | 阶段 0 先验证；优先用官方最新版 |
| 容器重启 | scrcpy-server 必须在容器重启后重新拉起 | `lifecycle.py` 增加对应步骤 |
| WebCodecs 浏览器限制 | 仅 Chromium 系（Chrome / Edge） | JPEG 帧路径保留为回退，不降级成白屏 |
| ADR 冲突 | `docs/adr/0001-runtime-boundary.md:20` 明确"控制台不嵌入 scrcpy 视频协议" | 由 ADR 0003 修订，已起草 |
| 多操作者冲突 | Hub 广播结构，多人同操作一台设备 | 控制权锁（TTL 自动释放） |
| 扇出规模 | 100 台并行注入对 host CPU 的压力 | 阶段 0 压测；必要时分批 + 节流 |

---

## 八、待确认

1. **目标台数量级**：几十台（M1 的 JPEG 帧方案够用）还是上百台（必须上 M3 的 H.264 + 常驻控制通道）？
2. **M1 先做墙还是先做控制**：群控按第九节先落地。设备墙的虚拟化和分级帧率仍留在 M1，不挡住同档案扇出。

---

## 九、群控实施方案

交互式群控复制的是手势，不是视频，也不是某台设备的原始字节。一组设备必须是同一个 `DeviceProfile`。比例相同、方向相同，界面才落在同一位置；9:16 和 9:19.5 不编进一组。

### 数据面

- 每台设备自己的画面走原来的 `/api/v1/instances/:id/stream`。Hub 不把主控的画面复制给从设备。
- 浏览器慢时，该连接只留最新一帧，画面可以丢。丢帧不占用控制队列。
- 控制是另一条队列：同一台设备串行，不同设备并行。队列满了拒绝这一次，不打乱已经排队的事件。

### 控制面

浏览器发给 Hub 的是手势，不是某台屏幕上的像素：

| 字段 | 含义 |
| --- | --- |
| `xr` / `yr` | 0 到 1 的比例，点击、滑动起点 |
| `x2r` / `y2r` | 滑动终点 |
| `x` / `y` / `width` / `height` | 某路画面里的像素。Hub 先除以这对宽高得到比例，再按目标设备重编码 |
| `type` | `tap`、`swipe`、`pointer`、`longpress`、`key`、`text` |

Hub 对组内每台设备做一次编码，写入该设备的控制队列。尺寸用这台设备最近一次 `video-size`；节点还没上报时用档案宽高。方向变化后节点再报一次，下一笔手势就按新尺寸算。

节点侧采集和控制分成两个并发任务。控制任务按到达顺序执行 `input tap`、`input swipe`、`input motionevent`、`input keyevent`、`input text`。换成 scrcpy 控制套接字时只换这个编码器，浏览器和 Hub 的手势不变。

主控屏只决定操作者看着哪一台、手势从哪一块画面取比例。服务端把同一条手势写给组内每一台，包括主控自己。

### 控制锁

一台设备同一时刻只有一个操作者。锁在扇出前一次性占住组内全部成员，15 秒无新操作自动释放，同一操作者的新手势会续期。别人占着任意一台时，整次扇出都不执行。

### 批量任务

安装、启动、停止不走鼠标。`POST /api/v1/groups/:id/batch` 把 `start` 或 `stop` 展开成每台一条现有的 `InstanceTask`。展开前先检查每台都能做这个动作，有一台不行就整批不创建。

### 这一步不做什么

- 不在转发路径里解码画面，也不从视频包里认控件。
- 不把 ws-scrcpy 的页面当作群控台。
- 不同档案不进同一组，不做跨比例的布局对齐。
- 设备墙虚拟化、分级帧率和 H.264 仍按前面的阶段，不和这次扇出绑在一起。
