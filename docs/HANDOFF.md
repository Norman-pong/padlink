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

| 阶段 | 交付 | 通过标准 |
| --- | --- | --- |
| M0 | daemon 从零实现：协议层（对拍 testvectors）+ uinput 注入 + `--test` + udev/systemd 安装物 | `go test ./...` 全过；PROBE-LINUX.md 光标画圆通过 |
| M1 | 鸿蒙工程（`devecocli create --app-name padlink --api-level 26`）+ 触摸板链路 | 光标跟手；§11.1 手势达标；真机多指 id 恒定 |
| M2 | 发现 / 配对 / token / 重连 | 断连 30s 自动恢复；错误确认码 5 次锁定 |
| M3 | 键盘区（HID usage 映射） | Ctrl+C/V、Super、Alt+Tab、中文经 fcitx 可用 |
| M4 | 语音听写 | §11.4：50 字 2 次内上屏，剪贴板内容不变 |
| M5 | 调优收口 | 延迟 P50≤40ms / P95≤80ms；`devecocli build` 通过 |

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
