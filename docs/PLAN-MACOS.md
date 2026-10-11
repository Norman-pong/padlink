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

**合成事件的两处必写字段（M-macOS-5 补全，Apple CGEventField 文档 + 社区实证）**：

- 按钮事件必须显式写 `kCGMouseEventClickState`（1=单击 / 2=双击 / 3=三击）；
  不写时应用侧 `NSEvent.clickCount` 恒为 1 —— 双击打开文件、三击选段全部失效。
  点击序号由 `internal/inject/click.go`（同键、限时、限距的连续按下）合成，
  间隔取系统设置 `NSEvent.doubleClickInterval`（`internal/quartz/interval_darwin.m`，
  实测非 GUI 进程可直接取值，默认 0.5s）。
- 按住按钮期间的位移必须投 `kCGEventLeftMouseDragged`（右/中键对应 Right/OtherMouseDragged），
  否则 AppKit 的 `mouseDragged:` 不触发 —— 拖文件、拖窗口、选词拖拽失效。
  `MouseMove.ButtonsDown`（由 inject 层多击合成器给出）决定事件类型。
- Linux 侧无对应改动：evdev 事件自带内核时间戳，多击由工具包/合成器按时序自行判定，
  拖拽由「BTN 按住 + REL 位移」天然表达。

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

### 2.4 TCC 辅助功能权限首启流与稳定签名身份

`uinput.Open()`（darwin）内封装：`AXIsProcessTrusted()` 未授权 →
`AXIsProcessTrustedWithOptions(prompt=true)` 弹系统授权框 → 轮询等待授权（日志提示用户去
系统设置勾选）→ 授权后继续初始化。轮询**不设超时**：launchd 场景下超时会导致退出/重启反复
弹窗；前台运行可由用户 Ctrl+C 中断。
PoC 已验证：未授权时事件被静默丢弃，因此该门控不是可选优化而是正确性前提。

**问题（2026-10-11 实测）**：TCC 对"责任进程"的记账粒度取决于代码签名。Go 产出的二进制默认是
ad-hoc（linker-signed）签名，TCC 记的是 **cdhash**（代码哈希）——每次重新构建 cdhash 都变，
**替换二进制后授权立即失效**（实测：换掉同路径二进制后 daemon 停在「仍在等待辅助功能授权」
轮询上；按"按路径记账"的旧认知去重新勾选列表里的同名条目同样无效，那是旧 cdhash 的残留条目）。

**解决：本机稳定签名身份**（`packaging/macos/signing-identity.sh`，install.sh 自动调用）。
首次安装时在本机生成一张自签代码签名证书（**不碰登录钥匙串、不需要管理员、不需要信任设置、
不联网**），此后每次安装/更新都用它签名，二进制的授权要求变成：

```
identifier "com.zhimingcool.padlink" and certificate leaf = H"<证书 SHA-1>"
```

与代码内容无关 ⇒ **辅助功能只需授权一次，后续更新二进制不再重新授权**。

关键实现细节（均有实测依据）：

| 细节 | 结论 |
| --- | --- |
| 自签证书是否需要"信任" | **不需要**。`codesign -s <证书 SHA-1>` 按哈希选身份即可签名；`codesign --verify` 报 `valid on disk` + `satisfies its Designated Requirement`，未信任状态（CSSMERR_TP_NOT_TRUSTED）不影响签名与要求求值 |
| 幂等判定不能只看 `find-identity -v -p codesigning` | 未信任身份被判 invalid → 会误判"身份缺失"而重复创建同名证书，随后 `-s <名字>` 报 ambiguous。改为 `find-certificate -c <CN> -Z` 取证书 SHA-1，一律按哈希签名 |
| "更新免授权"的可验证判据 | `signing-identity.sh check <旧二进制> <新二进制>`：读旧二进制的指定要求，用 `codesign --verify -R "=<要求>"` 校验新二进制。实测两个不同构建（cdhash 各异、授权要求逐字节相同）→ 正向 exit 0；反向对照（换一个证书哈希）exit 3 失败 |
| 签名后二进制能否正常运行 | 能。`install -m 0755` + 就地签名保留权限，launchd 正常拉起 |
| 身份文件位置与生命周期 | 钥匙串 `~/Library/Keychains/padlink-signing.keychain-db` + 口令 `~/.config/padlink/signing-keychain.pass`（0600）。二者跨更新保留 ⇒ 一次授权长期有效；被删除或换机则需重新授权一次 |
| 回滚 | `signing-identity.sh remove` 删除本机身份（不影响已安装二进制，只是后续更新会重新要求授权） |

首次授权步骤（install.sh 结尾会打印）：系统设置 → 隐私与安全性 → 辅助功能 → 若列表里有旧的
padlinkd 条目先按 `−` 删除 → 用 `+` 按路径添加 `~/.local/bin/padlinkd` → 勾选。daemon 在轮询
授权状态，授权后自动继续，无需重启。

