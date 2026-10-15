# iOS 端研发规划（PRD 附录 B 平台扩展）

> 状态：归档（待启动）。与 Android 端同一方法论：协议/交互契约照搬，UI 用平台官方规范，语音不自研。
> 单一事实源纪律不变：协议格式以 `protocol/PROTOCOL.md` + `protocol/testvectors.json` 为准；iOS 是协议的第四个实现，**不改协议、不动 daemon**。
> 范围变更：PRD §1.4 原列「Android / iOS 端」为 Non-goals；本规划生效后需同步修订 PRD §1.3/§1.4（与 PLAN-ANDROID 一并处理）。
> 设计规范：Apple Human Interface Guidelines（HIG）为唯一设计依据，动工时现场查证、不凭记忆。

## 0. 定位与目标

把鸿蒙端能力平移到 iPhone：触摸板 + 全键盘，局域网直连同一 daemon；语音输入由 iOS 系统键盘听写承接（§2.6），不自研 ASR。交互契约与验收指标（PRD §8/§11）原样继承，UI 按 Apple HIG 原生设计，不照搬鸿蒙界面。

不做的事（继承红线）：不做公网/内网穿透；不做空中鼠标；不改 daemon 注入层。

## 1. 平台接缝盘点（鸿蒙 → iOS 映射）

| 鸿蒙侧（现状） | iOS 侧方案 | 风险 |
| --- | --- | --- |
| ArkUI V2 / ArkTS（HDS 体系） | SwiftUI + Apple HIG（§2.7），状态用 `@Observable` / Combine | 低 |
| `common/proto/` 编解码 + HMAC | 纯 Swift 移植；HMAC-SHA256 用 **CryptoKit**（系统框架，零三方依赖） | 低 |
| `common/gesture/` 手势引擎 | 纯 Swift 直译；触摸源对应 UITouch | 低 |
| TouchEvent `touches` + `getHistoricalPoints()` | `UIEvent.coalescedTouches` / `predictedTouches`——语义对应良好 | 低 |
| UDP 广播发现 | 广播需受限 entitlement（付费账号专属，已放弃）；**Bonjour/mDNS 主路径**（daemon 加 mDNS 广告，免费账号可用），单播扫描/手动 IP 兜底；本地网络权限弹窗一次、不可绕 | 中（路径已定） |
| TCP 会话 | Network framework `NWConnection`；沿用「connect 换新实例、回调按会话代次挂载」约束 | 低 |
| 局域网通信 | iOS 14+ **本地网络隐私权限**（`NSLocalNetworkUsageDescription` + 系统弹窗） | 中 |
| preferences 持久化 | UserDefaults；token 进 **Keychain**（天然安全存储，无争议） | 低 |
| `setWindowKeepScreenOn` | `UIApplication.isIdleTimerDisabled = true` | 低 |
| 系统 IME + previewText 组合态 | SwiftUI TextField / UITextInput `markedText`（组合态判别语义对应） | 中 |
| 语音（Core Speech） | 不自研；系统键盘听写键（§2.6），模型系统管理、App 零下载 | 无 |
| HID usage 码表 / 设置项 | 平台无关，原样移植 | 低 |

## 2. 关键技术方案

### 2.1 工程形态

- 位置 `apps/ios/`，Xcode 工程 + SwiftUI；协议/手势/键盘模型等纯逻辑收进本地 Swift Package `PadlinkCore`（不 import UIKit/SwiftUI），XCTest 直接跑——对应鸿蒙/安卓的「纯逻辑可直测」分层。
- 本机环境：Xcode 27.0 已装；**iOS 模拟器运行时未安装**（预检实测）——I0 先执行 `xcodebuild -downloadPlatform iOS`。
- 签名材料不入库；开发期 Personal Team 可跑真机调试，受限 entitlement 需要付费开发者账号（§2.3）。

### 2.2 协议层与黄金向量对拍

- `common/proto/` 逐文件移植为纯 Swift（Codec/Packet/Auth/Utf8/PacketType）；HMAC 用 CryptoKit `HMAC<SHA256>`，截断 8 字节、常数时间比较按 PROTOCOL §5 自实现。
- 验收门：XCTest 从仓根 `protocol/testvectors.json` 读黄金向量全量对拍（正/负用例），与鸿蒙 node harness、daemon Go 单测、安卓 JUnit 构成四端对拍。**不通过不联调**。
- seq 回绕、payload_len ≤1400、校验失败丢弃计数等语义逐条照搬 PROTOCOL.md §2.2。

### 2.3 网络：发现 / 配对 / 会话（iOS 最大接缝）

