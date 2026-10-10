# 安装与使用指南

> 本文档为仓库侧完整版。APP 内置同款浓缩向导（发现页空态「电脑还没装服务端？」入口 / 设置 → 关于 → 服务端安装指南），两处内容改动需同步。

面向**使用者**：在电脑上装好服务端（padlinkd），手机 APP 才能找到电脑、配对并控制。全部通信仅在局域网内进行，不经过任何云端。

支持平台：**Ubuntu 26.04（GNOME Wayland，正式支持）**、**macOS（Apple Silicon / Intel）**。其他 Linux 发行版可用 tar.gz 安装，但不承诺兼容性。

## 一、在电脑上安装服务端

### 一行命令安装（Ubuntu / macOS，推荐）

在电脑终端执行：

```sh
curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/daemon/packaging/install-remote.sh | sh
```

脚本自动识别系统与架构，下载最新 Release 安装包、校验 SHA-256 后完成安装（Ubuntu 仅 udev 规则一步需要 sudo 密码）。装完后按平台收尾：

- **Ubuntu**：**注销并重新登录**（首次安装必须，否则注入权限不生效）；语音听写再装 `sudo apt install wl-clipboard`；建议 `gsettings set org.gnome.desktop.peripherals.mouse accel-profile 'flat'`。
- **macOS**：允许首次运行弹出的两项授权——「辅助功能」与「接受传入连接」。

`padlinkctl status` 能看到版本号即安装成功。以下为手动安装（备选）。

### Ubuntu 26.04（手动安装）

1. 从 GitHub Releases 下载对应包：
   - **deb**（推荐）：`sudo apt install ./padlink_<版本>_linux_amd64.deb`
   - **rpm**：`sudo dnf install ./padlink_<版本>_linux_x86_64.rpm`
   - **tar.gz**：解压后在包根目录执行 `./install.sh`
2. **首次安装后注销并重新登录**（/dev/uinput 的访问权限在登录时附加，不注销不生效）。
3. 启动并设为开机自启：

   ```sh
   systemctl --user enable --now padlink
   padlinkctl status        # 能看到版本号即正常
   ```

4. 语音听写依赖（不装则语音上屏功能降级，其余不受影响）：

   ```sh
   sudo apt install wl-clipboard
   ```

5. 建议把指针加速设为 flat（GNOME 默认曲线会与手机端灵敏度叠加；daemon 检测到非 flat 会在日志里警告）：

   ```sh
   gsettings set org.gnome.desktop.peripherals.mouse accel-profile 'flat'
   ```

### macOS（手动安装）

1. 从 GitHub Releases 下载 `padlink_<版本>_darwin_<arch>.tar.gz`（arm64 = Apple Silicon，amd64/x86_64 = Intel），解压后在包根目录执行 `./install.sh`（全程无需 sudo）。
2. 首次运行系统会各弹一次授权窗，**两项都必须允许**：
   - **辅助功能**（系统设置 → 隐私与安全性 → 辅助功能）：不授权则注入的键鼠事件被系统静默丢弃，表现为「连上了但光标不动」；
   - **接受传入连接**：不授权则手机搜不到/连不上本机。
3. 日志在 `~/Library/Logs/padlinkd.log`；`padlinkctl status` 可验证运行状态。

### 验证服务端（可选，无手机自测）

```sh
padlinkd --test -v   # Linux：光标画圆 → 点击 → 打字 → 滚动，全过打印 PASSED
```

## 二、手机连接电脑（首次配对）

前提：**手机和电脑连同一 WiFi**（同一局域网；公司/公共 WiFi 若开了 AP 隔离会搜不到，见排查表）。

1. 打开电脑服务端（上节），保持电脑不睡眠。
2. 手机打开 APP，进入「**发现与配对**」页，自动搜索附近主机（电脑没跑 padlinkd 会一直显示「正在搜索主机…」）。
3. 在「附近的主机」里点你的电脑 → 发起配对。
4. **电脑屏幕上弹出 4 位确认码**（桌面通知）。没看到通知就在电脑终端运行 `padlinkctl pair`，终端会显示同一个码。
5. 在手机上输入这 4 位码 → 提示「配对成功」，自动进入主控页。

配对只需一次：token 存在手机本机，之后打开 APP 用「快速连接上次的主机」即可直连。确认码 60 秒有效、最多试 5 次、一台电脑最多配 4 台手机。

## 三、主控页怎么用

单页三合一布局，无需切换：

- **触摸面**（整页常驻）：单指滑动移光标，单指点按 = 左键，双指滚动，双指点按 = 右键，长按拖动。
- **键盘**：点右下角键盘浮键展开——直接用系统输入法打字（文字实时上屏到电脑），上方键条提供 Enter、方向键、收起等补键；点「收起」键收回。
- **语音听写**：**按住**右下角麦克风说话，松开自动识别并把文字打到电脑上（说完了松手即可，无需再点）。

## 四、管理已配对的电脑

- 手机端：「设置 → 已配对主机」可重命名、设默认、删除（删除即解除配对）。
- 电脑端：`padlinkctl clients` 查看已配对设备；`padlinkctl unpair <id>` 解除某台并将其踢下线。

## 五、连不上 / 不好用怎么办

| 现象 | 可能原因 | 处理 |
| --- | --- | --- |
| 一直「正在搜索主机…」 | 电脑没跑 padlinkd；不在同一局域网；AP 隔离；防火墙拦了 53021 端口 | 电脑跑 `padlinkctl status` 确认在运行；确认同一 WiFi；路由器关闭 AP 隔离；防火墙放行 UDP/TCP 53021 |
| 配对失败：确认码错误 / 已过期 / 尝试次数过多 | 码 60 秒过期，错 5 次作废 | 重新点主机发起配对，用新码 |
| 配对失败：客户端已满 | 该电脑已配满 4 台 | 电脑 `padlinkctl clients` 后 `unpair` 一台 |
| 连上了但光标不动（macOS） | 辅助功能未授权 | 系统设置 → 隐私与安全性 → 辅助功能，勾选 padlinkd 后重启它 |
| 语音说完不上屏（Ubuntu） | 缺 wl-clipboard | `sudo apt install wl-clipboard` |
| 光标手感「发飘」 | GNOME 指针加速非 flat | 见一.5 的 gsettings 命令 |
| 断线后连不上 | WiFi 切换/路由器丢包 | 等待自动重连（约 30 秒内），或回发现页重新连接 |

## 网络与端口一览

| 项 | 值 |
| --- | --- |
| 端口 | **53021**（UDP + TCP） |
| 发现 | 手机 UDP 广播 HELLO → 电脑单播应答（含主机名/系统/版本） |
| 控制与按键、文本 | TCP（逐包 HMAC 认证，token 配对时签发） |
| 指针移动/滚动 | UDP（高频可丢，新值自然覆盖旧值） |
| 心跳 | 手机 1Hz 保活，10 秒无包判失联 |
| 范围 | 仅局域网，无公网/穿透 |
