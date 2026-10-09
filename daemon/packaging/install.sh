#!/bin/sh
# PadLink tar.gz 安装脚本（用户级安装；仅 udev 规则一步需要 sudo）。
# 用法：解压 release tar.gz 后在包根目录执行 ./install.sh
set -eu

SRC=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN_DIR="$HOME/.local/bin"
UNIT_DIR="$HOME/.config/systemd/user"

mkdir -p "$BIN_DIR" "$UNIT_DIR"
install -m 0755 "$SRC/padlinkd" "$BIN_DIR/padlinkd"
install -m 0755 "$SRC/padlinkctl" "$BIN_DIR/padlinkctl"
install -m 0644 "$SRC/packaging/systemd/padlink-local.service" "$UNIT_DIR/padlink.service"
echo "已安装 padlinkd / padlinkctl 到 $BIN_DIR"

# udev uaccess 规则：/dev/uinput 授权的必需项（详见 daemon/README.md）。
RULE=/etc/udev/rules.d/69-padlink-uinput.rules
if [ ! -f "$RULE" ]; then
  echo "安装 udev 规则需要 root（一次性）："
  if sudo install -m 0644 "$SRC/packaging/udev/69-padlink-uinput.rules" "$RULE"; then
    sudo udevadm control --reload || true
    sudo udevadm trigger /dev/uinput || true
    echo "udev 规则已安装。首次安装须注销并重新登录（uaccess ACL 在图形会话建立时附加）。"
  else
    echo "sudo 失败，请稍后手动执行：" >&2
    echo "  sudo install -m 0644 $SRC/packaging/udev/69-padlink-uinput.rules $RULE" >&2
    echo "  sudo udevadm control --reload && sudo udevadm trigger /dev/uinput" >&2
  fi
fi

systemctl --user daemon-reload
systemctl --user enable --now padlink
echo "padlink 服务已启用（systemctl --user status padlink 查看）。"
