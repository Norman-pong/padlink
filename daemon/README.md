# padlink daemon（padlinkd）

PadLink 的桌面守护进程（Go，零第三方依赖单二进制；Linux 无 cgo，macOS 经薄 cgo 封装调 Quartz CGEvent，见 docs/PLAN-MACOS.md）。交付：协议编解码（黄金向量对拍）、注入编排、Linux uinput / macOS CGEvent 双注入后端与 `--test` 注入自测、网络层（发现应答 / 配对 / token 认证 / TCP 会话 / UDP 数据面）、wl-clipboard 文本注入、`padlinkctl` 控制通道与安装物。

## 模块划分

| 路径 | 职责 |
| --- | --- |
| `internal/proto` | 线协议 v1 编解码（19B 头 + payload，大端）、HMAC-SHA256 截断认证；以 `protocol/testvectors.json` 为对拍单源 |
| `internal/inject` | 平台无关注入编排：MOVE/SCROLL/BUTTON/KEY → 事件序列、修饰键组合（CtrlV）、daemon 侧 key repeat（250ms + 33ms）、HID usage → Linux KEY_* 映射 |
| `internal/uinput` | 注入后端（`DeviceWriter` 实现）：Linux uinput + macOS CGEvent（darwin）；`caps.go` 为跨平台可测的能力位/ABI 纯逻辑，其余平台由 stub 保证可编译 |
| `internal/quartz` | macOS Quartz CGEvent 薄 cgo 封装（仅 darwin 构建）：绝对定位位移/像素滚轮/键鼠 post 与 TCC 辅助功能权限检测 |
| `internal/pairing` | 4 位确认码生命周期（60s/最多 5 次/常数时间比较/同时最多一轮）、token 签发与 `clients.json`（0600）持久化、客户端上限 4、`notify-send` 展示 |
| `internal/session` | TCP 会话（HELLO 协商、配对路径、逐包 token 认证、心跳超时、速率限制、会话结束按键兜底）与 UDP 数据面（发现应答、MOVE/SCROLL/ECHO）；所有注入经单一 goroutine 串行化 |
| `internal/textinject` | TEXT → wl-clipboard 剪贴板备份/写入/CtrlV/延迟恢复（`--no-newline`、显式 `--type`、可配恢复延迟；缺失/超时结构化降级） |
| `internal/hostinfo` | 启动自检：WAYLAND_DISPLAY 缺失、gsettings accel-profile 非 flat（双重加速警告） |
| `internal/control` | `padlinkctl` ↔ daemon 的 Unix socket 控制通道（行分隔 JSON） |
| `cmd/padlinkd` | 守护进程入口（`--test` 自测 + 常驻模式） |
| `cmd/padlinkctl` | CLI：status / pair / clients / unpair |
| `cmd/padlinktoy` | 联调工具三件套：record/replay 合成录制回放、fuzz 协议变异模糊测试、latency ECHO RTT 压测（PRD §2.2 `tools/` 的 daemon 侧承载） |
| `packaging/` | 安装物与发版脚本（udev 规则、两种 user 级 service 变体、install.sh、deb/rpm 钩子脚本） |

## 构建与测试

```sh
cd daemon
go vet ./...          # 零告警
go test ./... -race   # 含黄金向量对拍（直读仓库根 protocol/testvectors.json）与回环网络单测
# 交叉编译两架构
GOOS=linux GOARCH=amd64 go build -o dist/bin/padlinkd-linux-amd64 ./cmd/padlinkd
GOOS=linux GOARCH=arm64 go build -o dist/bin/padlinkd-linux-arm64 ./cmd/padlinkd
GOOS=linux GOARCH=amd64 go build -o dist/bin/padlinkctl-linux-amd64 ./cmd/padlinkctl
GOOS=linux GOARCH=arm64 go build -o dist/bin/padlinkctl-linux-arm64 ./cmd/padlinkctl
GOOS=linux GOARCH=amd64 go build -o dist/bin/padlinktoy-linux-amd64 ./cmd/padlinktoy
GOOS=linux GOARCH=arm64 go build -o dist/bin/padlinktoy-linux-arm64 ./cmd/padlinktoy
```

## 安装（Ubuntu 26.04 / GNOME Wayland）

### 方式一：从 GitHub Releases 安装（推荐）

