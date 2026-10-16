# Windows 移植方案与实测（PRD 附录 B 平台扩展）

依据：PRD §1.3 平台矩阵、附录 B 平台扩展战略；对标本文件姊妹篇 `PLAN-MACOS.md`。
动机：padlinkd 三层（协议/会话/注入编排）按「换平台只换 DeviceWriter 后端」设计，
Windows 是纯 daemon 侧新后端 + 安装/发版链路扩展，协议与手机端零改动。

## 已定决策（2026-10-16，与维护者逐项确认）

| # | 决策点 | 定案 | 理由 |
| --- | --- | --- | --- |
| 1 | Win32 API 访问 | **纯 syscall**（`NewLazyDLL`，无 cgo、零第三方依赖） | API 面仅约 10 个；守住 go.mod 零依赖与「linux 构建 CGO_ENABLED=0」现状 |
| 2 | 系统基线 | **仅 Win11** | 无 32 位版本，测试矩阵最小；AF_UNIX 全版本可用 |
| 3 | CPU 架构 | **amd64 + arm64 双包首发** | 架构差异由 Go 编译器抹平，同一份源码零改动；arm64 若无实机标「构建支持、未实测」 |
| 4 | 服务形态 | **HKCU `Run` 注册表键** | 用户级免管理员，装卸简单；不做 SCM 真服务（Session 0 隔离与交互注入冲突） |
| 5 | 测试环境 | **Win11 实机** | M0 尖峰与 M4 端到端均在实机执行 |

## 架构映射（10 个耦合点）

| # | 耦合点 | 方案 | 状态 |
| --- | --- | --- | --- |
| 1 | 指针移动 | SendInput 绝对定位（`ABSOLUTE`，0–65535 归一化），不经相对弹道 | 尖峰 A 待实测 |
| 2 | 按键 | SendInput 扫描码（`KEYEVENTF_SCANCODE`，扩展键补位），HID→scancode 新映射表 | M1 |
| 3 | CapsLock 补偿 | `Config.CapsLockActive` 钩子接 `GetKeyState(VK_CAPITAL)`（沿用既有 XOR 机制，不改写主机状态） | M1 |
| 4 | 滚轮 | `MOUSEEVENTF_WHEEL` ±120/格，HI_RES 按 1/120 累积到整格再发（**已知限制：无 hi-res 注入通道**） | M1 |
| 5 | 多击/拖拽 | 视尖峰 D 结果定：系统已合成 DBLCLK 则 `SetPointerState` 近乎空实现 | 尖峰 D 待实测 |
| 6 | 文本上屏 | Win32 剪贴板（CF_UNICODETEXT，RtlMoveMemory 读写模式）→ Ctrl+V → 延迟恢复（复用 textinject 既有契约） | 尖峰 C 定延迟默认值 |
| 7 | 配对弹窗 | `_pairing-dialog` 自旋子命令平移：TaskDialogIndirect（可挂定时器做倒计时）→ MessageBoxW+后台线程 WM_CLOSE → 终端 | M2（comctl32 v6 激活上下文细节届时尖峰） |
| 8 | 服务化 | HKCU Run 键 + 隐藏窗口进程 + stdout 重定向 `%LOCALAPPDATA%\padlink\logs\padlinkd.log` | M3 |
| 9 | 防火墙 | 安装脚本一次性 `netsh advfirewall` 放行 UDP/TCP 53021（管理员一步，同 Linux udev 模式） | M3 |
| 10 | 控制通道 | `net "unix"`（AF_UNIX，Win11 原生），路径 `%TEMP%\padlinkd\control.sock` | 尖峰 B 待实测 |

## M0 尖峰（`daemon/cmd/winspike`，实机一次性出数）

上机方法：下载 `winspike_<arch>.exe` → PowerShell 执行 `.\winspike.exe`（四测连跑约 30 秒，
期间屏幕上会自己画圆/弹测试窗，不要动鼠标）；剪贴板粘贴手动验证另跑 `.\winspike.exe -paste`
（先开记事本置前台）。输出整体回填下表。

| 尖峰 | 问题 | 方法 | 结果（待实机回填） |
| --- | --- | --- | --- |
| A | 绝对定位是否绕开「提高指针精确度」 | SPI_SETMOUSE 三种加速配置各画一个 r=220px/96 段圆，采样实际落点比最大半径偏差与收尾误差 | TBD |
| B | AF_UNIX 控制通道可用性 | `net unix` bind/accept/dial 往返 + 逐级加深路径探长度上限 + close 后文件残留检查 | TBD |
| C | 剪贴板时序 | C1 写读往返 20 轮计时；C2 手动：置串 → Ctrl+V → 默认 800ms 后恢复，肉眼核对上屏与还原 | TBD |
| D | 系统双击合成 | CS_DBLCLKS 测试窗 + SendInput 快速双击/慢速单击，数 DOWN/UP/DBLCLK 事件序列 | TBD |

## 里程碑

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| M0+M1 | 尖峰四测 → 本文件定稿 → `uinput_windows.go`（SendInput 后端）+ CapsLock 钩子 + 滚轮累积 | 实机 `padlinkd --test` 画圆/点击/敲键/滚动 PASSED；三平台 `go test ./...` 全绿 |
| M2 | textinject Windows 后端；`notify_windows.go`（TaskDialog/MessageBox/终端三层，复用 `dialogTexts`） | 实机语音上屏中文；配对弹窗倒计时 + 中英文案 |
| M3 | `install.ps1` + 一行安装（`irm … \| iex`）；Run 键 + 日志；`service` 增 windows 形态；`update` 支持 zip/自替换；goreleaser windows amd64+arm64 | 干净 Win11：一行安装 → 防火墙 → 自启 → `padlinkctl update` 升级闭环 |
| M4 | 手机 ↔ Win11 端到端：配对/触摸板/键盘/语音/换网自愈 | SETUP 排查表、PRD §1.3 平台矩阵、README 平台声明更新 |

## 已知限制（登记不修）

- **提权窗口不可注入**（UIPI）：非管理员 daemon 无法输入「以管理员身份运行」的窗口。
- **游戏 raw input**：走 Raw Input 的程序可能不响应 SendInput。
- **滚动粒度**：整格（120），无 hi-res。
- **SmartScreen/杀软**：未签名 exe + 键鼠注入行为，首启「更多信息→仍要运行」；代码签名证书为远期可选项。
