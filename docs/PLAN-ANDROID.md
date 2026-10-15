# Android 端研发规划（PRD 附录 B 平台扩展）

> 状态：归档（待启动）。语音路径已定（2026-10-13，用户决策）：**安卓端不做自研 ASR，语音输入由系统 IME 自带语音能力承接**（§2.6）。
> 单一事实源纪律不变：协议格式以 `protocol/PROTOCOL.md` + `protocol/testvectors.json` 为准；Android 是协议的第三个实现，**不改协议、不动 daemon**。
> 范围变更：PRD §1.4 原列「Android / iOS 端」为 Non-goals；本规划生效后需同步修订 PRD §1.3/§1.4（平台矩阵加 Android 行，Non-goals 移除 Android 项）。

## 0.5 语音方案调研存档（2026-10-13，/tmp/padlink-asr-poc 一次性程序）

为回答「自研端侧 ASR 是否可行」做过完整 PoC（结论：可行，sherpa-onnx + zipformer-zh-int8-2025-06-30，45 字中文约 98% 字准、RTF≈0.04、模型 167MB 按需下载）。**随后用户决策安卓端不内置 ASR**，语音走系统 IME 自带语音输入（§2.6）。PoC 数据存档备查：若未来要做脱离键盘层的独立语音浮件（对齐鸿蒙交互），可直接复用该结论，无需重新验证。

## 0. 定位与目标

把鸿蒙端（apps/harmony，约 7900 行 ArkTS）的能力平移到 Android 手机：触摸板 + 全键盘 + 语音听写，局域网直连同一 daemon。交互规格、协议语义、验收指标（PRD §8/§11）原样继承，不做平台差异化功能。

不做的事（继承红线）：不做公网/内网穿透；不做空中鼠标；不做 iOS；不改 daemon 注入层。

## 1. 平台接缝盘点（鸿蒙 → Android 映射）

| 鸿蒙侧（现状） | Android 侧方案 | 风险 |
| --- | --- | --- |
| ArkUI V2 / ArkTS（@ComponentV2，HDS 体系） | Jetpack Compose + Material 3 官方规范（不照搬鸿蒙 UI，§2.7），状态用 ViewModel + StateFlow | 低 |
| `common/proto/` 编解码 + HMAC（CryptoHmac） | 纯 Kotlin 移植；HMAC 用 `javax.crypto.Mac`（JDK 标准库） | 低 |
| `common/gesture/` 手势引擎（455 行纯逻辑） | 纯 Kotlin 直译；触摸源对应 MotionEvent | 低 |
| TouchEvent `touches` + `getHistoricalPoints()`（帧内合并报点） | `MotionEvent` pointer 语义 + `getHistoricalX/Y(pointer, hist)`——语义一一对应 | 低 |
| `@ohos.net.socket` UDP 广播发现 | `DatagramSocket.setBroadcast(true)`；WiFi 出口锁定用 `ConnectivityManager` + `Network.bindSocket()`（对应鸿蒙 NetHandle.bindSocket） | 中 |
| `@ohos.net.socket` TCP 会话 | `java.net.Socket`；沿用「connect 必须换新 socket 实例、回调按会话代次挂载」的 lore 约束（d7bdaf2f） | 低 |
| Core Speech Kit 端侧 ASR（zh-CN 离线） | **不实现自研 ASR**（已定，§2.6）：语音由系统 IME 自带语音输入承接，经键盘层既有文本镜像链路上行 | 无 |
| preferences 持久化（灵敏度/主机列表/设备指纹） | SharedPreferences 或 DataStore；token 存储方案见 §6 决策点 | 低 |
| `setWindowKeepScreenOn` | `Window.addFlags(FLAG_KEEP_SCREEN_ON)` / Compose `keepScreenOn` | 低 |
| 系统 IME + previewText 组合态判别 | Compose `TextField` + `TextFieldValue.composition` 差分，提交分派契约照抄 lore 6aca743b | 中 |
| 底部手势区动态避让（TYPE_NAVIGATION_INDICATOR） | `WindowInsets.navigationBars` / `systemGestures`，Compose 原生支持，比鸿蒙更直接 | 低 |
| HID usage 码表（KeyCodes.ets，172 行） | 平台无关数据表，原样移植 | 低 |
| 灵敏度/滚动/调试设置（SettingsModel） | 同结构移植 | 低 |

## 2. 关键技术方案

### 2.1 工程形态

- 位置 `apps/android/`，Kotlin + Jetpack Compose，单 app 模块；协议/手势/键盘模型等纯逻辑放 `core/` 纯 Kotlin 包（不 import android.*），JVM 单测直接加载——对应鸿蒙 `tools/etstest` 的「纯逻辑可 node 直跑」分层。
- Gradle **wrapper 入库钉版本**（构建可复现）；本机环境已具备：JDK 17（temurin）、Android SDK（buildTools 35.0.0、pixel_6_api35 模拟器镜像）。compileSdk 35；minSdk 见 §6 决策点。
- 签名材料不入库（对齐 lore ec6af6e0）：keystore 走 `local.properties`/环境变量，仓库只放模板。

