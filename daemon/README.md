# padlink daemon（padlinkd）

PadLink 的桌面守护进程：Go 单二进制，零第三方依赖（macOS 经薄 cgo 封装调 Quartz CGEvent）。

> 使用者向的「装服务端 → 手机配对 → 排查」见 [docs/SETUP.md](../docs/SETUP.md)；协议字节语义见 [protocol/PROTOCOL.md](../protocol/PROTOCOL.md)。

## 模块划分

| 路径 | 职责 |
| --- | --- |
| `internal/proto` | 线协议 v1 编解码（19B 头 + payload，大端）、HMAC-SHA256 截断认证；以 `protocol/testvectors.json` 为对拍单源 |
| `internal/uinput` | 注入后端：Linux uinput / macOS CGEvent；`caps.go` 为跨平台可测的能力位与 ABI 纯逻辑 |
| `internal/quartz` | macOS Quartz CGEvent 薄 cgo 封装（仅 darwin）：绝对定位位移、像素滚轮、键鼠 post、TCC 权限检测 |
| `internal/inject` | 平台无关注入编排：事件序列、修饰键组合、key repeat、HID usage → KEY_* 映射、多击点击序号合成、CapsLock 补偿 |
| `internal/textinject` | TEXT → 剪贴板备份/写入/粘贴/延迟恢复（Linux wl-clipboard、macOS pbcopy/pbpaste）；缺失或超时结构化降级 |
| `internal/session` | TCP 会话（协商/认证/心跳/限速）、UDP 数据面、多设备控制权裁决；所有注入经单一 goroutine 串行化 |
| `internal/pairing` | 确认码生命周期、token 签发与 `clients.json`（0600）持久化、客户端上限、同设备指纹复用 |
| `internal/service` | `padlinkctl` 的启停与卸载编排：安装形态探测 → 有序 Plan → 执行层（路径守卫） |
| `internal/control` | `padlinkctl` ↔ daemon 的 Unix socket 控制通道（行分隔 JSON） |
| `internal/hostinfo` | 启动自检：WAYLAND_DISPLAY 缺失、GNOME 指针加速非 flat |
| `cmd/padlinkd` | 守护进程入口（常驻模式 / `--test` 自测） |
| `cmd/padlinkctl` | CLI：status / pair / clients / unpair + start / stop / restart / uninstall |
| `cmd/padlinktoy` | 联调工具：record / replay / fuzz / latency |
| `packaging/` | 安装物与发版脚本（Linux：udev 规则、两种 user 级 service 变体、install.sh、deb/rpm 钩子；macOS：launchd plist 与 install.sh） |

## 构建与测试

```sh
cd daemon
go vet ./...          # 零告警
go test ./... -race   # 含黄金向量对拍（直读仓库根 protocol/testvectors.json）与回环网络单测

# 交叉编译（产物落 dist/bin/）
GOOS=linux GOARCH=amd64 go build -o dist/bin/padlinkd-linux-amd64 ./cmd/padlinkd
```

## 安装

一行命令（Ubuntu / macOS，推荐）：

```sh
curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.sh | sh
```

其余安装方式（deb/rpm、tar.gz、源码构建）与各平台收尾步骤见 [docs/SETUP.md](../docs/SETUP.md)。

## 运行与调试

```sh
padlinkd --test -v        # 无手机注入自测
padlinkd -v               # 前台 verbose（逐包调试行）
```

flags：`--port`（默认 53021）、`-v`、`--state-dir`（覆盖 token 存储目录）、`--restore-delay-ms`、`--version`。
日志走 stdout（systemd 接管进 journald）。SIGINT/SIGTERM 优雅退出：关全部会话（补发未释放按键）、控制 socket、uinput 设备。

### 注入自测序列

画 2 个 r=200px 圆（各 120 段精确闭合）→ 左键点击 → 敲出 `pl`（走 HID→KEY 映射）→ hi-res 滚动 +2 格 / -1 格；
成功打印 `padlinkd --test PASSED`（退出码 0）。

滚动语义：`REL_WHEEL_HI_RES` 必发（单位 1/120 格），同帧按内核惯例补发 legacy `REL_WHEEL = dy/120`（向零取整、不跨帧累积）。

### 两条不可回退的平台约束

- **CapsLock 补偿**：主机大写锁会对注入的字母键再反转一次，`internal/inject` 按「手机意图 XOR 主机大写锁」改写 Shift 语义，
  判定走 `Config.CapsLockActive`（Linux 读 `/sys/class/leds/input*::capslock/brightness`，读不到按未生效处理）。
  补偿失败只退化成主机侧反转，且不得残留按住的修饰键；macOS 恒 false（合成 CGEvent 带显式 flags，不经系统大写锁改写）。
- **文本注入恢复延迟**：必须够目标应用读完粘贴板——macOS 默认 800ms、Linux 300ms，**改小前必须真机复测**。
  macOS 还须兜底 UTF-8 locale（launchd 环境无 locale 时写 CJK 得到空内容，备份非 ASCII 剪贴板会读成空），
  由 `cmd/padlinkd` 的 `ensureUTF8Locale()` 处理，已有 UTF-8 的 `LC_ALL/LC_CTYPE/LANG` 不动。

## 控制通道（padlinkctl）