**发版说明**：darwin 归档需带 `packaging/macos/signing-identity.sh`（.goreleaser.yaml 已加），
安装时在**用户机器上**生成/复用本机身份并签名 ⇒ 终端用户同样是"授权一次，更新免授权"。若将来
要连首次授权弹窗都免掉（并让 Gatekeeper/公证通过），再换 Apple Developer ID 证书（见 §2.6 v2 项）。

### 2.5 文本注入：pbcopy/pbpaste

textinject 的 wl-copy/wl-paste 命令名/参数按平台参数化（Runner 抽象已就绪）；
darwin 侧 pbcopy 写入即 public.utf8-plain-text，pbpaste 读出。Cmd+V 由 main.go 按平台注入
（inject 接口不动）。

**两端共同契约**（2026-10-11 真机对齐，textinject 包注释同步声明，改动须同时满足）：

> 能备份到内容 → 注入后精确恢复；备份不到内容（Linux：读取失败；macOS：粘贴板只有非文本）→
> 取消本次注入并给出可见错误。任何情况下都不把注入文本留在剪贴板，也不覆盖备份不到的原内容。

真机实测（macOS 26；`pbpaste` 对"真·清空 / 只有图片 / 空串文本"三种状态一律退 0 且输出空串，
无法自行区分，故取空时补查类型表）：

| 粘贴板状态 | `pbpaste` | `osascript -e 'clipboard info'` | daemon 行为 |
| --- | --- | --- | --- |
| 有文本 | 文本 | 含 `«class utf8»`/`string`/`Unicode text` | 备份文本 → 注入 → 写回文本 |
| 真·清空 | 空 | （无输出） | 照常注入 → 收尾写空 |
| 空串文本 | 空 | 有文本类型、长度为 0 | 照常注入 → 收尾写空 |
| 只有非文本（图片/文件） | 空 | 只有 `TIFF picture`/`«class PNGf»` 等 | **取消注入**（`errNonText`），粘贴板不动 |

`osascript` 为系统自带、纯本地粘贴板查询，不需要辅助功能/自动化授权，且只在 `pbpaste` 取空的
少数路径上调用；类型表查询失败按备份失败降级，同样不写剪贴板。

**真机验收（2026-10-11，macOS 26 / arm64）**：测试实例 `padlinkd -v -port 53099
-restore-delay-ms 4000`，粘贴落点用 TextEdit 打开 `/tmp/mac-accept.txt`（避免把注入文本粘进用户
正在用的窗口）：

| 场景 | 操作 | 结果 |
| --- | --- | --- |
| A 文本恢复 | 粘贴板置 `baseline-mac-clean-*` → 注入 1 条 TEXT → 等 6s | 内容字节不变，RESTORE OK，失败行 0 |
| B 空粘贴板 | `clearContents` → 注入 → 1.5s 窗口内读一次 | 窗口内读到注入文本；收尾后 `pbpaste` 为空、类型表为 0 长文本，失败行 0 |
| C 只有图片 | 写入 NSImage → 注入 → 等 6s | 类型表前后一致（`TIFF picture`/`«class AVIF»`/`«class 8BPS»`… 仍在），恰好新增 1 条失败行：`注入 TEXT 失败: 剪贴板内容不是文本（本次注入已取消，剪贴板未改动）` |
| D 实际上屏 | A/B 两次注入落到 TextEdit | 落盘文件内容为 `空粘贴板注入验收乙macOS 注入上屏验收甲`（daemon → pbcopy → Cmd+V → TextEdit 自动保存） |

测量时机必须晚于恢复延迟（本例 4s）：早期两次「RESTORE FAIL / 图片被破坏」的判定，都是在注入
窗口内读剪贴板得到的中间态（其中一次还叠加了用户同期复制内容），不是缺陷。

**与 Linux 端的差异（同为"不破坏剪贴板"，结果因能力不同而不同）**：Linux 侧 `wl-paste` 通常
能按字节读到非文本内容（真机实测 `image/png` 193 字节原样往返、类型不变），因此**注入照常进行**
且剪贴板保真；只有读取确实失败（`inferred output type` 报错）时才取消注入。macOS 的 CLI 只有
纯文本面（`pbpaste` 对图片一律输出空串），无法字节级备份，所以**只能取消注入**——两端都不覆盖
备份不到的内容，差别仅在"Linux 能继续注入、macOS 拒绝"。若将来要让 macOS 也支持非文本剪贴板，
需走 quartz 包补 NSPasteboard 全保真备份（届时两侧行为可再对齐）。

已知限制（v1 接受，日志声明）：CLI 只有纯文本面，恢复只写回纯文本，富文本类型（RTF/HTML/图片）
不保真——粘贴板含文本时文本内容仍能备份/恢复，只有非文本时取消注入。`pbcopy` 空输入用于收尾：
macOS 无 `wl-copy --clear` 对应的 CLI，对粘贴方等价于"粘不出内容"，但类型表仍留有文本类型。

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
| M-macOS-2 | textinject pbcopy 后端 + Cmd+V 平台化 | TextEdit 焦点下语音文本注入上屏，剪贴板按延迟恢复；图片型剪贴板下取消注入且粘贴板内容不变（两端契约，见 §2.5） |
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
