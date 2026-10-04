#!/usr/bin/env bash
# PROBE-LINUX 验收辅助脚本：自动化 docs/PROBE-LINUX.md §0–§4 的机械步骤。
# §4 的目视确认（光标画圆/敲键/滚动）与 §5/§6 的 evtest/libinput 取证仍需人工执行。
# 用法（在 Ubuntu 26.04 GNOME Wayland 会话内、仓库根目录）：
#   bash daemon/dist/probe-acceptance.sh [--dry-run] [--skip-install]
# 选项：
#   --dry-run       只打印将执行的命令，不实际执行（需 root 的步骤标注 sudo）
#   --skip-install  跳过 §3 安装步骤（二进制/service 已就位时）
# 退出码：任一 FAIL 步骤即非零；全部 PASS/手动项为 0。
set -uo pipefail

DRY_RUN=0
SKIP_INSTALL=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --skip-install) SKIP_INSTALL=1 ;;
    *) echo "未知参数: $arg"; exit 2 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
BIN="$REPO_ROOT/daemon/dist/bin/padlinkd-linux-$ARCH"

PASS=0; FAIL=0
step() { # step <名称> <命令...>
  local name="$1"; shift
  if [ "$DRY_RUN" = 1 ]; then
    echo "[DRY ] $name: $*"
    return 0
  fi
  echo "---- $name"
  if "$@"; then
    echo "[PASS] $name"; PASS=$((PASS+1))
  else
    echo "[FAIL] $name（退出码 $?）"; FAIL=$((FAIL+1))
  fi
}
manual() { echo "[MANUAL] $1"; }

# 平台护栏：本脚本面向 Linux（uinput/udev/systemd）
if [ "$(uname -s)" != "Linux" ]; then
  echo "仅支持 Linux（当前 $(uname -s)）。macOS 上可用 --dry-run 查看将执行的命令。"
  [ "$DRY_RUN" = 1 ] || exit 1
fi

echo "===== PROBE §0 环境自检 ====="
if [ "$DRY_RUN" = 1 ]; then
  echo "[DRY ] 检查 XDG_SESSION_TYPE=wayland 与内核版本（≥5.0）"
else
  echo "XDG_SESSION_TYPE=$XDG_SESSION_TYPE（期望 wayland；非 wayland 即 PRD 不支持环境，勿继续）"
  echo "内核 $(uname -r)（需 ≥5.0 以支持 REL_WHEEL_HI_RES）"
  [ "$XDG_SESSION_TYPE" = "wayland" ] && { echo "[PASS] Wayland 会话"; PASS=$((PASS+1)); } || { echo "[FAIL] 非 Wayland 会话"; FAIL=$((FAIL+1)); }
fi

echo "===== PROBE §1 uinput 模块 ====="
step "加载 uinput 模块" sudo modprobe uinput
step "检查 /dev/uinput 节点" test -e /dev/uinput

if [ "$SKIP_INSTALL" = 0 ]; then
  echo "===== PROBE §2 udev uaccess 规则 ====="
  step "安装 udev 规则" sudo cp "$REPO_ROOT/daemon/dist/udev/69-padlink-uinput.rules" /etc/udev/rules.d/
  step "重载并触发 udev" sudo udevadm control --reload
  manual "执行: sudo udevadm trigger /dev/uinput ；然后【注销并重新登录】（uaccess ACL 在会话建立时附加）"
  step "校验 ACL（重登后执行本脚本可见 PASS）" bash -c 'getfacl /dev/uinput 2>/dev/null | grep -q "$(id -un)"'

  echo "===== PROBE §3 安装二进制与 user service ====="
  step "检查待安装二进制存在" test -f "$BIN"
  step "放置二进制" mkdir -p "$HOME/.local/bin" && cp "$BIN" "$HOME/.local/bin/padlinkd"
  step "安装 systemd user unit" bash -c "mkdir -p ~/.config/systemd/user && cp '$REPO_ROOT/daemon/dist/systemd/padlink.service' ~/.config/systemd/user/"
  step "daemon-reload 并启用服务" systemctl --user daemon-reload
  step "启动服务" systemctl --user enable --now padlink
  manual "查看服务日志: journalctl --user -u padlink -f（静默即正常）"
fi

echo "===== PROBE §4 注入自测（无手机） ====="
if [ "$DRY_RUN" = 1 ]; then
  echo "[DRY ] $HOME/.local/bin/padlinkd --test -v"
else
  echo ">>>> 屏幕上应出现：光标画两个圆、左键单击、打出 pl、滚动 +2/-1 格 <<<<"
  "$HOME/.local/bin/padlinkd" --test -v
  rc=$?
  [ $rc -eq 0 ] && { echo "[PASS] padlinkd --test（退出码 0）"; PASS=$((PASS+1)); } || { echo "[FAIL] padlinkd --test（退出码 $rc）"; FAIL=$((FAIL+1)); }
fi

echo "===== 人工取证项（自动化不可替代） ====="
manual "§4 目视：确认屏幕上光标画圆/点击/敲键/滚动（验收关键项）"
manual "§5 sudo evtest 选 'PadLink Virtual Pointer'，另开终端重跑 --test，期望 EV_REL/KEY/WHEEL_HI_RES 片段"
manual "§6 sudo libinput list-devices | grep -A6 -i padlink"
manual "§7 收尾：systemctl --user disable --now padlink（不想开机自启时）"

echo "===== 汇总 ====="
echo "自动步骤 PASS=${PASS} FAIL=${FAIL}（另有 [MANUAL] 人工项需逐条确认）"
[ "$FAIL" -eq 0 ]
