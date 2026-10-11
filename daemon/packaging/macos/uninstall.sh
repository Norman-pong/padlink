#!/bin/sh
# PadLink macOS 卸载脚本（用户级，无需 sudo）。
# 用法：
#   ./uninstall.sh            # 移除 LaunchAgent + PadLink.app + padlinkctl + 日志
#   ./uninstall.sh --purge    # 同时移除本机签名身份与配对 token（= 彻底回到未安装状态）
#   ./uninstall.sh --purge --keep-identity   # 清 token 但保留签名身份（保留辅助功能授权）
#
# 说明：--purge 会删除 ~/Library/Keychains/padlink-signing.keychain-db 与口令文件，
# 于是下次安装会生成**新的证书**——在 TCC 眼里就是一个全新应用，所有权限（辅助功能、本地网络）
# 都需要重新授权。这正是「环境不统一导致排查困难」时的彻底复位手段。
set -eu

PURGE=0
KEEP_IDENTITY=0
for arg in "$@"; do
  case "$arg" in
    --purge) PURGE=1 ;;
    --keep-identity) KEEP_IDENTITY=1 ;;
    *) echo "未知参数: $arg" >&2; exit 2 ;;
  esac
done

BIN_DIR="$HOME/.local/bin"
APP="$HOME/Applications/PadLink.app"
AGENT_DIR="$HOME/Library/LaunchAgents"
LABEL=com.zhimingcool.padlink
PLIST="$AGENT_DIR/$LABEL.plist"
LOG="$HOME/Library/Logs/padlinkd.log"
STATE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/padlink"

# 删除前校验：所有目标必须落在 $HOME 之内，避免变量异常时误删系统路径
guard() {
  case "$1" in
    "$HOME"/*) ;;
    *) echo "拒绝删除（不在 HOME 内）: $1" >&2; exit 1 ;;
  esac
}

UID_NUM=$(id -u)
echo "== 停止并移除 LaunchAgent =="
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null && echo "  已 bootout $LABEL" || echo "  （未在运行）"
if [ -f "$PLIST" ]; then guard "$PLIST"; rm -f "$PLIST"; echo "  已删除 $PLIST"; fi

echo "== 移除应用与命令行工具 =="
if [ -d "$APP" ]; then
  guard "$APP"
  # 先注销，避免 LaunchServices 里留下指向已删应用的图标缓存
  LSREGISTER=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
  [ -x "$LSREGISTER" ] && "$LSREGISTER" -u "$APP" 2>/dev/null || true
  rm -rf "$APP"; echo "  已删除 $APP"
fi
for f in "$BIN_DIR/padlinkctl" "$BIN_DIR/padlinkd"; do
  if [ -f "$f" ]; then guard "$f"; rm -f "$f"; echo "  已删除 $f"; fi
done

if [ "$PURGE" = "1" ]; then
  echo "== 清除配对 token 与状态 =="
  for p in "$STATE_DIR"; do
    if [ -e "$p" ]; then guard "$p"; rm -rf "$p"; echo "  已删除 ${p}（下次需重新配对）"; fi
  done
  if [ "$KEEP_IDENTITY" = "0" ]; then
    echo "== 移除本机签名身份（下次安装会生成新证书 → 权限需重新授权） =="
    SIGNER=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/signing-identity.sh
    if [ -f "$SIGNER" ]; then
      sh "$SIGNER" remove || true
    else
      KC="$HOME/Library/Keychains/padlink-signing.keychain-db"
      [ -f "$KC" ] && guard "$KC" && security delete-keychain "$KC" 2>/dev/null && echo "  已删除 $KC" || true
      rm -f "$STATE_DIR/../padlink/signing-keychain.pass" 2>/dev/null || true
    fi
  else
    echo "== 保留签名身份（辅助功能授权下次仍然有效） =="
  fi
fi

if [ -f "$LOG" ]; then guard "$LOG"; mv -f "$LOG" "$LOG.removed" 2>/dev/null && echo "== 日志已改名: $LOG.removed ==" || true
fi

echo ""
echo "卸载完成。"
if [ "$PURGE" = "1" ] && [ "$KEEP_IDENTITY" = "0" ]; then
  echo "提示：系统设置 → 隐私与安全性 里的旧条目（辅助功能/本地网络）可能仍显示 PadLink，"
  echo "      那是残留记录，删除与否都不影响下次安装（新证书会被视为新应用）。"
fi
