# prototest — ArkTS 协议编解码对拍 harness

以 `protocol/testvectors.json` 黄金向量为单源，在 node 里直接跑鸿蒙侧 ArkTS 协议层
（`apps/harmony/entry/src/main/ets/common/proto/`），验证其编解码语义与 Go 参考实现
（`daemon/internal/proto/proto.go`）逐字节一致。

## 运行

```sh
node tools/prototest/run.mjs
```

要求 node ≥ 26（`--experimental-strip-types` 已默认启用）。零第三方依赖，退出码 0 = 全过，
1 = 存在失败（失败行会打印期望/实得的 hex 或字段差异）。

## 覆盖范围

- 8 条 valid 向量：decode 成功、type/seq/语义字段逐项断言、解码结果重新 encode 与向量 hex 级一致；
  未认证向量另做"同语义重新构造 → encode"hex 比对。
- 向量 2（HMAC MOVE）专项：`verifyHmac` 为真；同语义构造 + `seal` 后 encode 与向量 hex 级一致（含 hmac 字段）；
  并用 node:crypto 独立按"flags 含 FlagAuth、偏移 11..18 清零"重算 hmac 与向量内嵌值比对（防实现与向量双向都错）。
- 3 条 invalid 向量：decode 失败且 `DecodeError` 类别对应 `expect_error`。
- 追加负例：未知 type 0xF0、ver=2、各定长类型 payload 长度不符、TEXT 非法/超长/代理对 UTF-8、
  篡改 1 字节 hmac、未置 FlagAuth 调 `verifyHmac`、空输入。

## 为何用临时目录改写导入

- node 无法直接加载 `.ets` 后缀的模块；type-stripping 也要求相对导入带显式扩展名。
  故先把 `*.ets` 复制到 `os.tmpdir()` 下 `mkdtemp` 的临时目录改名为 `.ts`，
  并把源码内 `from './x'` 重写为 `from './x.ts'`，再动态 import。
- 类型仅使用的导入（如 `Packet` 接口）在源码中一律 `import type`，否则 ESM 链接期会因
  接口被擦除而报"不存在该导出"。这也是 codec 源码本身的约束（同时满足 node 直跑与 ArkTS 编译）。
- 源码不使用 TS enum / namespace / 参数属性等"需转换"语法（只允许可擦除语法），因此
  事件类型与错误类别均为 readonly 类静态常量。
- 临时目录在 `finally` 中 `rm -rf` 递归删除，不残留。

## codec 层约束

`apps/harmony/entry/src/main/ets/common/proto/` 下的文件是纯逻辑层：

- **禁止 import 任何 `@ohos.*` 模块**（含 `@ohos.util` 的 TextEncoder/TextDecoder——UTF-8 编解码为手写实现），
  保证 node 可直跑、可被本 harness 对拍，也便于未来迁移复用。
- **禁止装饰器**、TS enum、`any`/`unknown`、解构默认值等非 ArkTS 安全写法。
- 依赖方向单向：`PacketType ← Packet ← Codec ← Auth`，另有 `Utf8` 被 `Packet`/`Codec` 复用，无环。