- **本地网络权限不可绕（iOS 14+，WWDC20-10110 实证）**：任何局域网通信（**单播 UDP/TCP 与广播一视同仁**）在授权前全部失败（Network framework 报 privacy required）；系统弹窗只在首次触网时出现、**无编程请求 API**；Bonjour 浏览同样触发。UX 纪律照 Apple 官方建议：不在启动时触网，进发现页/点「搜索电脑」时才发起；被拒后设置页给引导与 `Settings` 深链。（用户已确认：授权弹窗不构成立项阻碍，按正常权限设计处理。）
- **发现路径（已定，2026-10-13 用户决策：不申请付费开发者账号，entitlement 路径永久移除）**：
  1. **Bonjour/mDNS 主路径**：iOS 侧 `NWBrowser` 浏览 `_padlink._udp`（Info.plist 声明 `NSBonjourServices`）——Apple 官方钦定发现方式，**免费账号即可用、无需任何审批**（权限弹窗仍要，但那本来就绕不开）。代价：daemon 需新增 mDNS 广告（附加能力，协议不动，见 §4）。
  2. 子网单播扫描兜底：向本机 /24 逐地址单播探测 53021/UDP（需先实证 daemon 应答单播 DISCOVER——协议层应无差别，I2 第一步用 padlinktoy 验证）。
  3. 手动 IP 入口（调试面板常驻，鸿蒙/安卓同款）。
  - 被排除：自定义 UDP 广播所需的 `com.apple.developer.networking.multicast` 为受限 entitlement，仅付费账号可申请——与「不申请付费账号」约束冲突，移除。
- 会话/心跳/重连/NOTICE 处理与鸿蒙、安卓完全一致（FR-6、PROTOCOL §4.7/§4.9）；lore d7bdaf2f 的会话代次守卫逐条落实。
- 模拟器红利（与安卓相反）：iOS 模拟器共享宿主网络栈，localhost 直达本机 daemon、广播在宿主子网内通常可达——发现链路可先在模拟器联调；但**权限弹窗/拒绝态必须真机验证**（模拟器权限行为与真机有差异）。

### 2.4 触摸板与手势引擎

- GestureEngine/Accel/TouchSample/GestureConfig 纯 Swift 直译，判定阈值/加速曲线/惯性参数不动。
- 触摸入口用 UIKit 层（`touchesBegan/Moved/...` 挂到 SwiftUI 视图Representable），**消费 `coalescedTouches`** 取回帧内合并报点（对应鸿蒙 historical points 语义）。
- 单测：鸿蒙 `tools/etstest` 手势用例移植为 XCTest。
- 模拟器多点触控能力有限（仅双指捏合模拟），**手势验收全部真机**；验收指标不变：P50 ≤ 40ms / P95 ≤ 80ms 双口径。

### 2.5 键盘区（混合形态，契约照抄）

lore 6aca743b 四条契约原样落实（ASCII→HID、CJK→TEXT 分块、组合态不上行、单字符+粘滞修饰合成组合键、zoneLeave 残留键红线）。
iOS 差异点：组合态判别用 `UITextInput.markedTextRange` 非空判定；SwiftUI TextField 拿不到 marked text 时下沉 UITextField（UIViewRepresentable）。删除镜像/中缀插入用例随鸿蒙单测一并移植。

### 2.6 语音（方案已定：不自研，系统键盘听写）

与安卓端同一决策：键盘层文本由系统 IME 承接，iOS 系统键盘自带**听写键**（普通话为系统级能力，端侧识别机型覆盖由 Apple 保证），识别结果以普通提交文本进入输入区、经既有镜像链路上行。**App 零语音代码、零权限、零模型管理**。

与安卓的结构性差异（iOS 更有利）：即便未来要做 App 内语音浮件，系统 `SFSpeechRecognizer` 是全机型官方 API、模型由系统管理（设置里下载语言包），可用 `requiresOnDeviceRecognition` 强制端侧——**任何形态下 App 都不需要自己下载模型**。

隐私口径同安卓：语音数据路径由系统听写服务决定，上架文案如实标注，不承诺「语音端侧离线」。

### 2.7 UI 与资源

- **Apple HIG 为唯一设计依据**（developer.apple.com/design，动工时现场查证）；SwiftUI + SF Symbols，亮暗模式、Dynamic Type 跟随系统。不照搬鸿蒙 HDS 表达（材质胶囊、归层、token 体系），状态提示用 HIG 对应的 banner/alert 组件。
- 页面流功能对应（形态按 HIG 惯例重组）：引导 / 发现与配对 / 主控（触摸面常驻 + 键盘停靠；无语音浮件）/ 设置 / 调试面板（版本号连点 5 次）。
- 功能级交互契约保留：触摸面常驻且键盘展开时仍可用；键盘收起唯一入口为键条「收起」键；离页补发未释放 up；NOTICE 冷静期倒计时常驻可见。
- 控件状态与无障碍按 HIG：按钮各态、触摸目标 ≥44pt、VoiceOver label。
- 文案中/英双 locale（对齐鸿蒙/安卓的同步约束）。