### 2.2 协议层与黄金向量对拍

- `common/proto/`（Codec/Packet/Auth/Utf8/PacketType，约 600 行 ArkTS）逐文件移植为纯 Kotlin。
- 验收门：JUnit 单测从仓根 `protocol/testvectors.json` 读黄金向量全量对拍（正/负用例），与鸿蒙 `tools/prototest/run.mjs`、daemon Go 单测构成三端对拍。**不通过不联调**（PRD §0.2 铁律 2 的平移）。
- seq 回绕、payload_len ≤1400、校验失败丢弃计数等语义逐条照搬 PROTOCOL.md §2.2。

### 2.3 网络：发现 / 配对 / 会话

- 发现：UDP 广播 `255.255.255.255:53021`，失败兜底子网定向广播（与鸿蒙 DiscoverCore 同策略）；锁 WiFi 出口用 `ConnectivityManager.getAllNetworks()` 筛 TRANSPORT_WIFI + `Network.bindSocket()`。
- 会话：TCP 通道承载 KEY/BUTTON/TEXT/HELLO/ECHO/BYE/NOTICE，UDP 单播承载 MOVE/SCROLL；1Hz ECHO 心跳、3 次超时判失联、指数退避重连（1s/2s/4s…上限 15s）照 FR-6 实现。
- 必守的已踩坑（lore d7bdaf2f，鸿蒙真机教训）：connect 换新 socket 实例；close/error 回调按会话代次挂载；切换主机成功后写默认主机。
- NOTICE(0/1) 下行处理照鸿蒙 NoticeCore：控制权冷静期倒计时常驻主控页、文本未上屏本地化 toast。
- 权限：`INTERNET`、`ACCESS_NETWORK_STATE`（对应鸿蒙 GET_NETWORK_INFO 的踩坑——发现筛网必需）。
- 模拟器约束（写入联调文档）：Android 模拟器在 NAT 后（宿主 10.0.2.2 可达），**UDP 广播发现出不了虚拟网**——发现/广播链路必须真机验证；模拟器联调走调试面板的手动 IP 入口（鸿蒙已有同款手动配对卡片，设计照搬）。

### 2.4 触摸板与手势引擎

- GestureEngine/Accel/TouchSample/GestureConfig（约 600 行）纯逻辑直译；手势判定阈值、加速曲线、惯性滚动参数不动。
- Compose 触摸入口用 `pointerInput` + `awaitPointerEventScope`，**消费全部 pointers 与 historical points**（对应鸿蒙 touches 语义，lore 7e4e586f/风险表第 6 条），禁用框架手势竞争（整层 consume）。
- 单测：鸿蒙侧 `tools/etstest` 的手势用例移植为 JUnit（用例语义一一对应，数字不断言实现细节）。
- 验收指标不变：P50 ≤ 40ms / P95 ≤ 80ms（调试面板 + 录屏双口径），真机测，模拟器只做链路通断。

### 2.5 键盘区（混合形态，契约照抄）

lore 6aca743b 四条契约原样落实：可映射 ASCII 逐键走 HID；不可映射（CJK/emoji）合并 TEXT run 走剪贴板注入（1400B 分块）；IME 组合态不上行只认提交增量；单字符提交遇粘滞修饰合成组合键、多字符清粘滞直发；离开键盘区 zoneLeave 补发一切未抬起 up（残留键红线）。
Android 差异点：组合态判别用 `TextFieldValue.composition` 非空判定；删除镜像 KEY Backspace 的中缀插入用例（鸿蒙单测已踩坑修复）一并移植。

### 2.6 语音（方案已定：不自研 ASR，走系统 IME 语音输入）

**安卓端不内置语音识别**。键盘层的文本输入本就由系统 IME 承接（§2.5 混合形态），主流 Android IME（Gboard、讯飞、搜狗、微信键盘等）均自带话筒键语音输入，其识别结果以普通 IME 提交文本进入输入区，经既有「组合态不上行、提交增量镜像」链路上行电脑——**零新增代码路径、零权限（不要 RECORD_AUDIO）、零模型体积**，且标点、语种、离线能力跟随用户自选 IME。

与鸿蒙端的交互差异（明示、可接受）：鸿蒙有独立语音浮件、键盘收起时也可按住说话；安卓端语音需先展开键盘层、点 IME 话筒键。语音上屏后仍走 TEXT/HID 分派与 NOTICE(1) 失败提示，daemon 侧零感知。

若未来要补齐独立语音浮件，自研 ASR 的可行性已验证（§0.5 存档），不构成立项风险。

隐私口径：语音数据路径由用户所选 IME 决定（多数 IME 默认在线识别），须在应用内说明与隐私政策中如实标注，不再承诺「语音端侧离线」（PRD §12 对安卓端做相应差异化表述）。

### 2.7 UI 与资源

