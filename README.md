# 口袋键鼠（padlink）

把 HarmonyOS 手机变成电脑的低延迟无线外设：**触摸板 + 全键盘 + 语音听写**。手机与电脑只需在同一局域网内，不经过任何云端。

支持平台：**macOS 13+**、**Ubuntu 26.04（GNOME Wayland）**、**Windows 11**（amd64 / arm64）。其他 Linux 发行版可用但不承诺兼容；明确不做 X11 适配与公网/内网穿透。

## 开始使用

### 1. 手机端安装

鸿蒙手机端已上架应用中心，扫码或点击二维码安装「口袋键鼠」：

<a href="https://appgallery.huawei.com/app/detail?id=com.zhimingcool.padlink">
  <img src="docs/images/appgallery-qr.png" alt="口袋键鼠应用中心下载二维码" width="220" />
</a>

### 2. 电脑端安装

**Windows 11**：以管理员身份打开 **PowerShell**，执行：

```powershell
irm https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.ps1 | iex
```

**macOS / Ubuntu**：打开终端，执行：

```sh
curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.sh | sh
```

脚本自动识别系统架构，下载对应安装包并校验 SHA-256 后安装。首次安装后，Windows 需重新打开终端；macOS 需按提示授权；Ubuntu 需注销并重新登录。

### 3. 连接电脑

在电脑终端确认服务正在运行：

```sh
padlinkctl status
```

手机端进入 APP 的「发现与配对」页，选中电脑，输入电脑屏幕上弹出的 4 位配对码即可。

已安装的电脑端升级（三平台同命令，保留配对记录）：

```sh
padlinkctl update
```

仅检查更新可使用 `padlinkctl update --check`；deb / rpm 安装请通过系统包管理器更新。

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
| `script/` | 一行安装脚本（Windows：`install.ps1`；macOS / Linux：`install.sh`）与安装回归测试 |

## 常用命令

```sh
# 以下命令均从仓库根目录执行
go -C daemon vet ./...
go -C daemon test ./... -race
go -C daemon run ./cmd/padlinkd --test -v     # 无手机注入自测

# 鸿蒙 APP（首次克隆先在 apps/harmony/ 下复制 build-profile.json5.template 为 build-profile.json5）
(cd apps/harmony && devecocli build)

# 契约对拍与纯逻辑单测（node ≥ 26，零依赖）
node tools/prototest/run.mjs
node tools/etstest/run.mjs .        # 全量；可传单目录名（默认 gesture）
```

开发与运维细节（模块划分、安装形态、控制通道、联调工具）见 **[daemon/README.md](daemon/README.md)**。

## 发版（双轨）

服务端与手机端独立发版、独立版本号，对照台账见 [docs/RELEASES.md](docs/RELEASES.md)：

- **服务端（daemon）**：打 `v*` tag → 先过 CI，再由 GoReleaser 发布各平台 tar.gz；Windows 包含 `install.ps1`，macOS / Linux 包含 `install.sh`，Linux 另提供 deb / rpm：

  ```sh
  git tag v1.4.0 && git push origin v1.4.0
  ```

- **手机端（APP）**：先 bump `apps/harmony/AppScope/app.json5` 的 versionName/versionCode（公式见文件内注释），本地 DevEco 构建提审 AppGallery 后，打 `app-v*` tag 留档（CI 校验版本一致并建 Release 记录）：

  ```sh
  git tag app-v1.3.2 && git push origin app-v1.3.2
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
