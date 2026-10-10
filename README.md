# 口袋键鼠（padlink）

把 HarmonyOS 手机变成电脑的低延迟无线外设：**触摸板 + 全键盘 + 语音听写**，三指滑动切换。v1 目标平台 Ubuntu 26.04（GNOME Wayland）；通用 Linux"可用不承诺"；macOS 列入 v2（PRD 附录 B）。

本仓库为 **Monorepo**：`daemon/`（Go Linux 守护进程）+ `apps/harmony/`（鸿蒙 APP）+ `protocol/`（协议单源）+ `tools/`（对拍/单测 harness）。v1 代码已全部落地（M0–M4），**真机验收（Ubuntu GNOME Wayland + 鸿蒙真机）待执行**——程序见 [docs/PROBE-LINUX.md](docs/PROBE-LINUX.md)，里程碑状态见 [docs/HANDOFF.md](docs/HANDOFF.md)。执行入口：[docs/HANDOFF.md](docs/HANDOFF.md)。

> **使用者看这里**：在电脑上安装服务端、手机配对连接的完整步骤见 [docs/SETUP.md](docs/SETUP.md)；下文为开发向内容。电脑端一行安装：
>
> ```sh
> curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/daemon/packaging/install-remote.sh | sh
> ```

## 代码地图与常用命令

| 目录 | 内容 | 常用命令 |
| --- | --- | --- |
| `daemon/` | padlinkd 守护进程 + padlinkctl CLI（协议/注入/配对/文本注入） | `cd daemon && go vet ./... && go test ./...`；交叉编译 `GOOS=linux GOARCH=amd64 go build -o dist/bin/padlinkd-linux-amd64 ./cmd/padlinkd` |
| `daemon/packaging/` | udev uaccess 规则 + systemd user unit 两变体 + install.sh + deb/rpm 钩子脚本 | 安装步骤见 `daemon/README.md` |
| `daemon/cmd/padlinktoy/` | 联调工具三件套（record/replay 录制回放、协议 fuzz、ECHO RTT 压测，PRD §2.2 `tools/`） | `go -C daemon run ./cmd/padlinktoy -h`；用法见 `daemon/README.md`「联调工具」 |
| `apps/harmony/` | 鸿蒙 APP（API 26：触摸板/键盘/语音三功能区） | 首次克隆 `cp build-profile.json5.template build-profile.json5`（签名材料不入库，DevEco 自动签名生成本地副本）；`cd apps/harmony && devecocli build` |
| `tools/prototest/` | 协议黄金向量对拍（ArkTS 侧） | `node tools/prototest/run.mjs` |
| `tools/etstest/` | 鸿蒙纯逻辑单测（gesture/net/viewmodel/keyboard/voice） | `node tools/etstest/run.mjs <目录>` |
| `tools/etsrun/` | ets→ts 加载公共库（两个 harness 共用） | — |

## Linux 发版（daemon）

打 `v*` tag 即触发 `.github/workflows/release.yml`：GoReleaser（配置 `.goreleaser.yaml`）交叉编译 linux/amd64+arm64，产出 **tar.gz**（三二进制 + install.sh）、**deb** 与 **rpm**（padlinkd/padlinkctl + udev 规则 + user 单元，postinst 自动重载 udev），发布到 GitHub Releases。

```sh
git tag v1.0.0 && git push origin v1.0.0
# 本地预演产物：goreleaser release --snapshot --clean（落 dist/，已 gitignore）
```

## 文档地图

| 文件 | 内容 |
| --- | --- |
| [docs/SETUP.md](docs/SETUP.md) | **安装与使用指南（使用者入口：服务端安装 / 配对连接 / 主控页用法 / 排查表）** |
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
