# macOS 移植规划（PRD 附录 B v2 前置研发）

日期：2026-10-09 ｜ 依据：PRD §4.8 / 附录 B（macOS v2 预留：CGEvent + cgo 或 Swift 辅助进程）
动机：开发机即 macOS（27.0.1 arm64），鸿蒙模拟器/真机 → 本机 Mac 的链路比 Linux 更易做真机验证。

## 进度（2026-10-09 当日）

- M-macOS-1 ✅ 注入后端（quartz/uinput_darwin/键表/AX 门控），`--test` 真机 PASSED
- M-macOS-2 ✅ 文本注入 pbcopy/pbpaste 后端 + Cmd+V 平台化
- M-macOS-3 ✅ 控制 socket `$TMPDIR` 回落、配对通知 osascript、launchd 安装物（install.sh 本机实测通过）
- M-macOS-4 ✅ CI 迁 macos-latest + darwin cgo 构建（快照 8 产物验证）；端到端联调通过——
  模拟器经调试手动配对入口连本机 Mac：配对（15s 慢输码）→ Connected（RTT 1ms）→
  位移/按键/点击注入全部实证（光标坐标探针 + TextEdit AppleScript 回读）

设计变更记录：AX 授权等待从「2 分钟超时退出」改为「驻留轮询不限时」——
launchd 场景下超时退出会触发 KeepAlive 重启并反复弹系统授权框；前台场景用户可 Ctrl+C 中断。

E2E 实测结论（2026-10-09，模拟器 Pura90API26 ↔ 本机 Mac）：

1. **模拟器发现非对称**：UDP HELLO 能出模拟器（宿主收到，源地址 127.0.0.1），
   但应答路由不回去——广播发现在模拟器不可用，实锤只能走手动 IP（调试入口）。
2. **踩出跨平台配对缺陷**（已修）：配对会话 10s 读超时 < 用户读码输码时长，
   慢输码必断连。修复=配对轮次期间读超时宽限至码 TTL+10s（session 层 pairWaitUntil）。
3. 滚轮手机端双指手势 CLI 无法模拟，注入路径由 `--test` 像素滚轮覆盖；
   语音听写模拟器无 MIC 链路，留真机验证。
4. **踩出修饰键合成缺陷**（2026-10-09 用户报障复现，已修）：CGEvent 投递的合成
   修饰键 keyDown 不进入系统修饰状态，后续合成按键 flags 恒 0——Cmd+V 退化为
   裸 v（TEXT 注入整体失效，中文/语音落地全灭），Shift+字母退化为小写。
   修复=Go 侧跟踪按住修饰键，PostKey 显式 CGEventSetFlags；
   探针实证 TextEdit 落「你好世界中文ABC」+「A」，剪贴板原内容恢复。

## 0. PoC 结论（本机实测，/tmp/padlink-mac-poc 一次性程序）

| 验证项 | 方法 | 结果 |
| --- | --- | --- |
| cgo 工具链 | clang 21 + ApplicationServices 框架编译 | ✅ |
| CGEvent 相对位移注入 | 注入 +3px 后回读光标位置 | ❌ 未授权时被静默丢弃（符合文档行为） |
| TCC 辅助功能权限 | AXIsProcessTrusted / WithOptions(prompt) | ✅ 检测与系统授权弹窗均可用 |
| 剪贴板链路 | pbcopy/pbpaste 写入-回读-恢复 | ✅ 无需任何权限 |

授权后复测（同日，终端已获辅助功能权限）：相对位移注入 ✅（+3px 精确命中），
M-macOS-1 真机验收 `padlinkd --test`（画圆/点击/敲键/滚动）PASSED。

结论：注入通道=Quartz CGEvent（cgo），门槛=TCC 辅助功能权限（授权前事件被静默丢弃，
必须做首启引导流）；剪贴板=pbcopy/pbpaste 零权限可用。方案可行性成立。

## 1. 平台接缝盘点（实测全量梳理）

