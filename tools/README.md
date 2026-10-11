# tools — ArkTS 侧测试 harness

三个 node 脚本（零第三方依赖，要求 node ≥ 26），把鸿蒙端纯逻辑源码按 ArkTS 约定加载后直接跑，
使 `gesture/`、`proto/` 等目录的正确性不必依赖模拟器或真机。

| 脚本 | 用途 | 运行 |
| --- | --- | --- |
| `etstest/run.mjs` | 跑 `common/<目录>` 下的纯逻辑单测 | `node tools/etstest/run.mjs <目录>`（默认 `gesture`） |
| `prototest/run.mjs` | 以黄金向量对拍 ArkTS 协议层编解码 | `node tools/prototest/run.mjs` |
| `etsrun/load.mjs` | 上面两者的共享加载库（被 import，不单独运行） | — |

退出码 0 = 全过，1 = 存在失败。

## 加载机制

node 无法直接加载 `.ets`，type-stripping 也要求相对导入带显式扩展名。`etsrun/load.mjs` 因此把源码目录树
复制到 `os.tmpdir()` 下 `mkdtemp` 的临时目录，`.ets` 改名 `.ts`、源码内相对导入补 `.ts`；调用方在
`finally` 中 `unloadEtsTree` 递归删除，不残留。

## 纯逻辑层的可测试性约束

被 harness 加载的目录必须自包含并遵守：

- **禁止 import 任何 `@ohos.*` 模块**（含 `@ohos.util` 的 TextEncoder/TextDecoder——UTF-8 由 `common/proto/Utf8.ets` 手写实现）。
- **禁止装饰器、TS enum、namespace、参数属性**等"需转换"语法，只允许可擦除语法；事件类型与错误类别用 readonly 静态常量表达。
- 类型仅使用的导入一律 `import type`，否则 ESM 链接期会因接口被擦除而报"不存在该导出"；这同时是 ArkTS 编译的要求。
- 依赖方向单向：`PacketType ← Packet ← Codec ← Auth`，`Utf8` 被 `Packet`/`Codec` 复用，无环。

新增纯逻辑目录的单测放该目录内 `*.test.mjs`。

## prototest 覆盖范围

以 `protocol/testvectors.json` 为单源，验证 ArkTS 协议层（`apps/harmony/entry/src/main/ets/common/proto/`）
与 Go 参考实现（`daemon/internal/proto/proto.go`）逐字节一致：

- valid 向量：decode 成功、type/seq/语义字段逐项断言、重新 encode 与向量 hex 级一致。
- HMAC 向量专项：`verifyHmac` 为真；另用 `node:crypto` 独立按"flags 含 FlagAuth、偏移 11..18 清零"重算 hmac
  与向量内嵌值比对，防实现与向量双向都错。
- invalid 向量：decode 失败且错误类别对应 `expect_error`。
- 追加负例：未知 type、ver 不符、各定长类型 payload 长度不符、TEXT 非法/超长/代理对、篡改 hmac、未置 FlagAuth 调校验、空输入。