## 3. 阶段划分与验收标准（单人全职估算）

| 阶段 | 内容 | 通过标准 | 工期 |
| --- | --- | --- | --- |
| I0 | 工程骨架：Xcode 工程 + PadlinkCore 包 + 模拟器运行时安装 | 模拟器起屏；XCTest 可跑 | 1 天 |
| I1 | 协议层 Swift 移植 + 黄金向量对拍 | testvectors 全量绿（正/负） | 2 天 |
| I2 | daemon 加 mDNS 广告 + iOS 网络/发现（Bonjour 主路径 + 单播兜底）/配对/token/重连 + 存储 | 真机发现配对成功；断连 30s 自动恢复；错码 5 次锁定 | 4 天 |
| I3 | 触摸板链路：手势引擎 + 主控页 + 调试面板 | 光标跟手；§11.1 手势成功率 ≥90% | 4 天 |
| I4 | 键盘区（marked text 混合 + 粘滞修饰 + 分派契约；含系统听写上行验证） | Ctrl+C/V、Super、Alt+Tab、中文上屏可用；听写文本正常上行；残留键红线过 | 3 天 |
| I5 | 设置页收口 + 延迟实测 + 真机验收 | §8 延迟双口径达标；§11 全项（语音项为听写上行） | 2 天 |

合计约 15–16 天，另加 entitlement 审批的日历等待（可与其他阶段并行）。

离线验证门（每次收口）：`xcodebuild test`（黄金向量 + 手势 + 键盘分派）+ 模拟器构建；真机验收项与离线项显式区分（lore 632fc3ff）。

## 4. 影响范围与回归

- `protocol/`：**零改动**。`daemon/`：协议逻辑零改动；发现走 Bonjour 主路径，daemon 需**新增 mDNS 广告**（附加能力：在现有广播应答旁注册 `_padlink._udp` 服务广播，不动任何既有报文格式与端口）——免费账号约束下这是必做项而非可选项，I2 范围内一并实现。
- 上线前 padlinktoy 对新端链路回归一轮（replay/fuzz 不变）。
- 需同步修订：`docs/PRD.md` §1.3/§1.4、`README.md`、`docs/HANDOFF.md`（与 PLAN-ANDROID 的修订合并执行）。
- 新增：`apps/ios/`、本文档。
- 商店相关当前不适用（无付费账号、不上架）；若未来上架再补：App Store 隐私清单、本地网络用途声明、截图素材。

## 5. 风险清单（2026-10-13 调研 + 用户决策后修订）

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| **无付费账号 → 无正式分发渠道**（免费 Personal Team 签名 7 天到期需重部署，App Store/TestFlight 均不可用） | **高（结构性约束，已接受）** | 定位「自用/开发态」：iOS 端价值在自用场景，上架留待未来账号决策；代码与商店化无耦合，届时直接打包提交 |
| 本地网络权限被用户拒绝 | 中 | 一次弹窗、全 App 生效；拒绝态检测 + 设置页引导 + Settings 深链；弹窗时机放在用户主动进发现页（WWDC 建议） |
| 权限拒绝时降级路径全灭 | 中 | 单播/手动 IP 同样受门控——拒绝后无任何通信可能，引导是唯一解（与所有 LAN 应用相同的平台现实） |
| ~~组播 entitlement 审批~~ | 已移除 | 不申请付费账号，广播路径放弃；Bonjour + 单播兜底 |
| 品类审核风险 | 低 | 先例充足（Unified Remote、Remote Mouse、Mobile Mouse 在架且 2026-10 仍更新）；无注入类私有 API（上架时适用） |
| SwiftUI TextField 拿不到 marked text | 中 | 预案即下沉 UITextField，I4 先做差分最小验证页 |
| 模拟器多指能力缺失、权限弹窗行为与真机有差异 | 低 | 手势与权限验收全部真机 |
| 企业/公共 WiFi AP 隔离 | 中 | 沿用既有 PRD 风险项的失败自检文案 |

## 6. 待决决策

1. **Deployment target**：建议 iOS 17（`@Observable`、NavigationStack 完整形态；2026 年覆盖率已足够）。若要 iOS 16 需回退 ObservableObject。
2. ~~发现路径选择~~：**已定**（2026-10-13 用户决策，不申请付费账号）——Bonjour 为主（daemon 新增 mDNS 广告）、单播扫描兜底、手动 IP 调试入口；entitlement 路径移除。
3. ~~Apple Developer 账号~~：**已定**——不申请付费账号。iOS 端定位自用/开发态（免费 Personal Team 真机调试，7 天签名到期重部署）；上架留待未来决策，代码不为商店化做任何预埋妥协。
4. **分发形态**：当前 = Xcode 部署到自有设备；未来若上架走 TestFlight → App Store。
5. token 存 Keychain（已定，无争议，无需决策）。
