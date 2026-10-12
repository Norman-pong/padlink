# 口袋键鼠（padlink）执行交接

> 时点快照（2026-10-12），里程碑状态以 git log / lore 为准。

手机（HarmonyOS API26）→ 局域网 UDP/TCP → Go 守护进程 → uinput：**触摸板 + 全键盘 + 语音听写**，单页融合控制面（功能区切换归浮动按钮）。支持 Ubuntu 26.04 GNOME Wayland 与 macOS。

本仓库为 Monorepo，daemon（`daemon/`）与鸿蒙工程（`apps/harmony/`）均已落地；macOS 侧方案与实测见 `docs/PLAN-MACOS.md`。**鸿蒙真机与 GNOME 桌面消费层验收待执行**（验收程序见 `docs/PROBE-LINUX.md`）。

## 读什么（按序）

1. `docs/PRD.md` §0（铁律）→ 全文
2. `protocol/PROTOCOL.md` + `protocol/testvectors.json`（实现前必须先过向量对拍单测）
3. `docs/HARMONY-DOCIDS.md`（写任何鸿蒙 API 前查证）
4. 选读：`docs/RESEARCH-WAYLAND.md`（Wayland 集成事实与陷阱）、`docs/PROBE-LINUX.md`（Linux 真机验收程序）、`docs/PLAN-MACOS.md`（macOS 方案与实测）

## 环境

| 端 | 要求 |
| --- | --- |
| mac | devecocli ≥1.3.0 且 PROJECT_PATH 指向鸿蒙工程；API26 模拟器只用已有实例（勿新建）；Go ≥1.26 |
| Ubuntu 26.04 | `apt install wl-clipboard evtest`；鼠标指针加速设 flat；udev/systemd 安装物按 PRD §7.1 |

## 里程碑（通过标准即验收）

> 离线验证门：daemon `go vet`/`go test -race`/双架构交叉编译；鸿蒙 `devecocli build` 全量构建 + deveco 静态检查零诊断 + 黄金向量与纯逻辑单测全绿。**GNOME 桌面消费层与鸿蒙真机待验收**（⏳ 项）。

### 已验证与待验收的边界

- **内核注入层**：`padlinkd --test` 在 linux/arm64 容器（`--device /dev/uinput`）PASSED（退出码 0，PROBE §4 自动化判据）；`/proc/bus/input/devices` 实证设备注册 —— `Name="PadLink Virtual Pointer"`、`Vendor=504c Product=0001`、`EV=7`（SYN|KEY|REL），与设计能力位一致。
- **协议与网络栈**：`padlinktoy` 对打容器内真实 padlinkd（跨真实 TCP/UDP）：token 认证 ECHO 150/150 回包（P50 RTT 0.44ms）；`replay` 125/125 帧（版本协商 v1、TEXT 送达、0 ERR）；`fuzz` 1886 变异包后 daemon 存活（认证路径完好、恶意 TCP 被正确断开）。
- **仍待真桌面会话**（⏳）：光标可见画圆（GNOME Wayland + libinput 消费）、evtest/libinput 取证（PROBE §4 目视 + §5/§6）、uaccess ACL、systemd user service 拉起。
- **鸿蒙端全软件链路**：模拟器主控页触摸面 → 手势引擎 → UDP MOVE → 宿主网络 → 容器内 padlinkd → uinput → **内核 EV_REL 事件流**（与设计轨迹的弦长数学吻合）；Draw circle 每轮 daemon MOVE 恰 +72、应用侧 UDP sends 与 daemon 端计数一致；token 认证上线、1Hz ECHO 心跳、断开重连回环通过。
- **模拟器验证暴露的平台约束**（详见 `docs/HARMONY-DOCIDS.md`）：设置页必须有可达入口；覆盖系统栏的悬浮元素必须避让 `TYPE_SYSTEM`；`net.connection` 全系接口必须在 `module.json5` 声明 `GET_NETWORK_INFO`；`@Builder` 传状态变量必须单参对象按引用；daemon UDP 与 TCP 的逐包 verbose 日志须对称。
- **仍待真机**（⏳，模拟器无法替代）：真实多指触摸（多指 id 恒定、双指滚动/长按拖拽手感）、§11 盲测、延迟口径 P50≤40ms/P95≤80ms（§8 双口径）、语音端侧 ASR 真实识别、键盘中文上屏。

