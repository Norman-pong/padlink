# PRD：口袋键鼠（代号 padlink）— 鸿蒙手机作为电脑的触摸板 / 键盘 / 语音输入设备

> 状态：技术方案已验证，待开发
> 仓库形态：**Monorepo**（鸿蒙 APP + Linux 守护进程同仓开发，协议规范为单源）；本仓库当前仅含文档与协议规范，代码全部从零实现
> 鸿蒙 API 查证索引：`docs/HARMONY-DOCIDS.md`；Linux 集成事实：`docs/RESEARCH-WAYLAND.md`
> 全文标注〔溯源：…〕处为硬约束，执行时不得凭记忆改写

---

## 0. 执行约束与溯源机制（执行 agent / 开发者必读，先读本章再动工）

### 0.1 单一事实源与裁决顺序

| 主题 | 事实源 | 冲突时裁决 |
| --- | --- | --- |
| 需求与范围 | 本 PRD | 以 PRD 为准 |
| 协议格式 | `protocol/PROTOCOL.md` + `protocol/testvectors.json` | 以**黄金向量字节**为准 |
| 鸿蒙 API 用法/参数 | devecocli 本地官方文档（`devecocli docs read <完整文档ID>`） | 以**现场读取**为准，模型记忆仅作线索 |
| Linux/Wayland 集成事实 | `docs/RESEARCH-WAYLAND.md`（含上游 URL） | 以上游文档为准 |
| 运行时行为 | 真机实测（验证程序见 `docs/PROBE-LINUX.md`） | 以实测输出为准 |

### 0.2 执行铁律（防跑偏红线）

1. 正文〔溯源：…〕标注的约束**不得擅改**；要推翻必须：给出新证据（文档 ID 或实测输出）→ 更新对应溯源条目 → 在项目决策记录（lore）留痕，三步缺一不可。
2. 协议任何改动：先改 `protocol/` 并重生成 `testvectors.json`（双端字节级对拍通过）→ 再动两端实现；跳步即回退重做。
3. 鸿蒙端写任何 API 调用前，参数与记忆不符时必须 `devecocli docs read` 现查——语音识别存在三处默认值陷阱（§4.5），凭记忆写参数 = 制造静默缺陷。
4. 范围硬边界（§1.3/§1.4）：**不做 X11 适配、不做公网/内网穿透**；执行中遇到相关"顺手优化"一律拒绝并记录。
5. 每个 FR 开工前先查 `docs/HARMONY-DOCIDS.md` 对应条目；收口时提交说明须回链所用文档 ID。
6. 本 PRD 与项目 AGENTS.md/lore 冲突时，按"项目规范 > 本 PRD 细节、本 PRD 范围 > 项目规范"处理，并留痕。

## 1. 产品概述

### 1.1 一句话定位

把 HarmonyOS 手机变成电脑的**低延迟无线外设**：触摸板 + 全键盘 + 语音听写，三者通过三指滑动在手机上无缝切换（v1 目标平台：Ubuntu 26.04；平台扩展战略见 §1.3 与附录 B）。

### 1.2 目标用户与场景

| 场景 | 描述 |
| --- | --- |
| 演示 / 客厅操控 | 笔记本接电视或投影，人不在电脑前，用手机当触摸板翻页、点选 |
| 临时外设替代 | 鼠标没电 / 没带，快速应急操控 |
| 沙发听写输入 | 手机按住说话，文字直接上屏到电脑文档（中文语音端侧识别，无网可用） |

### 1.3 平台矩阵

| 端 | 目标 |
| --- | --- |
| 手机 | HarmonyOS NEXT，targetAPIVersion ≥ 26（沉浸材质体系要求），真机为主（触摸真实度） |
| 电脑 | Ubuntu 26.04 LTS（Resolute Raccoon，GNOME 50，kernel 7.0；桌面会话仅 Wayland 后端），**仅 GNOME Wayland**（受支持且受测的唯一环境）；**明确不做 X11 适配**——daemon 启动时校验 WAYLAND_DISPLAY，缺失打印不支持警告〔溯源：RESEARCH-WAYLAND.md §5〕 |
| 其他 Linux 发行版 | **可用不承诺**：uinput 为内核级能力，指针/键盘注入天然跨发行版与桌面；但语音文本注入依赖 wl-clipboard（Wayland 合成器），X11 会话下不可用。社区自用可以，不进 v1 测试与验收 |
| macOS（v2 规划） | macOS 13+：CGEvent 注入后端（需辅助功能权限授权引导），文本注入 pbcopy + Cmd+V（比 Wayland 更简单，无 wl-clipboard 类陷阱）；估算 1~1.5 周，详见附录 B |

