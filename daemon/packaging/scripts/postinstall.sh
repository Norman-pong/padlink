#!/bin/sh
# deb/rpm 安装后：重载 udev 规则并触发 uinput 静态节点。
# uaccess ACL 在图形会话建立时附加，首次安装须注销重登后才对登录用户生效。
set -e

udevadm control --reload || true
udevadm trigger /dev/uinput || true

echo "padlink: udev 规则已安装。若为首次安装，请注销并重新登录，然后执行："
echo "  systemctl --user enable --now padlink"
