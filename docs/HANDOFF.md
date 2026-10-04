# 口袋键鼠（padlink）执行交接

手机（HarmonyOS API26）→ 局域网 UDP/TCP → Go 守护进程 → uinput：**触摸板 + 全键盘 + 语音听写**，三指滑动切换。v1 仅支持 Ubuntu 26.04 GNOME Wayland。

本仓库只含文档与协议规范（PRD / 协议与黄金向量 / 查证索引 / 技术方案），**全部代码从零实现**：daemon 于 M0、鸿蒙工程于 M1。

## 读什么（按序）

1. `docs/PRD.md` §0（铁律）→ 全文
2. `protocol/PROTOCOL.md` + `protocol/testvectors.json`（实现前必须先过向量对拍单测）
3. `docs/HARMONY-DOCIDS.md`（写任何鸿蒙 API 前查证）
4. 选读：`docs/RESEARCH-WAYLAND.md`（Wayland 集成事实与陷阱）、`docs/PROBE-LINUX.md`（M0 后真机验收程序）

## 环境

| 端 | 要求 |
| --- | --- |
| mac | devecocli ≥1.3.0 且 PROJECT_PATH 指向鸿蒙工程；API26 模拟器只用已有实例（勿新建）；Go ≥1.24 |
| Ubuntu 26.04 | `apt install wl-clipboard evtest`；鼠标指针加速设 flat；udev/systemd 安装物按 PRD §7.1 |

## 里程碑（通过标准即验收）

> **状态（2026-10-05）：M0–M5 代码全部落地；M0 注入/网络链路已获真实 Linux 内核验证**（OrbStack 容器，见下），**GNOME Wayland 桌面消费层与鸿蒙真机验收仍待执行**（⏳ 项）。离线验证门：daemon `go vet`/`go test -race`/双架构交叉编译；鸿蒙 `devecocli build` 全量构建 + deveco 静态检查零诊断 + 黄金向量 27 例与纯逻辑单测 150+ 例全绿。

### 已获真实 Linux 内核验证的部分（OrbStack 容器，2026-10-05）
- `padlinkd --test` 于 linux/arm64 容器（`--device /dev/uinput`）：**PASSED，退出码 0**（PROBE §4 自动化判据）；`/proc/bus/input/devices` 实证设备注册：`Name="PadLink Virtual Pointer"`、`Vendor=504c Product=0001`、`EV=7`（SYN|KEY|REL），与设计能力位一致。
- 宿主机 `padlinktoy` 对打容器内真实 padlinkd（跨真实 TCP/UDP 网络栈）：token 认证 ECHO 150/150 回包（P50 RTT 0.44ms）；`replay` 125/125 帧（版本协商 v1、TEXT 送达、0 ERR）；`fuzz` 1886 变异包后 daemon 存活（认证路径完好、恶意 TCP 被正确断开）。
- **仍待真桌面会话**（⏳）：光标可见画圆（GNOME Wayland + libinput 消费）、evtest/libinput 取证（PROBE §4 目视 + §5/§6）、uaccess ACL、systemd user service 拉起。

| 阶段 | 交付 | 通过标准 | 状态 |
| --- | --- | --- | --- |
| M0 | daemon 从零实现：协议层（对拍 testvectors）+ uinput 注入 + `--test` + udev/systemd 安装物 | `go test ./...` 全过 ✅；PROBE-LINUX.md 光标画圆通过 ⏳（内核层已验证 ✅，见上） | 代码完成（6713970/c562c09），桌面层验收待执行 |
| M1 | 鸿蒙工程 + 触摸板链路 | 光标跟手 ⏳；§11.1 手势达标 ⏳；真机多指 id 恒定 ⏳ | 代码完成（d80fcb7/4b678ff/f3587fc），手势引擎 38 例单测 ✅ |
| M2 | 发现 / 配对 / token / 重连 | 断连 30s 自动恢复 ⏳；错误确认码 5 次锁定 ✅（daemon 单测 + 容器内 token 认证实测 ✅） | 代码完成（74c288a） |
| M3 | 键盘区（HID usage 映射） | Ctrl+C/V、Super、Alt+Tab、中文经 fcitx 可用 ⏳ | 代码完成（5a4ecf1），粘滞序列单测 ✅ |
| M4 | 语音听写 | §11.4：50 字 2 次内上屏，剪贴板内容不变 ⏳ | 代码完成（21c967c），ASR 三陷阱参数显式落参 ✅ |
| M5 | 调优收口 | 延迟 P50≤40ms / P95≤80ms ⏳（传输层 RTT 分量已由 padlinktoy 实测，端到端待真机）；`devecocli build` 通过 ✅ | 收口完成（合成轨迹按钮/padlinktoy/文档） |

### 真机验收待办（代码已就绪，按序执行）
1. **Ubuntu 26.04 GNOME Wayland**：按 `docs/PROBE-LINUX.md` §0–§7 逐条执行（uaccess → systemd → `padlinkd --test` 目视光标画圆/敲键/滚动 → evtest/libinput 取证）。内核注入层已验证，本步聚焦桌面消费层与权限/服务形态。
2. **鸿蒙真机**：`devecocli run` 部署 → 发现/配对 → 触摸板手势 §11.1–11.3 → 语音 §11.4 → 延迟双口径（调试面板 + 120fps 录屏，§8）。
3. 联调分层纪律：`padlinkd --test`（无手机）→ `padlinktoy replay`（真机无手机注入流）→ 调试面板 echo/合成轨迹 → 真触摸。

## 工程纪律记录
- 提交 `e7f612c`（触摸板手势引擎）曾由执行 Agent 自行创建，违反"提交由总指挥执行"约定；经审查内容合格后，已由总指挥于 2026-10-05 cherry-pick 重放为 `4b678ff`（树完全一致，消息原样），此后全部提交均由总指挥执行。

## 关键坑 → 文档 ID

| 坑 | 结论 | 文档 ID |
| --- | --- | --- |
| 触摸 | 消费 `touches` + `getHistoricalPoints()`，勿用 `changedTouches`（vsync 帧合并丢位移） | `ts-universal-events-touch`、`faqs-arkui-907` |
| 语音 | `recognitionMode=0`、`maxAudioDuration=60000`、`vadEnd` 调大——三参数漏一即坏 | `hms-ai-speechrecognizer`、`faqs-core-speech-12` |
| UDP 广播 | bind `0.0.0.0` → `setExtraOptions({broadcast:true})` → `bindSocket(WiFi NetHandle)` | `faqs-network-96` |
| 避让 | 底部手势条用 `TYPE_NAVIGATION_INDICATOR`（GESTURE 枚举恒空）；侧边留边真机校准 | `arkts-apis-window-e`、`faqs-arkui-824` |
| 麦克风 | 权限声明必须带 `reason`+`usedScene`；拒绝走 onError 1002200012 | `speechrecognizer-guide`、`errorcode-corespeech` |

完整 ID 索引：`docs/HARMONY-DOCIDS.md`（`devecocli docs read <完整路径>`；短 ID 搜不到，search 用内容关键词）。

## 红线

① 鸿蒙 API 先查后写；② 协议改动先向量后代码；③ 不做 X11 / 内网穿透；④〔溯源〕约束不得擅改。全文见 PRD §0.2。