### 1.4 v1 范围外（Non-goals）

- **空中鼠标模式**：v1 不做（协议与页面框架预留事件源，见附录 B）
- **公网/内网穿透、中继：明确不做**——产品仅在同一局域网内使用，不集成、不测试任何穿透方案；远程场景由用户自行组网（如自担风险使用 VPN），与产品无关
- 多手机同时控制一台主机（协议允许多客户端 token，UI 不做多端管理）
- Android / iOS 端
- 媒体遥控器皮肤、PPT 翻页专用模式
- 其他 Linux 发行版/合成器的适配与测试（架构天然兼容，但"能用"≠"支持"，不进 v1 验收）
- macOS 支持（v2 规划，见附录 B，不属 v1 范围）

---

## 2. 系统架构

### 2.1 链路总览

```
┌─ 鸿蒙 APP ──────────────┐         ┌─ Ubuntu 26.04 ──────────────┐
│ 主控页（三功能区切换）      │  局域网  │ padlinkd（用户级守护进程）      │
│  ├ 触摸板：触摸事件→手势判定 │ ──────▶ │  ├ 发现应答 / 配对 / 鉴权       │
│  ├ 键盘：  键码→组合键序列    │ UDP+TCP │  ├ uinput 注入（鼠标+键盘）     │
│  └ 语音：  端侧ASR→文本     │         │  └ 文本注入（剪贴板+Ctrl+V）    │
└─────────────────────────┘         └─────────────┬───────────────┘
                                                  ▼
                                          /dev/uinput（内核级虚拟输入设备）
                                                  ▼
                                     GNOME Wayland（libinput 直接消费）
```

要点：
- **uinput 是系统输入注入的正解**：内核级虚拟设备，Wayland 直接消费，绕开 xdotool 类工具在 Wayland 下失效的问题。
- **双通道传输**：高频可丢的指针移动走 UDP（丢包由新值自然覆盖）；不可丢的按键/文本/配对/心跳走 TCP。
- 手机端触摸板使用时必然前台 + 亮屏，v1 **不需要**长时任务与 RunningLock（v2 空中鼠标息屏场景再引入）。

### 2.2 Monorepo 目录结构（目标结构，代码从零创建）

```
padlink/
├── docs/                    # PRD、执行交接（HANDOFF）、查证索引、技术方案
├── protocol/                # 通信协议规范（单源，两端共同遵守）
│   ├── PROTOCOL.md          # 报文格式、事件类型、认证
│   └── testvectors.json     # 黄金测试向量（两端对拍单源）
├── apps/
│   └── harmony/             # 鸿蒙 APP（devecocli create --api-level 26 生成）
│       └── …                # ArkTS/ArkUI，见 §6
├── daemon/                  # Linux 守护进程（Go，无 cgo，单二进制）
│   ├── cmd/padlinkd/        # 守护进程入口
│   ├── cmd/padlinkctl/      # CLI：status / pair / clients / unpair
│   ├── internal/…           # discovery / pairing / uinput / inject / clip
│   ├── packaging/systemd/   # user 级 service（打包/手工安装两变体）
│   └── packaging/udev/      # uinput uaccess 规则
├── tools/                   # 联调工具：录回放器、协议 fuzz、延迟压测
└── README.md
```

约束：
- `protocol/` 是两端唯一事实源，改协议必须先改这里并重生成向量，再同步两端实现。
- daemon CI：`go vet` + 单测；鸿蒙端 deveco-mcp check；提交门见 §10。

---

## 3. 功能需求总览（FR 清单）

| 编号 | 功能 | 优先级 | 里程碑 |
| --- | --- | --- | --- |
| FR-0 | 设备发现与配对（自动发现 + 4 位确认码 + token） | P0 | M2 |
| FR-1 | 触摸板主控（全屏手势式） | P0 | M1 |
| FR-2 | 三指滑动切换功能区 | P0 | M1 |
| FR-3 | 全键盘输入区（自绘 QWERTY + 粘滞修饰键） | P1 | M3 |
| FR-4 | 语音听写区（端侧 ASR → 文本注入） | P1 | M4 |
| FR-5 | 设置页（灵敏度 / 滚动方向 / 主机管理） | P0 | M1 |
| FR-6 | 连接管理（状态机 / 自动重连 / 心跳） | P0 | M2 |
| FR-7 | 调试面板（隐藏入口，联调与验收用） | P0 | M1 |

