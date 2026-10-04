# padlink daemon（padlinkd）

PadLink 的 Linux 守护进程（Go，无 cgo、零第三方依赖单二进制）。本阶段交付：协议编解码（黄金向量对拍）、注入编排、uinput 后端与 `--test` 注入自测；网络会话层由后续里程碑接入。

## 模块划分

| 路径 | 职责 |
| --- | --- |
| `internal/proto` | 线协议 v1 编解码（19B 头 + payload，大端）、HMAC-SHA256 截断认证；以 `protocol/testvectors.json` 为对拍单源 |
| `internal/inject` | 平台无关注入编排：MOVE/SCROLL/BUTTON/KEY → 事件序列、修饰键组合（CtrlV）、daemon 侧 key repeat（250ms + 33ms）、HID usage → Linux KEY_* 映射 |
| `internal/uinput` | Linux uinput 后端（`DeviceWriter` 实现）；`caps.go` 为跨平台可测的能力位/ABI 纯逻辑，`uinput_linux.go` 为 `//go:build linux` 实现，非 Linux 平台由 stub 保证可编译 |
| `cmd/padlinkd` | 守护进程入口，本阶段仅 `--test` |

会话/发现/配对/文本注入属后续里程碑，尚未实现（不建占位目录）。

## 构建与测试

```sh
cd daemon
go vet ./...          # 零告警
go test ./...         # 含黄金向量对拍（直接读仓库根 protocol/testvectors.json，无副本）
# 交叉编译两架构
GOOS=linux GOARCH=amd64 go build -o dist/bin/padlinkd-linux-amd64 ./cmd/padlinkd
GOOS=linux GOARCH=arm64 go build -o dist/bin/padlinkd-linux-arm64 ./cmd/padlinkd
```

## 注入自测（无手机，调试分层第一层）

```sh
# Linux 主机上（需 udev uaccess 已授权 /dev/uinput，见 PROBE-LINUX.md §2）
./dist/bin/padlinkd-linux-$(dpkg --print-architecture) --test -v
```

序列对齐 `docs/PROBE-LINUX.md` §4：光标画 2 个 r=200px 圆（各 120 段精确闭合）→ 左键点击 → 敲出 `pl`（走 HID→KEY 映射）→ hi-res 滚动 +2 格 / -1 格；`-v` 追加 verbose 行；成功打印 `padlinkd --test PASSED`（退出码 0），任何错误非零退出并输出原因。非 `--test` 模式当前仅打印占位提示（网络会话层 M0-B 接入）。

滚动语义：`REL_WHEEL_HI_RES` 必发（单位 1/120 格），同帧按内核惯例补发 legacy `REL_WHEEL = dy/120`（向零取整、不跨帧累积）；GNOME/mutter 只消费 HI_RES 路径。详见 `docs/RESEARCH-WAYLAND.md` §1。

真机验证步骤（udev 规则、evtest 取证、libinput 确认）见 `docs/PROBE-LINUX.md`。