| 阶段 | 交付 | 通过标准 | 状态 |
| --- | --- | --- | --- |
| M0 | daemon：协议层（对拍 testvectors）+ uinput 注入 + `--test` + udev/systemd 安装物 | `go test ./...` 全过；PROBE-LINUX.md 光标画圆通过（内核层已验证，桌面层待执行） | 代码完成 |
| M1 | 鸿蒙工程 + 触摸板链路 | 光标跟手；§11.1 手势达标；真机多指 id 恒定（链路已获模拟器级端到端验证） | 代码完成，手势引擎单测 ✅ |
| M2 | 发现 / 配对 / token / 重连 | 断连 30s 自动恢复；错误确认码 5 次锁定（daemon 单测 + 容器内 token 认证 + 模拟器认证/心跳/重连回环均已验证） | 代码完成 |
| M3 | 键盘区（HID usage 映射） | Ctrl+C/V、Super、Alt+Tab、中文上屏可用 | 代码完成，粘滞序列单测 ✅ |
| M4 | 语音听写 | §11.4：50 字 2 次内上屏，剪贴板内容不变 | 代码完成，ASR 三陷阱参数显式落参 ✅ |
| M5 | 调优收口 | 延迟 P50≤40ms / P95≤80ms（传输层 RTT 分量已由 padlinktoy 实测，端到端待真机）；`devecocli build` 通过 | 收口完成 |

### 真机验收待办（代码已就绪，按序执行）
1. **Ubuntu 26.04 GNOME Wayland**：按 `docs/PROBE-LINUX.md` §0–§7 逐条执行（uaccess → systemd → `padlinkd --test` 目视光标画圆/敲键/滚动 → evtest/libinput 取证），聚焦桌面消费层与权限/服务形态。
2. **鸿蒙真机**：`devecocli run` 部署 → 发现/配对 → 触摸板手势 §11.1–11.3 → 语音 §11.4 → 延迟双口径（调试面板 + 120fps 录屏，§8）。
3. 联调分层纪律：`padlinkd --test`（无手机）→ `padlinktoy replay`（真机无手机注入流）→ 调试面板 echo/合成轨迹 → 真触摸。

## 关键坑 → 文档 ID

| 坑 | 结论 | 文档 ID |
| --- | --- | --- |
| 触摸 | 消费 `touches` + `getHistoricalPoints()`，勿用 `changedTouches`（vsync 帧合并丢位移） | `ts-universal-events-touch`、`faqs-arkui-907` |
| 语音 | `recognitionMode=0`、`maxAudioDuration=60000`、`vadEnd` 调大——三参数漏一即坏 | `hms-ai-speechrecognizer`、`faqs-core-speech-12` |
| UDP 广播 | bind `0.0.0.0` → `setExtraOptions({broadcast:true})` → `bindSocket(WiFi NetHandle)` | `faqs-network-96` |
| 避让 | 底部手势条用 `TYPE_NAVIGATION_INDICATOR`（GESTURE 枚举恒空）；侧边留边真机校准 | `arkts-apis-window-e`、`faqs-arkui-824` |
| 麦克风 | 权限声明必须带 `reason`+`usedScene`；拒绝走 onError 1002200012 | `speechrecognizer-guide`、`errorcode-corespeech` |
| 切换主机被拉回 | `TcpChannel.connect()` 每次必须换新 socket 实例：上一次 `close()` 异步落地会在 connect 之后关掉刚建立的连接，会话建立即被判「TCP 断开」→ 重连循环拉回旧主机。另：TCP `close`/`error` 回调按会话代次挂载，不能只在构造时挂一次（旧会话断开会被误报给新会话） | 本项目真机实测 |
| 列表不刷新 | NavDestination 页从子页返回不会重建：主机列表必须在 `onShown` 重新加载，只靠 `aboutToAppear` 会一直显示旧快照（表现为配对成功却看不到新主机） | `faqs-arkui-403`、`faqs-arkui-1110` |

完整 ID 索引：`docs/HARMONY-DOCIDS.md`（`devecocli docs read <完整路径>`；短 ID 搜不到，search 用内容关键词）。

## 红线

① 鸿蒙 API 先查后写；② 协议改动先向量后代码；③ 不做 X11 / 内网穿透；④〔溯源〕约束不得擅改。全文见 PRD §0.2。