---

## 4. 功能需求详述

### 4.1 FR-0 设备发现与配对

1. **发现**：APP 进入发现页后向局域网发 UDP 广播（默认端口 53021）：bind `0.0.0.0` → `setExtraOptions({broadcast:true})`（**必须在 bind 成功之后**）→ `connection.getAllNets()` 筛 BEARER_WIFI 得 NetHandle 后 `netHandle.bindSocket(udpSocket)` 锁定 WiFi 出口（参考官方 FAQ faqs-network-96）；
2. **配对**：用户选中主机 → daemon 侧生成 4 位数字确认码，60 秒有效、最多试 5 次；展示方式：`notify-send` 桌面通知 + `padlinkctl pair` 终端输出（无桌面环境时）。APP 输入码正确 → daemon 生成 32 字节 token 双端持久化（手机端安全存储，daemon 端 0600 文件）。
3. **重连**：已配对主机打开 APP 直连（token 认证）；token 失效走重新配对。

### 4.2 FR-1 触摸板主控（全屏手势式）

整屏触摸面（避让系统手势区，见 §6.3），手势语义：

| 手势 | 效果 | 判定规则（默认值，均可配） |
| --- | --- | --- |
| 单指滑动 | 光标相对移动 | 位移差 × 灵敏度 × 加速曲线 |
| 单指轻点 | 左键点击 | 按下 ≤150ms 且总位移 ≤8vp |
| 双指轻点 | 右键点击 | 两指按下时间差 ≤40ms，其余同上 |
| 双指滑动 | 滚动（默认**自然滚动**） | 垂直分量累积映射 REL_WHEEL_HI_RES（平滑滚动） |
| 长按后拖动 | 按住左键拖拽 | 按下 ≥300ms 且位移 ≤8vp → BTN_LEFT down，抬指 up |
| 三指左右滑 | 切换功能区 | ≥3 指同向水平位移 ≥60vp，500ms 节流防抖（见 FR-2） |

- **加速曲线**：慢速 1:1 精确、快速最高 2.5x 增益（分段线性，拐点可调）；灵敏度默认 1.2，设置范围 0.5–2.0。
- 手势判定状态机要求：任何分支必须有 CANCEL/多指插入的复位路径，禁止出现"多指抬起后光标漂移"。
- **绝对不做**屏幕上固定左右键按钮区（已确认交互形态）。
- **触摸数据消费规则**：Move 事件按 vsync 帧合并下发（一帧内多个报点只下发最新一个），手势判定器必须消费 `touches`（器件报点率）而非 `changedTouches`（屏幕刷新率重采样值），并用 `getHistoricalPoints()` 取回帧内被合并报点——否则快速滑动丢位移、双指滚动起始段抖动；上表判定阈值需按"帧末采样"语义校准〔溯源：faqs-arkui-907、ts-universal-events-touch〕。
- **指针加速基线**：GNOME 对相对设备默认 adaptive 加速（0.3–3.5x）且无 per-device 配置；APP 灵敏度曲线要可控，要求主机鼠标设 flat profile 作为标定基线，daemon 启动时检测并提示〔溯源：RESEARCH-WAYLAND.md §4——不存在环境变量式 per-device 加速配置，gsettings 全局为唯一入口〕。

### 4.3 FR-2 功能区切换

- 顺序循环：**触摸板 ⇄ 键盘 ⇄ 语音**，三指横滑切换。
- 切换转场用 HDS 物理动效（弹簧曲线），页面内容无闪烁；切换时若存在未释放按键（如按住的左键）必须先发 release。
- 设计约束：三指手势**只允许水平方向**（系统三指下滑为截屏手势，应用不可拦截；水平方向无系统占用）。

### 4.4 FR-3 键盘输入区（混合形态，v2 修订）

