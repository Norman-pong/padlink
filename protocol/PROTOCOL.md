# PadLink 线协议规范（PROTOCOL）

> 协议版本：v1（报文 ver 字段 = 1）
> 冲突裁决顺序（PRD §0.1）：**黄金向量 > 本文**。
> - 黄金向量：`protocol/testvectors.json`（正/负用例，含 HMAC MOVE、CJK TEXT）
> 两端（Go daemon 与 ArkTS APP）的编解码实现**必须**先通过 testvectors.json 对拍单测，通过后方可联调。
>
> 变更流程（PRD §0.2 铁律 2）：改本规范 → 升 ver → 重生成 testvectors.json → 双端对拍通过 → 再动两端实现。跳步即回退。

## 1. 通道与端口

默认端口 **53021**（可配置，发现报文需用默认端口才能互通）。

| 通道 | 事件类型 | 说明 |
| --- | --- | --- |
| UDP 广播（发现） | DISCOVER_RESP / PAIR_REQ / PAIR_OK / PAIR_NAK | 255.255.255.255 或子网定向广播 |
| UDP 单播 | **MOVE / SCROLL** / ECHO | 高频可丢：丢包由新值自然覆盖，不做重传 |
| TCP | **BUTTON / KEY / TEXT** / PAIR_* / HELLO / ECHO / BYE / ERR / NOTICE | 不可丢事件：点击、按键、文本错乱即事故，必须可靠传输 |

NOTICE 是唯一的 **daemon → 手机** 方向事件（控制权裁决提示，见 §4.7），其余事件均为手机 → daemon。

## 2. 报文 = 固定 19 字节头 + payload

多字节整数一律**大端序**（网络字节序）。UDP payload ≤ 1400 字节（分片安全上限）。

### 2.1 头布局（19B）

| 偏移 | 长度 | 字段 | 说明 |
| --- | --- | --- | --- |
| 0 | 2 | magic | 恒 `'P''L'`（0x50 0x4C） |
| 2 | 1 | ver | 协议版本，本版 = 1 |
| 3 | 1 | type | 事件类型，见 §3 |
| 4 | 1 | flags | bit0 (0x01) = FlagAuth：hmac_trunc 有效 |
| 5 | 2 | seq | 发送方递增序号，16 位自然回绕（丢包/乱序统计用） |
| 7 | 2 | payload_len | payload 字节数，≤ 1400 |
| 9 | 2 | reserved | 保留，发送置 0，接收不校验 |
| 11 | 8 | hmac_trunc | HMAC-SHA256 截断 8 字节，见 §5；未认证时全 0 |

### 2.2 校验顺序（接收方）

长度 ≥ 19 → magic → ver → payload_len ≤ 1400 → payload_len == 实际剩余长度 →（FlagAuth 置位时）HMAC。
**任何一步失败 = 丢弃该包并计数**，不回 NAK、不中断连接。

## 3. 事件类型（type 枚举）

| 值 | 名称 | 通道 | payload |
| --- | --- | --- | --- |
| 1 | HELLO | TCP | 版本协商（双端交换支持的 ver，取交集） |
| 2 | DISCOVER_RESP | UDP | 发现应答：主机名/系统/daemon 版本/配对状态（JSON，v1 约定见 daemon discovery 模块） |
| 3 | PAIR_REQ | TCP/UDP | 4 位确认码尝试 |
| 4 | PAIR_OK | TCP/UDP | 配对成功，签发 32B token |
| 5 | PAIR_NAK | TCP/UDP | 配对拒绝（码错/过期/超次数） |
| 6 | MOVE | UDP | 见 §4.1 |
| 7 | SCROLL | UDP | 见 §4.2 |
| 8 | BUTTON | TCP | 见 §4.3 |
| 9 | KEY | TCP | 见 §4.4 |
| 10 | TEXT | TCP | 见 §4.5 |
| 11 | ECHO | UDP/TCP | 见 §4.6，心跳与 RTT 测量 |
| 12 | BYE | TCP | 优雅断开 |
| 13 | ERR | TCP | 错误通报 |
| 14 | NOTICE | TCP | daemon → 手机提示（见 §4.7） |

未定义的 type 值：丢弃 + 计数（向前兼容）。

## 4. payload 规范（定长字段严格校验，长度不符 = 丢包）

### 4.1 MOVE（4B）——相对指针移动

```
int16 dx (BE) | int16 dy (BE)
```
单位为计数（count），由手机端灵敏度/加速曲线换算后下发；daemon 原样写 uinput REL_X/REL_Y。

### 4.2 SCROLL（4B）——高分辨率滚轮

