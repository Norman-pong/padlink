#!/bin/sh
# PadLink macOS 安装脚本（用户级 LaunchAgent；全程无需 sudo）。
# 用法：解压 darwin release tar.gz 后在包根目录执行 ./install.sh
set -eu

SRC=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN_DIR="$HOME/.local/bin"
AGENT_DIR="$HOME/Library/LaunchAgents"
LABEL=com.zhimingcool.padlink
PLIST="$AGENT_DIR/$LABEL.plist"
LOG="$HOME/Library/Logs/padlinkd.log"

mkdir -p "$BIN_DIR" "$AGENT_DIR" "$HOME/Library/Logs"
install -m 0755 "$SRC/padlinkd" "$BIN_DIR/padlinkd"
install -m 0755 "$SRC/padlinkctl" "$BIN_DIR/padlinkctl"
echo "已安装 padlinkd / padlinkctl 到 $BIN_DIR"

# 稳定签名身份：让「辅助功能」只授权一次，之后更新二进制不再重新授权。
# Go 默认的 ad-hoc（linker-signed）签名在 TCC 里按 cdhash 记账，重建即失效；本机自签身份按
# 「identifier + 证书哈希」记账，与代码内容无关，故长期有效（详见 signing-identity.sh 注释）。
SIGNER="$SRC/packaging/macos/signing-identity.sh"
if [ -f "$SIGNER" ] && sh "$SIGNER" sign "$BIN_DIR/padlinkd" "$BIN_DIR/padlinkctl"; then
  sh "$SIGNER" verify "$BIN_DIR/padlinkd" | sed -n 's/^    授权要求: /辅助功能授权要求: /p'
else
  echo "警告：稳定签名失败，本次为 ad-hoc 签名——下次更新二进制后需要重新授权辅助功能。" >&2
fi

# Gatekeeper：浏览器下载的包带 quarantine 属性（curl 下载无），先去除
xattr -d com.apple.quarantine "$BIN_DIR/padlinkd" 2>/dev/null || true
xattr -d com.apple.quarantine "$BIN_DIR/padlinkctl" 2>/dev/null || true

# plist 为模板：ProgramArguments 须绝对路径，安装时按本机 $HOME 落盘
sed -e "s|__PADLINKD_PATH__|$BIN_DIR/padlinkd|" -e "s|__HOME__|$HOME|" \
  "$SRC/packaging/macos/$LABEL.plist" > "$PLIST"

UID_NUM=$(id -u)
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$UID_NUM" "$PLIST"
launchctl kickstart "gui/$UID_NUM/$LABEL"

echo ""
echo "padlinkd 已通过 launchd 启动（日志：${LOG}；launchctl print gui/${UID_NUM}/${LABEL} 查看状态）。"
echo ""
echo "首次运行须完成两项系统授权（各弹一次窗，按提示勾选即可）："
echo "  1) 辅助功能：系统设置 → 隐私与安全性 → 辅助功能 → 勾选 padlinkd"
echo "     （不授权则键鼠注入被系统静默丢弃；daemon 驻留等待授权，授权后自动继续）"
echo "     ★ 只需授权这一次：二进制带本机稳定签名身份，后续更新不会失效。"
echo "       若确实要重新授权（例如手动改了签名/换了钥匙串）：先删掉列表里的旧条目，"
echo "       再用 + 号按路径重新添加 $BIN_DIR/padlinkd。"
echo "  2) 传入连接：弹窗「padlinkd 要接受传入网络连接吗？」→ 允许（局域网发现/会话端口 53021）"
echo ""
echo "授权完成后用 padlinkctl status 验证；手机端搜索不到主机时可手动添加本机 IP。"
