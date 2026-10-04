# PROBE-LINUX：Ubuntu 26.04 真机注入链路验证程序（M0 验收）

> 本程序验证 **M0 交付物**（padlinkd 及安装物）在真实 GNOME Wayland 会话的端到端注入：libinput 消费虚拟设备（光标画圆）、uaccess 权限在登录会话生效、systemd user service 常驻。文中 `daemon/dist/…` 路径指 M0 构建产物。
> 逐条执行，期望输出已注明；任何一步不符即停并记录。

## 0. 环境自检（30 秒）

```bash
# 期望：XDG_SESSION_TYPE=wayland（GNOME 默认）；不是则 PRD 明确不支持，勿继续
echo "$XDG_SESSION_TYPE"; uname -r   # 内核 ≥ 5.0 才有 REL_WHEEL_HI_RES（26.04 远超）
```

## 1. 装工具 + 加载 uinput

```bash
sudo apt update && sudo apt install -y evtest
sudo modprobe uinput && ls -l /dev/uinput
# 期望：crw------- 1 root root 10,223 ...（节点存在；权限下一步解决）
```

## 2. 装 udev uaccess 规则并重登

```bash
sudo cp daemon/dist/udev/69-padlink-uinput.rules /etc/udev/rules.d/
sudo udevadm control --reload && sudo udevadm trigger
# 注销并重新登录（uaccess ACL 在会话建立时附加），然后：
getfacl /dev/uinput | grep "$(id -un)"
# 期望：user:<你的用户名>:rw-     ← 无 root 即可打开 uinput 的证据
```

## 3. 放置二进制并安装 user service（可选，也可先裸跑）

```bash
mkdir -p ~/.local/bin && cp daemon/dist/bin/padlinkd-linux-$(dpkg --print-architecture) ~/.local/bin/padlinkd
mkdir -p ~/.config/systemd/user && cp daemon/dist/systemd/padlink.service ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now padlink
journalctl --user -u padlink -f   # 期望：服务常驻不崩溃（M0 版无网络循环，静默即正常；网络循环 M2 引入）
```

## 4. 注入自测（PRD §10 调试第一层：无手机）

```bash
~/.local/bin/padlinkd --test -v
# 期望输出：
#   drawing 2 circles (r=200px, 120 segments each)…
#   left click…  typing 'pl' (HID usage map)…  scrolling hi-res +2 notches / -1 notch…
#   [verbose] device "PadLink Virtual Pointer" created, sequence complete
#   padlinkd --test PASSED          ← 退出码 0
# 失败样本与原因：
#   /dev/uinput 不存在  → sudo modprobe uinput（或 BIOS/内核裁剪掉了 uinput）
#   无权限打开          → 第 2 步 uaccess 未生效（是否忘了重登？）
```

**此时直接看屏幕：光标应画出两个圆、点一下左键、打出 "pl"、页面滚动 2 格再回 1 格。这是 Wayland 端到端的关键一环。**

## 5. 内核层事件取证（光标不动时用这层定位）

```bash
sudo evtest   # 交互列表中选择 "PadLink Virtual Pointer"，另开终端执行第 4 步
# 期望片段：
#   Event: time ..., type 2 (EV_REL), code 0 (REL_X), value -2
#   Event: time ..., type 1 (EV_KEY), code 272 (BTN_LEFT), value 1
#   Event: time ..., type 1 (EV_KEY), code 25 (KEY_P), value 1
#   Event: time ..., type 2 (EV_REL), code 11 (REL_WHEEL_HI_RES), value 240
#   Event: time ..., type 2 (EV_REL), code 8 (REL_WHEEL), value 1
#   （设备能力块应含 EV_SYN/EV_KEY/EV_REL，KEY 位图含 KEY_*，REL 含 WHEEL_HI_RES）
```

## 6. libinput 视角确认（可选，GNOME 侧"设备被认领"证据）

```bash
sudo libinput list-devices | grep -A6 -i padlink
# 期望：PadLink Virtual Pointer 出现，且列出 Scroll wheel / Left/Middle/Right 按钮
```

## 7. 收尾

```bash
systemctl --user disable --now padlink   # 不想开机自启时
```