```
int32 dy_hi_res (BE)
```
单位 = 1/120 滚轮格（与内核 REL_WHEEL_HI_RES 一致，**120 = 一格**；正值方向由"自然滚动"设置在手机端换算后下发）。

### 4.3 BUTTON（2B）

```
u8 btn | u8 down
```
btn：1=左键 2=中键 3=右键；down：1=按下 0=抬起。

### 4.4 KEY（3B）

```
u16 hid_usage (BE) | u8 down
```
hid_usage 为 **USB HID 键盘页 usage ID**（手机端键盘区直接发 HID 码，daemon 负责 HID → Linux KEY_* 映射；v2 macOS 后端映射 kVK_*）。down 同上。

### 4.5 TEXT（0..1400B）

原始 UTF-8 字节，长度由 payload_len 隐含；**必须是合法 UTF-8**（校验失败丢弃）。语音听写最终文本走此事件，daemon 走剪贴板 + Ctrl+V 注入（PRD §4.5）。

### 4.6 ECHO（8B）

```
int64 ts_ms (BE)
```
发送方 Unix 毫秒时间戳；对端原样回传同 payload，发起方算 RTT（不做跨机时钟同步）。

### 4.7 NOTICE（2B）——daemon → 手机提示

```
u8 code | u8 arg
```

唯一的**下行**事件：daemon 用它向手机通报控制权裁决结果，不改变任何注入状态。TCP 通道、随该客户端 token 封 FlagAuth（手机端验签失败即丢弃计数）。

| code | 含义 | arg |
| --- | --- | --- |
| 0 | 控制被其他设备占用：本机输入已被丢弃 | 冷静期剩余秒数（0..255，0 = 未知） |

未定义的 code：手机端忽略（向前兼容）。同一轮冷静期内 daemon 只下发一次，避免刷屏。

### 4.8 PAIR_REQ（0B / JSON / 4B）

配对请求两种形态（daemon 按 payload 首字节区分）：

```
空(0B) 或 JSON {"name":"<设备名>","dev":"<设备指纹>"}   # 发起配对（≤64B name，≤32B dev）
4B ASCII 数字                                          # 确认码尝试
```

- `name`：手机上报的展示名，≤64 字节；可为空串。
- `dev`：**可选**设备指纹 = 手机首次运行生成的 16 字节随机值 hex（32 个小写 hex 字符）或空串。
  daemon 用它区分「同一台手机重新配对」与「新手机」：指纹命中已配对记录时复用该记录、仅轮换 token（不占客户端名额），
  未命中才新增。缺省/非法时按新设备处理（对旧版手机向后兼容）。

### 4.9 多设备控制权（daemon 侧裁决，不改协议语义）

同一台电脑可能同时被多台手机连接（默认上限 4 台）。控制权同一时刻归**一台**设备（"第一设备"），规则：

- 持权设备有操作（任意 MOVE/SCROLL/BUTTON/KEY/TEXT）时，其他设备的控制事件一律丢弃，并向该设备下发
  NOTICE(0, 剩余秒数) 一次，同时进入 **15 秒冷静期**（`session.DefaultPreemptCooldown`）。
- 冷静期内该设备的控制事件继续丢弃（不再重复 NOTICE）。
- 冷静期满后，仅当持权设备**当前无操作**（`DefaultPreemptIdle` 内无控制事件且无按住的按键/按钮）时，
  该设备的控制事件才放行，并成为新的持权设备；若持权设备仍在操作，则重新进入 15 秒冷静期。
- 持权设备断开即释放控制权，任一设备的首个控制事件立刻取得控制权。
- 被拒设备若此刻仍持有按键/按钮（异常时序兜底），daemon 先补发其释放事件再丢弃其输入（PRD §11.2 零残留红线）。

## 5. 认证（HMAC）

- 配对成功后，daemon 签发 32 字节 token，双端持久化；此后所有报文置 FlagAuth 并携带 hmac_trunc。
- 计算：`hmac_trunc = HMAC-SHA256(key=token, header_19B || payload)[:8]`，其中 header 参与计算时 **hmac 字段（偏移 11..18）全零**。
- 校验用常数时间比较。FlagAuth 置位但接收方无 key、或比较失败 → 丢包 + 计数。

## 6. 一致性资产

| 资产 | 位置 | 用途 |
| --- | --- | --- |
| 黄金向量 | `protocol/testvectors.json` | 正/负用例（含 HMAC MOVE、CJK TEXT）；任何实现以此为对拍单测 |
| 真机验证程序 | `docs/PROBE-LINUX.md` | daemon 侧注入链路验收（非协议，联调用） |