- **系统输入法承接文本**（TermNext 式取舍）：输入区为常驻 TextArea，用户以手机自带 IME 打字（拼音/任意已装输入法，中文不再依赖电脑侧 fcitx）。APP 经 onChange 的 previewText 区分 IME 组合态（拼音预上屏）与提交：组合态不上行，仅提交增量镜像到电脑。
- **提交流分派**：可映射 ASCII 逐字符走 HID 键码（保留按键语义、不踩电脑剪贴板）；不可映射字符（CJK/emoji）合并成 TEXT run 走剪贴板注入（同 §4.5，1400B 分块）；输入框内删除镜像为 KEY Backspace（粘滞 Ctrl 叠加 = 删词）；**单字符提交 × 粘滞修饰 = 组合键**（Ctrl+C 等），多字符提交视为短语——清除粘滞直发。
- **自绘键条补键语义**（系统输入法给不出的部分）：Esc / Tab / ⌫ / Enter / ←↑↓→ 直发 HID；修饰键 Shift / Ctrl / Alt / Super 做**粘滞态**（点亮保持），任一非修饰键触发后自动复位。按键按下即发 key-down、抬指发 key-up；长按连发（key repeat）在 daemon 侧处理。
- **区域生命周期**：进入键盘区自动唤起输入法并切 RESIZE 避让；离开收起输入法、补发一切未抬起的 up（§11.2 残留键红线）。
- 历史：v1 为整幅自绘 QWERTY（不经系统输入法、中文依赖电脑侧 fcitx）；v2 改混合形态（中文不依赖电脑配置、IME 手感与联想可用），整幅键盘移除（git 史可回溯）。

### 4.5 FR-4 语音听写区

- **端侧识别**：Core Speech Kit `speechRecognizer`（中文 zh-CN 为当前唯一语种，含中文语境英文；离线端侧执行；公开 API `hms-ai-speechrecognizer`，API 11+）。**参数必须显式全量传递**（三处默认值陷阱）：`recognitionMode: 0`（实时录音识别，默认 1 是写音频流模式，传错功能直接不可用）、`maxAudioDuration: 60000`（默认仅 20000）、`vadEnd` 按需调大（默认 800ms 静音即判停）；"按住说话"松手时主动 `finish()`。中间/最终结果靠同一 onResult 回调的 `isFinal` 字段区分（蹦字模式默认开启）〔溯源：hms-ai-speechrecognizer（CreateEngineParams/StartParams/SpeechRecognitionResult 表）、speechrecognizer-guide 步骤10、faqs-core-speech-12〕。
- **文本注入（daemon 侧）**：中文无法逐字符打 HID 键码，采用**剪贴板 + Ctrl+V**（GNOME 下无更优替代：ydotool type 硬编码 ASCII 表、wtype 依赖 mutter 未实现的协议）：`wl-paste --no-newline` 备份（**必须 --no-newline，否则恢复后多出换行**）→ `wl-copy --type 'text/plain;charset=utf-8'` 写入 → 注入 Ctrl+V → 300–500ms（可配）后用 `wl-copy --type <原MIME>` 恢复；所有 wl-* 子进程加 2–3s 超时降级。恢复时序竞态为已知接受边界：慢应用可能粘贴到旧内容；验收口径为剪贴板**内容**不变（owner 可能由 mutter 内置剪贴板管理器接管，属正常行为）〔溯源：RESEARCH-WAYLAND.md §3/§6〕。
- daemon 启动时检测 `wl-clipboard`：缺失则该功能降级，APP 端收到明确错误提示（附安装命令 `sudo apt install wl-clipboard`）。
- 权限：`ohos.permission.MICROPHONE`；首次拒绝后的引导文案进设置页。

### 4.6 FR-5 设置页

灵敏度（0.5–2.0）、滚动方向开关（默认自然滚动）、已配对主机管理（重命名 / 删除 / 切换默认）、调试面板开关、关于（版本、协议版本）。

### 4.7 FR-6 连接管理

- 状态机：`发现中 → 配对中 → 已连接 → 重连中(指数退避 1s/2s/4s…上限 15s) → 失联`。
- 心跳：TCP 通道 1Hz ECHO，RTT 展示于状态浮层与调试面板；3 次超时判失联。
- 状态浮层：连接状态用 ImmersiveMaterial THIN 档悬浮胶囊常驻主控页角落（HDS 归层：层 1 材质表达层级）。

### 4.8 FR-7 调试面板（隐藏入口：版本号连点 5 次）

实时显示：触摸事件原始流与采样率、发送速率、RTT、丢包率、当前手势状态机状态；内置 echo 自测、`--test` 合成轨迹触发按钮。此面板是延迟验收（§11）的测量工具。

