# 口袋键鼠（padlink）

把 HarmonyOS 手机变成电脑的低延迟无线外设：**触摸板 + 全键盘 + 语音听写**，三指滑动切换。手机与电脑只需在同一局域网内，不经过任何云端。

支持平台：**macOS 13+**、**Ubuntu 26.04（GNOME Wayland）**。其他 Linux 发行版可用但不承诺兼容；明确不做 X11 适配与公网/内网穿透。

## 开始使用

电脑端一行安装（Ubuntu / macOS）：

```sh
curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.sh | sh
```

手机端进入 APP 的「发现与配对」页，选中电脑，输入电脑屏幕上弹出的 4 位配对码即可。

完整步骤 —— 安装收尾、主控页用法、管理已配对电脑、连不上时的排查表 —— 见 **[docs/SETUP.md](docs/SETUP.md)**。

## 仓库结构

Monorepo，协议规范为两端单源：

| 目录 | 内容 |
| --- | --- |
| `daemon/` | Go 守护进程 `padlinkd` + CLI `padlinkctl` + 联调工具 `padlinktoy`（零第三方依赖） |
| `apps/harmony/` | 鸿蒙 APP（ArkTS/ArkUI，API 26） |
| `protocol/` | 线协议规范与黄金向量（两端编解码的字节级单源） |
| `tools/` | ArkTS 侧测试 harness（纯逻辑单测、协议对拍） |
| `docs/` | 使用指南、产品需求、平台依据台账、上架手册 |
| `script/install.sh` | 一行远程安装脚本 |

## 常用命令

```sh
# 服务端
cd daemon && go vet ./... && go test ./... -race
go -C daemon run ./cmd/padlinkd --test -v     # 无手机注入自测

# 鸿蒙 APP（首次克隆先 cp build-profile.json5.template build-profile.json5）
cd apps/harmony && devecocli build

# 契约对拍与纯逻辑单测（node ≥ 26，零依赖）
node tools/prototest/run.mjs
node tools/etstest/run.mjs .        # 全量；可传单目录名（默认 gesture）
```

开发与运维细节（模块划分、安装形态、控制通道、联调工具）见 **[daemon/README.md](daemon/README.md)**。

## 发版

打 `v*` tag 触发 GitHub Actions 跑 GoReleaser，产出 tar.gz（三二进制 + install.sh）、deb 与 rpm 并发布到 GitHub Releases：

```sh
git tag v1.0.0 && git push origin v1.0.0
```

## 文档

| 文件 | 内容 |
| --- | --- |
| [docs/SETUP.md](docs/SETUP.md) | **安装与使用指南（使用者入口）** |
| [daemon/README.md](daemon/README.md) | daemon 开发与运维 |
| [protocol/PROTOCOL.md](protocol/PROTOCOL.md) | 线协议规范 v1（19B 头、14 事件、HMAC） |
| [docs/PRD.md](docs/PRD.md) | 产品需求与执行约束 |
| [docs/HANDOFF.md](docs/HANDOFF.md) | 执行交接（环境 / 里程碑 / 关键坑） |
| [docs/RESEARCH-WAYLAND.md](docs/RESEARCH-WAYLAND.md) | GNOME Wayland 集成事实与陷阱 |
| [docs/PLAN-MACOS.md](docs/PLAN-MACOS.md) | macOS 移植方案与平台差异 |
| [docs/HARMONY-DOCIDS.md](docs/HARMONY-DOCIDS.md) | 鸿蒙 API 查证索引 |
| [docs/PROBE-LINUX.md](docs/PROBE-LINUX.md) | Ubuntu 真机验收程序 |
| [docs/AGC-SUBMIT.md](docs/AGC-SUBMIT.md) | AppGallery 上架手册 |