Unix socket：Linux `$XDG_RUNTIME_DIR/padlinkd/control.sock`，macOS 回落 `$TMPDIR`；运行目录不可用时禁用并告警。行分隔 JSON：

```jsonc
// 请求
{"cmd":"status"} {"cmd":"pair"} {"cmd":"clients"} {"cmd":"unpair","arg":"<客户端id>"}
// 响应
{"ok":true,"data":{...}} / {"ok":false,"error":"..."}
```

```sh
padlinkctl status           # 版本 / 运行时长 / 会话数 / stats / 配对状态 / 加速档
padlinkctl pair             # 生成并显示确认码（与桌面通知同码；进行中幂等）
padlinkctl clients          # [{id,name,dev_id,paired_at,online}]，指纹列可看出「同一台手机重配」
padlinkctl unpair 1a2b3c4d  # 删 token 并踢下线（会话兜底补发按键）
```

服务启停与卸载不经控制 socket，padlinkd 未运行时也可用：

```sh
padlinkctl start / stop / restart
padlinkctl uninstall                     # 停服 + 移除服务定义与用户级二进制（保留 token）
padlinkctl uninstall --purge --yes       # 连配置/token/日志一起删（--purge 需二次确认，--yes 跳过）
padlinkctl uninstall --dry-run           # 只打印将执行的命令与将删除的路径
```

- 安装形态探测：Linux tar.gz（`~/.local/bin` + `~/.config/systemd/user/`）、Linux deb/rpm（`/usr/bin` + `/usr/lib/systemd/user/`）、macOS LaunchAgent。
- deb/rpm 形态不删 `/usr` 下文件，只提示 `sudo apt remove padlink` / `sudo dnf remove padlink`；
  udev 规则需 root，只打印可直接复制的 `sudo rm -f …`。
- 删除前双重校验路径（构建期 + 执行期），只 `os.Remove` 单个已知路径，不用 `RemoveAll`。

### 会话层规则（`PROTOCOL.md` 之外的 daemon 行为）

- 未认证包仅允许 PAIR_REQ，其余 → ERR(1)+关；FlagAuth 包逐个尝试已配对 token（≤4），命中即绑定会话。
- HMAC 连续 5 次失败 → ERR(1)+关；TCP 读 deadline 10s 无任何包判失联（手机侧 1Hz ECHO 维持）。
- 速率限制：每 TCP 会话令牌桶 2000 包/s、桶 4000，超限丢弃计数，1s 内丢弃 >2000 → ERR(3)+关；
  UDP 全局桶同参数先拦未认证洪泛，已认证流量退还全局令牌后改记同参数的按客户端桶。
- TEXT（语音/键盘文本）在注入 goroutine 内同步执行剪贴板往返：最坏 ~13s（4 个子进程各 3s 超时 + 恢复延迟），
  期间键鼠事件在注入队列排队、UDP MOVE 溢出即丢——文本注入期间指针短暂卡顿属预期行为。
- 会话结束（BYE/断开/超时/unpair）兜底补发本会话按下未释放的 KEY/BUTTON up。
- 同设备识别：`dev` 指纹命中既有客户端 → 复用该记录并只轮换 token（不占客户端名额）。

## 联调工具（padlinktoy）

与 daemon 共用 `internal/proto`（字节级单源不旁路）。token 经 `-token`（hex）或 `PADLINK_TOKEN` 提供；不写日志、不入录制文件。

```sh
go -C daemon run ./cmd/padlinktoy <子命令> -h
```

| 子命令 | 用途 |
| --- | --- |
| `record` | 合成事件序列 → `.plrec`（`--circle` / `--scroll` / `--buttons` / `--type`；带 token 则帧就地封签） |
| `replay` | 按时间戳回放 `.plrec` 到目标 daemon，打印各类型帧数 / 失败数 / ERR / 耗时 |
| `fuzz` | 协议变异模糊测试（位翻转、截断、长度篡改、未知 type、粘包半包），结束后连发 ECHO 做活性判定 |
| `latency` | 封签 ECHO 压测 RTT（min/P50/P90/P95/P99/max 与丢包）；`--load` 叠加 UDP MOVE 负载，`--json` 机器可读 |

`.plrec` 格式（自描述、不含 token）：

```text
行 1（ASCII）: PLREC <版本> <raw|sealed>   # raw 回放需 token 现场封签；sealed 原样发送
每帧: rel_ms:8B BE | chan:2B BE（0=UDP, 1=TCP） | len:2B BE | frame:len B   # len ≤ 19+1400
```

**latency 口径**：输出的是传输层 RTT 分量；触摸→光标端到端延迟需真机录屏逐帧测量。

## 安全约定

- token 存 `clients.json`，写入后显式 Chmod **0600**（防宽松 umask）；加载时权限宽于 0600 打警告。
- 主机身份存 `host_id`（与 `clients.json` 同目录、同权限）：16B 随机值，随 DISCOVER_RESP 明文广播，
  仅作手机端「已配对主机换地址」的候选提示——未认证通道可被冒用，地址迁移以 token 封签报文验签通过为准（PROTOCOL.md §4.10）。
- 确认码与 token 查找均用常数时间比较。
- token 与剪贴板内容（备份/注入文本）一律不进日志，日志只记长度与计数。
- DISCOVER_RESP 不含敏感信息；配对发起成功不回包，确认码只出现在主机侧；文本注入备份仅驻内存。
