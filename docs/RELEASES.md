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
| 2026-10-16 | daemon | 1.4.0 | `v1.4.0` | 发现应答带 host_id（换网自愈的候选提示）；配对码原生弹窗（logo/倒计时/中英双语） | APP ≥1.3.2 才消费 host_id 自愈；旧 APP 忽略新字段 |
| 2026-10-16 | daemon | 1.5.0 | `v1.5.0` | `padlinkctl update`（下载→SHA-256 校验→执行包内 install.sh；--check / 指定版本回退 / 弱网重试；deb/rpm 不接管） | 存量 ≤1.4.0 无 update 命令，升 1.5.0 仍走一行安装器，1.5.0 起自更新可用；APP 侧无影响 |
| 2026-10-16 | daemon | 1.6.0 | `v1.6.0` | **Windows 11 适配**：SendInput 注入后端（绝对定位/扫描码/滚轮整格）、文本注入（Win32 剪贴板）、配对码原生弹窗（倒计时）、HKCU Run 自启、install.ps1 + 一行安装、windows amd64/arm64 双包、update 支持 | 新平台，存量端不受影响；APP 零改动（协议未动）；Windows 端注意第三方安全软件需放行 53021（SETUP 排查表） |
| 2026-10-16 | daemon | 1.6.1 | `v1.6.1` | Windows daemon 构建改 **windowsgui 子系统**：不再有可见控制台窗口（控制台窗口被关闭会连带杀掉 daemon，实测踩坑）；交互调试经 attachParentConsole 接回父控制台，已重定向的 stdout 不动 | 仅 Windows daemon 二进制变化，1.6.0 用户 `padlinkctl update` 即得；其他平台无影响 |
| 2026-10-19（确认上架） | APP | 1.3.2 | 尚未留档 tag | 用户确认鸿蒙端已上架应用中心；实际首次上架日期待补。已配对档案按 hostId 匹配与换 IP 迁移自愈；拒收未封签 ECHO 应答；「配对码」文案与双语 | daemon <1.4.0 时不广播 host_id，自愈回落现状（ip:port 直连），其余功能不受影响 |
| 2026-10-19 | daemon | 1.6.2 | [v1.6.2](https://github.com/Norman-pong/padlink/releases/tag/v1.6.2) | 已发布 13 项产物；修复 Windows 更新脚本名称误报与防火墙命令；一行安装增加重试、校验与解压失败检查，安装子进程临时绕过执行策略；拆分会话与输入内核职责；补 Windows 安装和更新 CI 门禁 | 协议与配对数据保持兼容，存量 APP 无需同步升级；1.5.0 起可使用 `padlinkctl update`，Windows 更新失败提示中的旧脚本名在升级后修正 |
| 待提审 | APP | 1.3.3 | 提审后创建 `app-v1.3.3` | 优化触摸采样与移动滤波；适配折叠外屏及小屏布局；安装指南增加 Windows 说明、系统切换与逐条复制 | 保持现有协议；建议电脑端使用 1.6.2，旧版仍可连接；AGC 认证返回凭证错误，待修正凭证并配置发布签名，尚未上传或提交审核 |

## 兼容性说明（host_id 换网自愈）

- 自愈需要**两端同时 ≥ 对应版本**：daemon 1.4.0 广播 host_id、APP 1.3.2 按 hostId 迁移。
- 任一端为旧版时功能自动降级：旧 daemon 不广播 → APP 回落 ip:port 匹配（原行为）；旧 APP 忽略 host_id 字段。
- 迁移档案的落盘以 token 封签报文验签通过为前提（PROTOCOL.md §4.10），单端升级不引入安全回归。