---

## 5. 通信协议规格（摘要，全文见 `protocol/PROTOCOL.md`）

### 5.1 通道与端口

| 通道 | 传输 | 用途 | 理由 |
| --- | --- | --- | --- |
| 53021/UDP（广播） | 不可靠 | 发现、配对握手 | 广播可达性 |
| 53021/UDP（单播） | 不可靠 | MOVE / SCROLL（高频，允许丢，新值覆盖旧值） | 低延迟 |
| 53021/TCP | 可靠 | KEY / BUTTON / TEXT / 配对确认 / ECHO 心跳 / 控制 | 不可丢事件 |

### 5.2 报文头（固定 19 字节）+ payload

```
magic(2B 'PL') | ver(1B) | type(1B) | flags(1B) | seq(2B) | payload_len(2B) |
reserved(2B) | hmac_trunc(8B, 配对后启用) | payload(≤1400B, UDP 分片上限内)
```

- 事件类型枚举：`HELLO / DISCOVER_RESP / PAIR_REQ / PAIR_OK / PAIR_NAK / MOVE(dx,dy) / SCROLL(dy_hi_res) / BUTTON(btn,down) / KEY(code,down) / TEXT(utf8) / ECHO / BYE / ERR`
- 鉴权：每包 HMAC-SHA256(token) 截断 64bit；未认证包一律丢弃并计数（防局域网伪造/重放）。
- 版本协商：HELLO 交换 ver，不一致取双端较低值并告警。
- **黄金测试向量**：`protocol/testvectors.json` 是两端编解码一致性的对拍单源；改头结构必须同步重生成向量并升 ver。

---

## 6. 鸿蒙端技术规格（apps/harmony）

### 6.1 工程与架构底线（hmos-hds-design 技能规定）

- 脚手架：`devecocli create --app-name padlink --api-level 26`。
- 状态管理**一律 ArkUI V2**（@ComponentV2/@Local/@Param/@Monitor）；导航一律 `Navigation + NavPathStack`。
- 页面结构：`发现/配对页 → 主控页（三功能区容器 + 切换转场）→ 设置页`；高频触摸处理逻辑与 UI 分离（ViewModel + 纯函数手势判定器，可单测）。
- 网络：`@ohos.net.socket`（UDPSocket + TCPSocket），发送循环独立于 UI 线程。

### 6.2 权限与系统能力

| 能力 | API / 权限 |
| --- | --- |
| 局域网 UDP/TCP | `@ohos.net.socket`，`ohos.permission.INTERNET` |
| 麦克风（语音区） | `ohos.permission.MICROPHONE`（module.json5 声明必须带 `reason` + `usedScene{abilities:['EntryAbility'], when:'inuse'}`；运行时 `requestPermissionsFromUser` 弹窗；被拒不抛异常——ASR onError 返回 1002200012，需 UI 分支处理） |
| 屏幕常亮 | `window.setWindowKeepScreenOn` |
| 多点触控 | 触摸事件 TouchObject.id（手指唯一标识，支持多指跟踪） |

### 6.3 HDS 设计规范要点（本 APP UI 很薄，合规即止）

- 全屏触摸面避让：底部手势条用 `getWindowAvoidArea(TYPE_NAVIGATION_INDICATOR)` 取动态值（返回单位 px，需转 vp）；**侧边手势区无 API 可查**——`TYPE_SYSTEM_GESTURE` 枚举存在但官方注明所有设备返回空，左右边缘按经验值留边距 + 真机校准；`windowSizeChange/avoidAreaChange` 双监听（对应 HDS 硬约束 12）〔溯源：arkts-apis-window-e（AvoidAreaType 枚举表）、faqs-arkui-824、faqs-arkui-1114〕。
- 颜色/字号/间距一律 `sys.color.*` / `ohos_id_*` token，禁硬编码；文案进 string.json。
- 键盘按键与触控行为自绘组件：按压态 onTouch 自管（DOWN 置位 / UP、CANCEL 复位）；必须配无障碍属性。
- 状态浮层材质 ADAPTIVE 档；不堆视效，工具型定位（归层：结构性改动落层 5）。

---

## 7. Linux 守护进程规格（daemon/）

### 7.1 运行形态

