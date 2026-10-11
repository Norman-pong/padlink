#!/bin/sh
# PadLink macOS 稳定签名身份：让「辅助功能」授权一次后长期有效。
#
# 背景（2026-10-11 实测）：Go 产出的二进制默认是 ad-hoc（linker-signed）签名，TCC 对它的授权
# 记的是 **cdhash**（代码哈希）——每次重新构建 cdhash 都变，授权随即失效，用户被迫反复去系统
# 设置重新勾选（实测：替换二进制后 daemon 卡在「仍在等待辅助功能授权」轮询上）。
#
# 方案：本机生成一张自签的代码签名证书（**不碰登录钥匙串、不需要管理员、不联网、不需要信任
# 设置**），之后每次构建都用它签名。此时二进制的授权要求是
#   identifier "com.zhimingcool.padlink" and certificate leaf = H"<证书 SHA-1>"
# ——与代码内容无关，因此「授权一次，后续更新二进制不再重新授权」。
# 签名按**证书哈希**选身份：同名证书与「未信任」状态都不影响（实测自签证书无需加入信任设置，
# codesign 能正常签名，`codesign --verify` 报 valid on disk + satisfies its DR）。
#
# 用法：
#   signing-identity.sh ensure              # 幂等创建/复用签名身份（缺什么补什么）
#   signing-identity.sh sign <二进制>...    # 用该身份签名（可多次）
#   signing-identity.sh verify <二进制>...  # 打印签名校验与授权要求
#   signing-identity.sh check <旧> <新>     # 用旧二进制的授权要求校验新二进制（更新是否免授权）
#   signing-identity.sh remove              # 回滚：删除本机签名身份（不影响已装二进制）
set -eu

CN="PadLink Local Signing"
KC_DIR="$HOME/Library/Keychains"
KC="$KC_DIR/padlink-signing.keychain-db"
PW_FILE="$HOME/.config/padlink/signing-keychain.pass"
IDENT_PREFIX="com.zhimingcool.padlink"

ident_for() {
  # padlinkd → com.zhimingcool.padlink；padlinkctl → com.zhimingcool.padlink.ctl
  base=$(basename "$1")
  case "$base" in
    padlinkd) echo "$IDENT_PREFIX" ;;
    padlinkctl) echo "$IDENT_PREFIX.ctl" ;;
    *) echo "$IDENT_PREFIX.$base" ;;
  esac
}

cert_hash() {
  # 证书 SHA-1（小写）。按哈希选身份可避免同名歧义，也不受「未信任」状态影响。
  security find-certificate -a -c "$CN" -Z "$KC" 2>/dev/null |
    sed -n 's/^SHA-1 hash: //p' | head -1 | tr 'A-Z' 'a-z'
}

ensure_keychain() {
  umask 077
  mkdir -p "$(dirname "$PW_FILE")" "$KC_DIR"
  [ -f "$PW_FILE" ] || LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32 > "$PW_FILE"
  PW=$(cat "$PW_FILE")

  [ -f "$KC" ] || security create-keychain -p "$PW" "$KC"
  security unlock-keychain -p "$PW" "$KC"

  # 幂等：已有本 CN 的证书就直接复用。注意不能用 find-identity -v——未信任的自签身份会被
  # 判为 invalid（CSSMERR_TP_NOT_TRUSTED），据此判断会把已有身份误当缺失而重复创建同名证书。
  [ -n "$(cert_hash)" ] && return 0

  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT
  # keyUsage/extendedKeyUsage 必须含 codeSigning，否则 codesign 拒用该身份
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
    -keyout "$TMP/key.pem" -out "$TMP/cert.pem" -subj "/CN=$CN" \
    -addext "basicConstraints=critical,CA:FALSE" \
    -addext "keyUsage=critical,digitalSignature" \
    -addext "extendedKeyUsage=critical,codeSigning" >/dev/null 2>&1
  openssl pkcs12 -export -inkey "$TMP/key.pem" -in "$TMP/cert.pem" \
    -out "$TMP/id.p12" -passout pass:"$PW" -name "$CN" >/dev/null 2>&1

  # -T /usr/bin/codesign + partition-list：签名时不再弹「codesign 想使用密钥」
  security import "$TMP/id.p12" -k "$KC" -P "$PW" -T /usr/bin/codesign >/dev/null
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$PW" "$KC" >/dev/null
  echo "已创建本机签名身份「${CN}」（钥匙串：${KC}）"
}