| 接缝 | Linux 现状 | macOS 方案 | 改动量 |
| --- | --- | --- | --- |
| 注入后端 | `uinput_linux.go`（build tag 隔离，stub 保编译） | 新增 `uinput_darwin.go` + `internal/quartz`（薄 cgo 封装） | 新增，不改 Linux |
| 键码 | inject 内 HID→Linux KEY_*（hid_map.go） | darwin 后端内 KEY_*→CGKeyCode 对照表（覆盖面与 hid_map 对齐） | 新表 |
| 文本注入 | wl-copy/wl-paste（textinject 命令硬编码） | pbcopy/pbpaste；命令集参数化按平台选 | 小改 |
| 粘贴组合键 | inject.CtrlV()=Ctrl+V | Cmd+V（main.go 按平台注入组合键函数，不动 inject 接口） | 一行级 |
| 主机自检 | hostinfo：Wayland/gsettings 加速 profile | darwin 分支：跳过 Linux 检查；指针加速见 §2.3（架构上绕开） | 小改 |
| 控制通道 | Unix socket，路径依赖 XDG_RUNTIME_DIR | darwin 回落 `~/Library/Application Support/padlinkd/` | 小改 |
| 常驻/安装 | systemd user unit + install.sh | launchd LaunchAgent plist + packaging/macos/install.sh | 新增 |
| 发现/配对/会话/proto | 纯 net 代码（session/udp.go 等） | 零改动 | 无 |
| CI 发版 | ubuntu-latest + goreleaser（CGO_ENABLED=0） | release 迁移 macos-latest：darwin cgo 本机构建，linux 纯 Go 交叉照旧（nfpm 纯 Go 不受影响） | 配置 |

鸿蒙 App 侧零改动（协议/会话层跨平台复用，PRD 既定属性）。

## 2. 关键技术方案

### 2.1 注入后端：CGEvent（cgo），Device API 不变

darwin 后端实现与 Linux 相同的 `Open/KeyEvent/RelEvent/Sync/Close` 面，`Sync()` 在 darwin 为空操作
（CGEventPost 逐事件落窗Server）。cgo 面收敛在 `internal/quartz` 一个包内，只暴露
moveAbs/scrollPixel/key/button/axTrusted 五个函数级原语，业务逻辑留在 Go 侧。
Linux 侧保持无 cgo 单二进制（PRD §4.8 既定：两平台构建互不影响）。

### 2.2 位移语义：绝对定位合成，绕开指针加速

CGEvent 鼠标事件显式携带目标位置。darwin 后端对 REL_X/REL_Y 的处理 =
`CGEventGetLocation()` 取当前位置 + delta 合成绝对坐标后 post。
**OS 加速曲线只作用于 HID 硬件相对事件，对绝对定位 mouseMoved 不生效** ——
Linux 侧需要 gsettings flat profile 的问题在 macOS 架构性消失，hostinfo 无需对应检查。
已知取舍：物理鼠标与注入并发时的合成公平性不如内核 REL 模型（极端场景丢一个 delta），
与 Barrier/Synergy 同款取舍，v1 接受；多显示器坐标系与屏幕边缘钳制行为列入 M-1 实测。

### 2.3 滚轮：原生像素单位

REL_WHEEL_HI_RES（1/120 格）→ CGEventCreateScrollWheelEvent(kCGScrollEventUnitPixel)。
macOS 原生支持像素级平滑滚动，体验上限高于 Linux 的 hi-res 转换；映射系数 M-1 实测校准。
自然滚动方向由系统按当前设备设置处理，App 侧 natural_scroll 开关语义保持一致（默认开）。

### 2.4 TCC 辅助功能权限首启流