- 语言：**Go（无 cgo 单二进制，交叉编译 amd64/arm64）**。
- **注入层平台化**：注入实现为可替换后端 `DeviceWriter` 接口——Linux 用 uinput（无 cgo）；v2 macOS 用 CGEvent（cgo 或 Swift 辅助进程 + 辅助功能权限引导）；协议与会话层跨平台复用，换平台只换注入后端，手机端零改动。
- systemd **user service**（无 root）：unit 挂 `PartOf=graphical-session.target` + `WantedBy=graphical-session.target`（default.target 下 uaccess ACL 与 WAYLAND_DISPLAY 均未就绪）；udev 规则 `SUBSYSTEM=="misc", KERNEL=="uinput", TAG+="uaccess", OPTIONS+="static_node=uinput"`（uinput 默认 root:root 0600 且上游无默认授权）；`Restart=on-failure`；启动时校验 WAYLAND_DISPLAY，缺失即明确报错〔溯源：RESEARCH-WAYLAND.md §2/§3〕。
- 日志进 journald；`-v` 前台 verbose 模式（开发期主力调试形态）。

### 7.2 模块与职责

| 模块 | 职责 |
| --- | --- |
| discovery | 应答 UDP 广播，暴露主机信息 |
| pairing | 4 位确认码生命周期（60s/5 次）、token 签发与校验、多客户端上限 4 |
| uinput | 创建虚拟设备：REL_X/Y、**REL_WHEEL_HI_RES（注册即必发——mutter 仅消费 HI_RES 路径，只注册不发会被 libinput 告警回退）**、BTN_LEFT/RIGHT/MIDDLE、全键盘 KEY_*；legacy REL_WHEEL 累积补发仅作非 GNOME 环境防御〔溯源：RESEARCH-WAYLAND.md §1〕 |
| inject | MOVE/SCROLL/BUTTON/KEY → uinput 事件序列（含修饰键组合展开、key repeat） |
| text | TEXT → 备份剪贴板 → wl-copy → 注入 Ctrl+V → 恢复（wl-clipboard 缺失时优雅降级） |
| session | TCP 会话、心跳、鉴权、速率限制 |

### 7.3 CLI 与自测

- `padlinkctl status|pair|clients|unpair`。
- `padlinkd --test`：注入"光标画圆 + 逐键敲击"合成序列，用于**脱离手机独立验证注入链路**（调试分层的第一层）。
- 协议解析单元测试覆盖：乱序、截断、错 magic、HMAC 失败、重放；实现须先通过 `protocol/testvectors.json` 对拍。

---

## 8. 非功能需求（NFR）

| 项 | 指标 |
| --- | --- |
| 端到端延迟（手指触 → 光标动） | P50 ≤ 40ms，P95 ≤ 80ms（局域网，echo RTT + 120fps 录屏逐帧双口径测量） |
| 丢包容忍 | MOVE 通道 5% 丢包不感知 |
| 可靠性 | daemon 崩溃 systemd 自动拉起；网络抖动 30s 内自动重连 |
| 安全 | token 逐包 HMAC；确认码防误连/爆破（5 次锁）；剪贴板备份内容不落盘、不日志 |
| 功耗 | 连续触控使用 30 分钟无明显发热 |

---

## 9. 里程碑（单人全职估算；MVP = M0–M2）

| 里程碑 | 内容 | 工期 |
| --- | --- | --- |
| M0 | monorepo 骨架 + daemon（协议层对拍 testvectors、uinput 注入、`--test` 自测、udev/systemd 安装物） | 3 天 |
| M1 | 鸿蒙工程 + 触摸板链路全通（手势判定、注入、灵敏度、调试面板、设置） | 5 天 |
| M2 | 发现 + 配对 + token + 重连 | 3 天 |
| M3 | 全键盘区（键码映射、粘滞修饰键） | 4 天 |
| M4 | 语音区（端侧 ASR + 剪贴板注入） | 3 天 |
| M5 | 延迟调优、HDS 收口、验收 | 3 天 |

合计 v1 ≈ 4 周单人；MVP（可当触摸板用）≈ 2 周。

---

## 10. 验证与提交门（开发流程约定）

- 日常：鸿蒙端 deveco-mcp `check` 快检 + `devecocli run --apply` 真机快部署；daemon `go test ./...`。
- 视效改动：`devecocli ui screenshot` + 像素采样留证（HDS verify 流程）。
- 收口门：`devecocli build` 全量类型检查 + daemon 交叉编译两架构。
- 联调分层纪律：先 daemon `--test`（无手机）→ echo（无注入）→ 真触摸；任何"光标不动"按此三层定位。
- 真机验收程序：`docs/PROBE-LINUX.md`（M0 后执行）；鸿蒙真机多指 id 验证于 M1 期间完成。

