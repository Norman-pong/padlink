#!/bin/sh
# PadLink macOS 安装脚本（用户级 LaunchAgent；全程无需 sudo）。
# 用法：解压 darwin release tar.gz 后在包根目录执行 ./install.sh
#
# 形态说明：daemon 装在 ~/Applications/PadLink.app 里（app bundle），LaunchAgent 指向
# bundle 内的可执行文件。做成 bundle 是为了「辅助功能」授权可靠且只需一次：
# 裸 CLI 二进制在系统设置里手动添加的条目经常不生效，且授权与代码哈希纠缠（重建即失效）；
# app bundle 是系统预期形态，条目按 bundle id + 签名证书记账，跨版本更新保持有效。
set -eu

SRC=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN_DIR="$HOME/.local/bin"
APP_DIR="$HOME/Applications"
APP="$APP_DIR/PadLink.app"
MACOS_DIR="$APP/Contents/MacOS"
AGENT_DIR="$HOME/Library/LaunchAgents"
LABEL=com.zhimingcool.padlink
PLIST="$AGENT_DIR/$LABEL.plist"
LOG="$HOME/Library/Logs/padlinkd.log"

mkdir -p "$MACOS_DIR" "$AGENT_DIR" "$HOME/Library/Logs"
install -m 0755 "$SRC/padlinkd" "$MACOS_DIR/padlinkd"
install -m 0755 "$SRC/padlinkctl" "$BIN_DIR/padlinkctl"
echo "已安装 PadLink.app（daemon）到 ${APP}，padlinkctl 到 $BIN_DIR"

# Info.plist：按本机取到的 daemon 版本落盘（缺 -version 支持时回落 1.0.0）
VERSION=$("$MACOS_DIR/padlinkd" -version 2>/dev/null || echo 1.0.0)
TPL="$SRC/packaging/macos/PadLink.app.Info.plist"
[ -f "$TPL" ] || { echo "缺少模板 $TPL" >&2; exit 1; }
sed -e "s|__VERSION__|$VERSION|g" "$TPL" > "$APP/Contents/Info.plist"
echo "已写入 Info.plist（CFBundleIdentifier=com.zhimingcool.padlink，版本 ${VERSION}）"

# 稳定签名身份：让「辅助功能」只授权一次，之后更新不再重新授权。
SIGNER="$SRC/packaging/macos/signing-identity.sh"
if [ -f "$SIGNER" ] && sh "$SIGNER" sign "$APP"; then
  sh "$SIGNER" verify "$APP" | sed -n 's/^    授权要求: /辅助功能授权要求: /p'
else
  echo "警告：稳定签名失败，本次为 ad-hoc 签名——下次更新后需要重新授权辅助功能。" >&2
fi

# Gatekeeper：浏览器下载的包带 quarantine 属性（curl 下载无），先去除
xattr -dr com.apple.quarantine "$APP" 2>/dev/null || true
xattr -d com.apple.quarantine "$BIN_DIR/padlinkctl" 2>/dev/null || true

# plist 为模板：ProgramArguments 须绝对路径，安装时按本机 $HOME 落盘
sed -e "s|__PADLINKD_PATH__|$MACOS_DIR/padlinkd|" -e "s|__HOME__|$HOME|" \
  "$SRC/packaging/macos/$LABEL.plist" > "$PLIST"

UID_NUM=$(id -u)
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_NUM" "$PLIST"
launchctl kickstart "gui/$UID_NUM/$LABEL"

if [ -f "$BIN_DIR/padlinkd" ]; then
  echo ""
  echo "注意：检测到旧版裸二进制 $BIN_DIR/padlinkd（本版 daemon 已移入 ${APP}）。"
  echo "      它可能仍在「辅助功能」列表里，建议删除该文件并从列表移除对应条目，避免混淆："
  echo "      rm $BIN_DIR/padlinkd"
fi

echo ""
echo "padlinkd 已通过 launchd 启动（日志：${LOG}；launchctl print gui/${UID_NUM}/${LABEL} 查看状态）。"
echo ""
echo "首次运行须完成两项系统授权（各弹一次窗，按提示勾选即可）："
echo "  1) 辅助功能：系统设置 → 隐私与安全性 → 辅助功能 → 勾选 **PadLink**"
echo "     （daemon 启动时会弹系统授权框，点「打开系统设置」后开启 PadLink 即可；"
echo "      不授权则键鼠注入被系统静默丢弃，daemon 会驻留等待，授权后自动继续）"
echo "     ★ 只需授权这一次：PadLink.app 带本机稳定签名身份，后续更新不会失效。"
echo "  2) 传入连接：弹窗「padlinkd 要接受传入网络连接吗？」→ 允许（局域网发现/会话端口 53021）"
echo ""
echo "授权完成后用 padlinkctl status 验证；手机端搜索不到主机时可手动添加本机 IP。"