ensure() {
  ensure_keychain
  H=$(cert_hash)
  [ -n "$H" ] || { echo "签名身份创建失败：$KC 中没有 $CN 证书" >&2; exit 1; }
  echo "签名身份就绪：证书 SHA-1 = $H"
}

sign() {
  [ $# -gt 0 ] || { echo "用法: $0 sign <二进制|app bundle>..." >&2; exit 2; }
  ensure_keychain
  H=$(cert_hash)
  [ -n "$H" ] || { echo "签名身份缺失，请先运行 $0 ensure" >&2; exit 1; }
  for f in "$@"; do
    case "$f" in
      *.app)
        # app bundle：先签内部可执行文件，再用同一 identifier 签 bundle 本体（封套）
        plist="$f/Contents/Info.plist"
        [ -f "$plist" ] || { echo "缺少 $plist" >&2; exit 1; }
        bid=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist" 2>/dev/null) \
          || { echo "读不到 CFBundleIdentifier: $plist" >&2; exit 1; }
        exe=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$plist" 2>/dev/null) \
          || { echo "读不到 CFBundleExecutable: $plist" >&2; exit 1; }
        [ -f "$f/Contents/MacOS/$exe" ] || { echo "缺少 $f/Contents/MacOS/$exe" >&2; exit 1; }
        codesign --force --sign "$H" --keychain "$KC" --identifier "$bid" --timestamp=none \
          "$f/Contents/MacOS/$exe" >/dev/null
        codesign --force --sign "$H" --keychain "$KC" --identifier "$bid" --timestamp=none "$f" >/dev/null
        echo "已签名: ${f}（bundle id=${bid}）"
        ;;
      *)
        [ -f "$f" ] || { echo "文件不存在: $f" >&2; exit 1; }
        codesign --force --sign "$H" --keychain "$KC" \
          --identifier "$(ident_for "$f")" --timestamp=none "$f" >/dev/null
        echo "已签名: ${f}（identifier=$(ident_for "$f")）"
        ;;
    esac
  done
}

req_of() {
  codesign -d -r- "$1" 2>/dev/null | sed -n 's/^designated => //p'
}

verify() {
  for f in "$@"; do
    echo "--- $f"
    codesign --verify --verbose=1 "$f" 2>&1 | sed 's/^/    /'
    echo "    授权要求: $(req_of "$f")"
  done
}

check() {
  # 关键验证：新二进制是否满足旧二进制记录的授权要求（满足 = 更新后无需重新授权）。
  # codesign 的行内要求须加 "=" 前缀，否则会被当成要求文件路径（报 No such file / invalid）。
  [ $# -eq 2 ] || { echo "用法: $0 check <已授权的旧二进制> <更新后的新二进制>" >&2; exit 2; }
  req=$(req_of "$1")
  [ -n "$req" ] || { echo "读不到 $1 的授权要求" >&2; exit 1; }
  echo "旧二进制的授权要求: $req"
  if codesign --verify -R "=$req" "$2" 2>/dev/null; then
    echo "结论：新二进制满足该要求 → 更新后无需重新授权 OK"
  else
    echo "结论：新二进制不满足该要求 → 更新后会要求重新授权 FAIL" >&2
    exit 1
  fi
}

remove() {
  security delete-keychain "$KC" 2>/dev/null && echo "已删除签名钥匙串 $KC" || echo "（无签名钥匙串）"
  rm -f "$PW_FILE"
}

cmd=${1:-ensure}
shift 2>/dev/null || true
case "$cmd" in
  ensure) ensure ;;
  sign) sign "$@" ;;
  verify) verify "$@" ;;
  check) check "$@" ;;
  remove) remove ;;
  *) echo "用法: $0 ensure|sign|verify|check|remove" >&2; exit 2 ;;
esac
