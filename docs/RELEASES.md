# 发布台账（双轨版本）

daemon（电脑端）与 APP（手机端）**独立发版、独立版本号**，本文件是两者的唯一对照台账：
每次发布在下方表格追加一行，并写清对端兼容要求。

## 版本来源与发布机制

| 端 | 版本来源 | 发布 tag | 机制 |
| --- | --- | --- | --- |
| daemon | tag 即版本（goreleaser 注入 `padlinkd -version`） | `vX.Y.Z` | GitHub Actions → GoReleaser 出 tar.gz/deb/rpm → GitHub Releases |
| APP | `apps/harmony/AppScope/app.json5` 的 versionName/versionCode 独立维护（versionCode = major×1e6 + minor×1e4 + patch×1e2） | `app-vX.Y.Z` | 本地 DevEco 构建提审 AppGallery；打 tag 后 CI 校验版本一致并建 GitHub Release 记录 |

发布门禁（`tools/check-version.mjs`，两轨共用）：
- `v*`（daemon）：只校验 app.json5 内部一致性，不要求与 tag 相等（APP 独立迭代）。
- `app-v*`（APP）：app.json5 必须与 tag 一致。

## 台账

| 日期 | 端 | 版本 | tag | 关键内容 | 对端兼容要求 |
| --- | --- | --- | --- | --- | --- |
| 2026-10-14 | daemon+APP | 1.3.1 | v1.3.1 | 文本未上屏提示（NOTICE 回传）等；历史合轨发布（v1.0.0–v1.3.1 两端同号） | — |
| 待发布 | daemon | 1.4.0 | `v1.4.0` | 发现应答带 host_id（换网自愈的候选提示）；配对码原生弹窗（logo/倒计时/中英双语） | APP ≥1.3.2 才消费 host_id 自愈；旧 APP 忽略新字段 |
| 待发布 | APP | 1.3.2 | `app-v1.3.2` | 已配对档案按 hostId 匹配与换 IP 迁移自愈；拒收未封签 ECHO 应答；「配对码」文案与双语 | daemon <1.4.0 时不广播 host_id，自愈回落现状（ip:port 直连），其余功能不受影响 |

## 兼容性说明（host_id 换网自愈）

- 自愈需要**两端同时 ≥ 对应版本**：daemon 1.4.0 广播 host_id、APP 1.3.2 按 hostId 迁移。
- 任一端为旧版时功能自动降级：旧 daemon 不广播 → APP 回落 ip:port 匹配（原行为）；旧 APP 忽略 host_id 字段。
- 迁移档案的落盘以 token 封签报文验签通过为前提（PROTOCOL.md §4.10），单端升级不引入安全回归。