- **视觉与交互规范：Material Design 3（Material You）官方指南为唯一依据**（m3.material.io，动工时现场查证、不凭记忆），Compose Material3 组件库 + `colorScheme`/动态取色，亮暗主题跟随系统。鸿蒙侧的 HDS 特有表达（ImmersiveMaterial 状态胶囊、`sys.color.*` token 体系、HDS 归层）**不移植**，一律换成 Material 对应物（状态用 Material banner/chip 类组件，颜色字号一律 Material3 theme token，禁硬编码）。
- 页面流保持功能对应（形态按 Material 惯例重组）：引导页 / 发现与配对页 / 主控页（触摸面常驻 + 底部键盘停靠层；**无语音浮件**，语音走键盘层 IME，见 §2.6）/ 设置页 / 调试面板（版本号连点 5 次进入，导航按 Material Navigation 惯例）。
- 功能级交互约束保留（与 UI 体系无关的契约）：触摸面常驻、键盘展开期间触摸面仍可用；键盘收起唯一入口为键条「收起」键（lore 5844a41b 的用户决策）；离开键盘区补发未释放 up；NOTICE 冷静期倒计时在主控页常驻可见。
- 组件状态与无障碍按 Material 规范实现：按钮五态（Default/Hover/Pressed/Disabled/Loading）、触摸目标 ≥48dp、关键控件 contentDescription。
- 文案中/英双 locale（对应鸿蒙 base/zh_CN/en_US 三同步约束 8c9483f9 的 Android 映射）。

## 3. 阶段划分与验收标准（单人全职估算）

| 阶段 | 内容 | 通过标准 | 工期 |
| --- | --- | --- | --- |
| A0 | 工程骨架：wrapper、Compose Hello、模拟器构建运行入库 | `./gradlew assembleDebug` 通过，模拟器起屏 | 1 天 |
| A1 | 协议层移植 + 黄金向量 JUnit 对拍 | testvectors 全量绿（正/负） | 2 天 |
| A2 | 网络/发现/配对/token/重连 + 存储 | 真机发现配对成功；断连 30s 自动恢复；错码 5 次锁定 | 3 天 |
| A3 | 触摸板链路：手势引擎 + 主控页 + 调试面板 | 光标跟手；§11.1 手势成功率 ≥90%；echo/合成轨迹自测过 | 4 天 |
| A4 | 键盘区（IME 混合 + 粘滞修饰 + 分派契约；含 IME 语音输入上行验证） | Ctrl+C/V、Super、Alt+Tab、中文上屏可用；IME 话筒语音输入正常上行；残留键红线过 | 3 天 |
| A5 | 设置页收口 + 延迟实测 + 真机验收 | §8 延迟双口径达标；§11 全项（语音项改为 IME 语音上行） | 2 天 |

合计约 15 天（协议/交互零设计、无自研 ASR，主要成本在平台接缝与真机回归）。

离线验证门（每次收口）：`./gradlew test`（黄金向量 + 手势 + 键盘分派单测）+ `assembleDebug`；真机验收项与离线项显式区分（lore 632fc3ff），不拿模拟器链路通断宣称验收通过。

## 4. 影响范围与回归

- `daemon/`、`protocol/`：**零改动**。Android 端上线前用 padlinktoy 对新端链路回归一轮（replay/fuzz 不变）。
- 需同步修订：`docs/PRD.md` §1.3/§1.4（平台矩阵 + Non-goals）、`README.md` 平台说明、`docs/HANDOFF.md` 环境与里程碑段。
- 新增：`apps/android/`、`docs/PLAN-ANDROID.md`（本文）；Android 版隐私政策（若上架商店：权限清单 `android.permission.*` 逐条声明，对齐 lore b5d817a3 的 AGC 教训）。
- CI：daemon 现有 GitHub Actions 不动；Android 侧 `gradlew test assembleDebug` 门禁可后加入（A6 视需要）。

## 5. 风险清单

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| IME 语音输入体验依赖用户所选 IME（识别质量/离线能力不一） | 低 | 产品定位即「承接系统输入法」；应用内说明引导换用带语音的 IME |
| 模拟器无法验证广播发现与多指触摸 | 中 | 手动 IP 调试入口；发现/多指/延迟验收全部真机 |
| IME 组合态差分在 Compose 的语义边界 | 中 | A4 先做差分最小验证页再上全键盘；鸿蒙既有用例移植兜底 |
| ROM 省电策略杀后台影响保活 | 低 | 产品形态为前台亮屏使用，不需要长时后台；不做前台服务 |
| 广播被 AP 隔离/劣质路由丢弃 | 中 | 沿用鸿蒙失败自检文案 + 手动 IP 入口（既有 PRD 风险项） |

## 6. 待决决策

1. **minSdkVersion**：建议 29（覆盖主流在役机型；触摸板/键盘/IME 语音均无高 API 依赖）。
2. **token 存储**：鸿蒙端为 preferences 明文（私有目录）；Android 对齐用 SharedPreferences 私有目录即可，或升级 Keystore 包裹（安全增益 vs 复杂度，倾向后者但可降级）。
3. **分发形态**：侧载 APK（现阶段）vs 商店上架（另起签名/隐私政策工作，不在本规划工期）。