`uinput.Open()`（darwin）内封装：`AXIsProcessTrusted()` 未授权 →
`AXIsProcessTrustedWithOptions(prompt=true)` 弹系统授权框 → 轮询等待授权（日志提示用户去
系统设置勾选）→ 授权后继续初始化；超时退出并给出手动指引。
授权对象是"责任进程"：LaunchAgent 直挂二进制时 TCC 按二进制路径记账，授权一次长期有效。
PoC 已验证：未授权时事件被静默丢弃，因此该门控不是可选优化而是正确性前提。

### 2.5 文本注入：pbcopy/pbpaste

textinject 的 wl-copy/wl-paste 命令名/参数按平台参数化（Runner 抽象已就绪）；
darwin 侧 pbcopy 写入即 public.utf8-plain-text，pbpaste 读出。
已知限制：CLI 只能备份纯文本剪贴板（图片/文件剪贴板备份不了），v1 接受并在日志声明；
后续如需要再走 quartz 包补 NSPasteboard 全保真备份。Cmd+V 由 main.go 按平台注入
（inject 接口不动）。

### 2.6 常驻与分发

- launchd LaunchAgent：`~/Library/LaunchAgents/com.zhimingcool.padlink.plist`
  （RunAtLoad+KeepAlive），packaging/macos/install.sh 负责安放+bootstrap，对齐 Linux install.sh 形态。
- 首次监听会触发 macOS 应用防火墙"接受传入连接"弹窗——install.sh 打印说明，
  或用 socketfilterfw 预登记（可选）。
- CI：release.yml 迁移 `macos-latest`（仓库公开，macOS runner 免费额度无倍率问题）：
  darwin/arm64 + darwin/amd64 本机 cgo 构建，linux 两架构继续纯 Go 交叉，nfpm 出 deb/rpm 不受影响。
- Gatekeeper：curl 下载不带 quarantine xattr 可直接跑；浏览器下载需 `xattr -d`（文档写明）。
  Apple Developer 公证（$99/年）留待 v2 后期，首版不做。

## 3. 阶段划分与验收标准

| 阶段 | 内容 | 验收（本机真机） |
| --- | --- | --- |
| M-macOS-1 | internal/quartz + uinput_darwin + KEY_*→CGKeyCode 表 + AX 首启流 | 本机授权后 `padlinkd --test` 注入自测：光标按脚本轨迹移动、滚轮/按键生效 |
| M-macOS-2 | textinject pbcopy 后端 + Cmd+V 平台化 | TextEdit 焦点下语音文本注入上屏，剪贴板按延迟恢复 |
| M-macOS-3 | hostinfo darwin 分支 + control socket 路径回落 + launchd 安装物 | install.sh 一键安装，重启会话后自启，padlinkctl 连通 |
| M-macOS-4 | CI 迁移 + 文档 + 端到端联调 | tag 构建出 darwin/linux 全部产物；模拟器手动 IP 连本机 Mac 触控板/键盘/语音全链路 |

模拟器网络注记：鸿蒙模拟器 NAT 后 UDP 广播发现可能不可达，联调走手动添加主机 IP
（宿主 LAN IP），TCP/UDP 会话面不受影响；真机 Wi-Fi 场景广播发现不受影响。

## 4. 影响范围与回归

- Linux 路径零改动（全部新代码走 darwin build tag / 平台分支），Linux 单测+probe 脚本照旧回归。
- 文档表述修正：daemon/README.md 与 PRD "无 cgo 单二进制" → "Linux 无 cgo；darwin 经 cgo 调 Quartz"（PRD §4.8 已预留该措辞）。
- 不动：鸿蒙 App、协议格式、配对/token 体系、Linux packaging。

## 5. 风险清单

1. AX 授权 UX：用户不授权则功能全灭——首启流+install.sh 文案+日志三处引导，M-1 实测打磨。
2. 绝对定位与物理输入并发公平性（§2.2 取舍）——M-1 实测确认可接受。
3. 滚轮像素映射系数手感——M-1/M-4 实测校准。
4. 未公证二进制的下载摩擦——文档兜底，v2 后期评估公证/ Homebrew tap。