- **deb（Debian/Ubuntu）**：`sudo apt install ./padlink_<版本>_linux_amd64.deb`
  ——udev 规则与 user 单元随包安装，postinst 自动重载 udev。
- **rpm（Fedora/openSUSE）**：`sudo dnf install ./padlink_<版本>_linux_x86_64.rpm`。
- **tar.gz（其他发行版）**：解压后在包根目录执行 `./install.sh`
  （二进制装 `~/.local/bin`，单元装 `~/.config/systemd/user/`，仅 udev 规则一步要 sudo）。

包装好后**首次安装须注销并重新登录**（uaccess ACL 在图形会话建立时附加），然后：

```sh
systemctl --user enable --now padlink
journalctl --user -u padlink -f
```

### 方式二：源码构建手工安装

0. **构建**：见上节交叉编译命令（产物落 `dist/bin/`，本目录已被 gitignore）。

1. **uinput uaccess 规则**（无 root 运行的前提）：

   ```sh
   sudo cp daemon/packaging/udev/69-padlink-uinput.rules /etc/udev/rules.d/
   sudo udevadm control --reload && sudo udevadm trigger /dev/uinput
   # 注销并重新登录（uaccess ACL 在图形会话建立时附加）
   getfacl /dev/uinput | grep "$(id -un)"   # 期望 user:<你>:rw-
   ```

2. **指针加速基线**（GNOME 默认 adaptive 会与手机端曲线双重叠加）：

   ```sh
   gsettings set org.gnome.desktop.peripherals.mouse accel-profile 'flat'
   ```

   daemon 启动时自动检测并在非 flat 时警告；只提示、不代改。

3. **文本注入依赖**（语音听写用，缺失则该功能降级）：

   ```sh
   sudo apt install wl-clipboard
   ```

4. **放置二进制 + user service**：

   ```sh
   mkdir -p ~/.local/bin && cp dist/bin/padlinkd-linux-$(dpkg --print-architecture) ~/.local/bin/padlinkd
   cp dist/bin/padlinkctl-linux-$(dpkg --print-architecture) ~/.local/bin/padlinkctl
   mkdir -p ~/.config/systemd/user && cp packaging/systemd/padlink-local.service ~/.config/systemd/user/padlink.service
   systemctl --user daemon-reload && systemctl --user enable --now padlink
   journalctl --user -u padlink -f
   ```

## 运行与调试

```sh
padlinkd --test -v        # 无手机注入自测（调试分层第一层）
padlinkd -v               # 前台 verbose（逐包调试行）
```

flags：`--port`（默认 53021）、`-v`（verbose）、`--state-dir`（覆盖 token 存储目录）、`--restore-delay-ms`（恢复剪贴板延迟，默认 300）。日志走 stdout（systemd 接管进 journald）。SIGINT/SIGTERM 优雅退出：关全部会话（补发未释放按键）、控制 socket、uinput 设备。

## 协议 v1 约定（daemon 侧文档化；`protocol/` 为字节级单源，不改）

报文格式、类型枚举、HMAC 认证见 `protocol/PROTOCOL.md`。以下是 daemon 侧的 payload 约定：

| 报文 | 方向 | payload | 约定 |
| --- | --- | --- | --- |
| HELLO | C→S / S→C | 2B BE 版本号 | TCP **首包必须**未认证 HELLO；回应 `min(双方)`（当前 v1，降级仅告警） |
| HELLO | C→S | 同上 | UDP 收到任意 HELLO（含广播）即回 DISCOVER_RESP 单播 |
| DISCOVER_RESP | S→C（UDP 单播） | JSON | `{"name":<主机名>,"os":<runtime.GOOS>,"daemon":<版本>,"ver":1,"paired":<已配对数>,"port":53021}`；不认证、不含敏感信息 |
| PAIR_REQ（发起） | C→S | 0B 或 JSON `{"name":"..."}`（≤64B） | 发起配对；**成功不回包**（确认码在主机侧展示：notify-send + `padlinkctl pair`）；失败回 PAIR_NAK；JSON 非法/名字超长 → ERR(2)+关 |
| PAIR_REQ（尝试） | C→S | 4B ASCII 数字 | 校验确认码；其余 payload 形态 → ERR(2)+关 |
| PAIR_OK | S→C | 32B token | 封 FlagAuth（用刚签发 token，手机可端到端自证）；同时持久化 `clients.json` |
| PAIR_NAK | S→C | 1B reason | 0=码错 1=过期 2=超次作废 3=无进行中配对 4=客户端已满；不封签（此时无 token） |
| ERR | S→C | 1B code | 1=auth（未认证/连续 5 次错 HMAC）2=proto（会话层违规或坏 payload）3=rate（速率持续超限） |
| BYE | C→S | 空 | 优雅断开；daemon 补发本会话未释放的按键/按钮 up |