---

## 11. 验收标准（关键场景）

1. 文本选中拖拽、双指滚动网页、双指点按弹右键菜单、长按拖文件——各 10 次成功率 ≥90%（3 人盲测）。
2. 三指切换 100 次零截屏误触、零残留按键状态。
3. 键盘区输入 `Ctrl+C/V`、`Super`、`Alt+Tab` 行为正确；中文经 fcitx 打字通畅。
4. 语音听写一段 50 字中文，2 次内正确上屏，且事后用户剪贴板**内容**不变（owner 变为 mutter 剪贴板管理器属正常行为，不判失败）。
5. 延迟达 §8 指标（调试面板 + 录屏双口径）。
6. 杀掉 daemon / 关闭 WiFi 再恢复，30s 内自动重连不假死。

---

## 12. 安全与隐私

- 全部通信仅局域网；无任何云端依赖（ASR 端侧本地执行）。
- token、配对信息存储位置双端最小化；剪贴板备份仅驻内存。
- 麦克风使用仅限语音区按住期间，权限申请时机在使用时。

## 13. 风险登记

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| 三指手势与系统手势冲突 | 中 | 仅水平方向；验收项 2 兜底 |
| wl-clipboard 在非 GNOME 合成器不可用 | 低 | 启动检测 + 明确降级提示（v1 只承诺 GNOME Wayland） |
| speechRecognizer 语言/机型覆盖 | 低 | 明确标注中文/中英；端侧离线已由官方文档确认 |
| 公司/公共 WiFi AP 隔离导致广播不通 | 中 | 发现页失败提示自检文案 |
| UDP 被劣质路由丢弃 | 低 | 心跳检测 + TCP 兜底提示切网络 |
| 触摸 Move 按 vsync 帧合并、双指插入短暂丢 move | 中 | 消费 touches + getHistoricalPoints 补点〔溯源：faqs-arkui-907〕 |
| ASR 默认参数陷阱（20s 上限/写流模式/800ms 判停） | 中 | 参数显式全量传递 + ASR 会话单例封装 |
| 剪贴板恢复时序竞态（慢应用粘到旧内容） | 低 | 恢复延迟可配 300–500ms；验收口径定为"内容不变" |

## 14. 待决决策

1. daemon 语言：默认 Go；若追求更小内存占用与零 GC 毛刺可选 Rust（不影响协议）。
2. 产品名 / 图标 / 是否上架应用市场（影响签名与隐私声明，不影响架构）。

---

## 附录 A：查证索引

| 主题 | 事实源 |
| --- | --- |
| 鸿蒙 API 文档 ID（触摸/网络/语音/窗口/工程/v2 预留/已排除路线） | `docs/HARMONY-DOCIDS.md` |
| Linux/Wayland 集成事实与上游来源（uinput/udev/wl-clipboard/指针加速/Ubuntu 26.04 平台事实） | `docs/RESEARCH-WAYLAND.md` |

## 附录 B：v2 扩展预留

- 协议事件源扩展位：`SENSOR_MOVE`（空中鼠标，`@ohos.sensor`）。
- 息屏常采场景引入长时任务（DATA_TRANSFER）+ RunningLock。
- 多主机切换 UI、媒体遥控模式。
- **macOS 后端（v2 平台扩展）**：
  - 注入：CGEvent API（`CGEventPost` 注入鼠标移动/按键/平滑滚动），对应 uinput 后端实现同一 `DeviceWriter` 接口；
  - 权限：辅助功能授权（`AXIsProcessTrusted` 检测 + 系统设置跳转引导），首次运行向导必备；
  - 键映射：HID usage code → macOS virtual keycode（kVK_*）映射表；修饰键语义映射 Ctrl↔Cmd 可配置；
  - 文本注入：`pbcopy` 写剪贴板 + 注入 Cmd+V，系统自带命令、无第三方依赖，比 Wayland 路径更简单；
  - 工程注意：Go 走 cgo 或 Swift 辅助进程（不影响 Linux 侧无 cgo 属性，两平台构建互不影响）；
  - 估算：1~1.5 周（含键映射表与权限引导流程）。
