#!/bin/sh
# PadLink 一行远程安装：解析最新 Release → 按平台下载 tar.gz → SHA-256 校验 → 执行包内 install.sh。
# 用法：curl -fsSL https://raw.githubusercontent.com/Norman-pong/padlink/main/daemon/packaging/install-remote.sh | sh
# 安装逻辑单源在 release 包内的 install.sh，本脚本只负责取包与验签，不复写安装步骤。
set -eu

REPO="Norman-pong/padlink"
# 弱网兜底：连接 15s 上限 + 瞬时错误重试 2 次（404 不重试）
CURL="curl -fsSL --connect-timeout 15 --retry 2"

die() {
  echo "install-remote: $*" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || die "未找到 curl（Ubuntu：sudo apt install curl）"

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) die "不支持的系统：$(uname -s)（支持 Ubuntu GNOME Wayland 与 macOS）" ;;
esac
case "$(uname -m)" in
  x86_64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) die "不支持的架构：$(uname -m)" ;;
esac

# 版本号取自 releases/latest 的 302 Location 末段（不依赖 jq）
tag=$(curl -fsSI --connect-timeout 15 --retry 2 "https://github.com/$REPO/releases/latest" |
  sed -n 's/^[Ll]ocation: .*\/tag\/\([^[:space:]\r]*\).*/\1/p')
[ -n "$tag" ] || die "无法解析最新版本号（releases/latest 跳转失败）"
ver="${tag#v}"

pkg="padlink_${ver}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "下载 $pkg（$tag）…"
$CURL "$base/$pkg" -o "$tmp/$pkg" || die "下载失败：$base/$pkg（该平台的安装包可能尚未发布）"
$CURL "$base/checksums.txt" -o "$tmp/checksums.txt" || die "下载 checksums.txt 失败"

grep "  $pkg\$" "$tmp/checksums.txt" > "$tmp/check.line" || die "checksums.txt 中未找到 $pkg"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && sha256sum -c check.line >/dev/null) || die "SHA-256 校验失败，安装中止"
else
  (cd "$tmp" && shasum -a 256 -c check.line >/dev/null) || die "SHA-256 校验失败，安装中止"
fi

mkdir "$tmp/x"
tar -xzf "$tmp/$pkg" -C "$tmp/x"
sh "$tmp/x/install.sh"
echo "完成。手机端打开 padlink 搜索本机即可配对。"