会话层规则：

- 握手后未认证包仅允许 PAIR_REQ，其余 → ERR(1)+关；FlagAuth 包逐个尝试已配对 token（≤4 个），命中即绑定会话↔客户端。
- FlagAuth 无匹配 token 或 HMAC 错：丢弃+计数，**连续 5 次** → ERR(1)+关。
- 认证后 TCP 入站出现 HELLO / DISCOVER_RESP → ERR(2)+关（它们属 UDP/握手语义）。
- 心跳：读 deadline **10s** 无任何包判失联断开（手机侧 1Hz ECHO 维持）；ECHO 原样回传（封同 token，回包 seq 自增）。
- 速率限制：每 TCP 会话令牌桶 **2000 包/s、桶 4000**；超限丢弃+计数，1s 窗口内超限丢弃 >2000 → ERR(3)+关。UDP 用同参数全局桶，超限静默丢弃。
- 注入串行化：TCP 多会话 + UDP 并发源收敛到单一注入 goroutine（channel），保证事件序与 DeviceWriter 无并发写。
- 会话结束（BYE/断开/超时/unpair）兜底补发本会话按下未释放的 KEY/BUTTON up（PRD §11.2 零残留的服务端兜底）。

## 控制通道（padlinkctl）

Unix socket：`$XDG_RUNTIME_DIR/padlinkd/control.sock`（`XDG_RUNTIME_DIR` 未设置时禁用并告警）。行分隔 JSON：

```jsonc
// 请求
{"cmd":"status"} {"cmd":"pair"} {"cmd":"clients"} {"cmd":"unpair","arg":"<客户端id>"}
// 响应
{"ok":true,"data":{...}} / {"ok":false,"error":"..."}
```

- status data：`version`、`uptime_sec`、`paired_clients`、`active_sessions`、`pairing{active,code,expires_in_sec}`、`stats{hmac_fail,dropped}`、`accel_profile`。
- pair：无进行中配对则生成新码（60s），返回 `{"code":"1234"}`；进行中复用同码（幂等）。
- clients data：`[{id,name,paired_at,online}]`。
- unpair：删 token 并踢下线（会话兜底补发按键）。

```sh
padlinkctl status
padlinkctl pair           # 终端显示确认码（与桌面通知同码）
padlinkctl clients
padlinkctl unpair 1a2b3c4d
```

## 注入自测（无手机，调试分层第一层）

```sh
# Linux 主机上（需 udev uaccess 已授权 /dev/uinput）
./dist/bin/padlinkd-linux-$(dpkg --print-architecture) --test -v
```

序列对齐 `docs/PROBE-LINUX.md` §4：光标画 2 个 r=200px 圆（各 120 段精确闭合）→ 左键点击 → 敲出 `pl`（走 HID→KEY 映射）→ hi-res 滚动 +2 格 / -1 格；成功打印 `padlinkd --test PASSED`（退出码 0），任何错误非零退出并输出原因。

滚动语义：`REL_WHEEL_HI_RES` 必发（单位 1/120 格），同帧按内核惯例补发 legacy `REL_WHEEL = dy/120`（向零取整、不跨帧累积）；GNOME/mutter 只消费 HI_RES 路径。详见 `docs/RESEARCH-WAYLAND.md` §1。

## 联调工具（padlinktoy）

PRD §2.2 的 `tools/`（录回放器、协议 fuzz、延迟压测）由 daemon 模块承载为单一二进制 `cmd/padlinktoy`：与 daemon 共用 `internal/proto` 协议实现（字节级单源不旁路）、零第三方依赖，Linux/macOS 均可构建运行，作为 PRD §7.3 联调分层（`--test` → echo/合成流量 → 真触摸）的中间层验证工具。

```sh
go -C daemon build -o dist/bin/padlinktoy ./cmd/padlinktoy   # 或 go -C daemon run ./cmd/padlinktoy <子命令> -h
```

