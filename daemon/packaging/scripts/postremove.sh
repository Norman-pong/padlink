#!/bin/sh
# deb/rpm 卸载后：重载 udev 规则（规则文件本身由包管理器移除）。
set -e

udevadm control --reload || true
