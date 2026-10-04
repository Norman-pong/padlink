# 口袋键鼠（padlink）

把 HarmonyOS 手机变成电脑的低延迟无线外设：**触摸板 + 全键盘 + 语音听写**，三指滑动切换。v1 目标平台 Ubuntu 26.04（GNOME Wayland）；通用 Linux"可用不承诺"；macOS 列入 v2（PRD 附录 B）。

本仓库为**文档与协议规范仓库，不含代码**——daemon 与鸿蒙工程从零实现。执行入口：[docs/HANDOFF.md](docs/HANDOFF.md)。

## 文档地图

| 文件 | 内容 |
| --- | --- |
| [docs/PRD.md](docs/PRD.md) | 产品需求（§0 执行铁律与事实源裁决、§4 功能规格、§11 验收） |
| [docs/HANDOFF.md](docs/HANDOFF.md) | **执行交接（agent 第一入口：环境 / 里程碑 M0–M5 与通过标准 / 关键坑→文档 ID / 红线）** |
| [docs/HARMONY-DOCIDS.md](docs/HARMONY-DOCIDS.md) | devecocli 官方文档 ID 索引（鸿蒙 API 查证唯一入口） |
| [docs/RESEARCH-WAYLAND.md](docs/RESEARCH-WAYLAND.md) | GNOME Wayland 集成技术方案（含上游 URL 与可信度） |
| [docs/PROBE-LINUX.md](docs/PROBE-LINUX.md) | Ubuntu 真机注入链路验证程序（M0 验收） |
| [protocol/PROTOCOL.md](protocol/PROTOCOL.md) | 线协议规范 v1（19B 头、13 事件、HMAC） |
| [protocol/testvectors.json](protocol/testvectors.json) | 黄金测试向量（两端编解码对拍单源） |

## 约束速记

- 鸿蒙 API 先查后写（`devecocli docs read <完整ID>`，索引见 HARMONY-DOCIDS.md）；协议改动先向量后代码。
- v1 硬边界：仅 GNOME Wayland 受支持；不做 X11 适配；不做公网/内网穿透。
- 完整铁律与裁决顺序见 PRD §0。