token 一律经 `-token`（hex）或环境变量 `PADLINK_TOKEN` 提供；不写日志、不入录制文件。

### record — 合成录制 → .plrec

```sh
padlinktoy record -o demo.plrec                       # 默认序列：画圆 r=200/120 段 + 滚动 3 格 + 左右键单击 + 文本
padlinktoy record --circle 120 90 --scroll 2 --buttons --type "你好" --gap-ms 8 -o demo.plrec
padlinktoy record --circle 200 120 -token $PADLINK_TOKEN -o sealed.plrec   # 带 token：帧就地封签（sealed）
```

生成 seal 后的事件帧序列，按 daemon 约定分通道：MOVE/SCROLL → UDP 帧，BUTTON/KEY/TEXT → TCP 帧。圆周 MOVE 为相邻点差分（净位移恒为零，可作回放闭环断言）。

### replay — 回放 .plrec 到目标 daemon

```sh
padlinktoy replay -f demo.plrec -host 192.168.1.10 -token $PADLINK_TOKEN
padlinktoy replay -f sealed.plrec -host 192.168.1.10 --speed 2.0
```

流程：TCP 建连 → 未认证 HELLO → 按时间戳节奏发送（UDP 帧走 UDP 单播，TCP 帧走已 seal 的 TCP）→ 封签 BYE 优雅退出；打印各类型帧数 / 发送失败数 / ERR 收到数 / 耗时。raw 录制回放必须提供 token（发送前现场封签）；sealed 录制原样发送（token 仅用于封签 BYE，须与录制时相同）。

### fuzz — 协议变异模糊测试

```sh
padlinktoy fuzz -host 192.168.1.10 --seconds 10 --rate 500
padlinktoy fuzz -host 192.168.1.10 -token $PADLINK_TOKEN --seconds 30   # 叠加认证路径变异
```

变异策略：合法帧随机 1/2/4/8 位翻转、19B 头内任意截断、payload_len 放大/缩小、magic/ver/flags 篡改、未知 type、超长 payload、非法 UTF-8 TEXT、FlagAuth 随机置位 + 随机 HMAC；TCP 侧叠加粘包/半包（分片写）与超量注入，UDP 侧混入 HELLO（非回环目标附加广播 HELLO）。结束后连发 3 个 ECHO（有 token 则 seal）做活性判定：ECHO 回包 / ERR 响应 / 无响应分别报告，无响应以非零退出码区分。

断言口径：fuzz 不得使 daemon panic/挂死（活性判定覆盖）；daemon 侧丢弃计数无法从外部读取，以活性 + 响应行为为准。

### latency — ECHO RTT 压测

```sh
padlinktoy latency -host 192.168.1.10 -token $PADLINK_TOKEN --hz 50 --duration 10s
padlinktoy latency -host 192.168.1.10 -token $PADLINK_TOKEN --load --json
```

TCP 封签 ECHO 按 `--hz` 发送并等回包算 RTT，输出 min/P50/P90/P95/P99/max 与丢包数；`--load` 叠加 1000/s UDP MOVE 背景负载测 RTT 退化；`--json` 输出机器可读结果。**口径**：此为**传输层 RTT** 分量；触摸→光标端到端延迟按 PRD §8 需真机录屏逐帧口径测量。

### .plrec 文件格式

自描述、不含 token（帧内容为 `protocol/` 线协议帧）：

```text
行 1（ASCII）: PLREC <版本> <raw|sealed>\n   # raw=帧未封签（回放需 token 现场封签）；sealed=帧已封签（原样发送）
每帧: rel_ms:8B BE | chan:2B BE（0=UDP, 1=TCP） | len:2B BE | frame:len B   # len ≤ 19+1400
```

## 安全约定

- token 存 `clients.json`，写入时 O_CREATE|O_TRUNC|O_WRONLY 后显式 Chmod **0600**（防宽松 umask）；加载时权限宽于 0600 打警告。结构带版本号字段便于迁移。
- 确认码常数时间比较；token 查找同样常数时间。
- token 与剪贴板内容（备份/注入文本）一律不进日志；日志只记长度与计数。
- DISCOVER_RESP 不含敏感信息；配对发起成功不回包，确认码只出现在主机侧。
- 文本注入备份仅驻内存（不落盘）。

真机验证步骤（udev 规则、evtest 取证、libinput 确认）见 `docs/PROBE-LINUX.md`。
