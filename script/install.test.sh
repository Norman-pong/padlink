#!/bin/sh
# 下载源与包内安装器使用本地夹具，不触碰实际安装。
set -eu
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
mkdir "$root/bin"
cat > "$root/bin/uname" <<'MOCK'
#!/bin/sh
case "$1" in -s) echo "${PADLINK_TEST_OS:-Linux}" ;; -m) echo x86_64 ;; esac
MOCK
cat > "$root/bin/curl" <<'MOCK'
#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    https://*) url=$1; shift ;;
    *) shift ;;
  esac
done
cp "$PADLINK_TEST_ASSETS/${url##*/}" "$output"
MOCK
chmod +x "$root/bin/uname" "$root/bin/curl"
export PATH="$root/bin:$PATH"

for scenario in success checksum-mismatch missing-checksum duplicate-checksum unpack-failed missing-installer installer-failed invalid-version windows-shell; do
  case_dir="$root/$scenario"
  mkdir -p "$case_dir/pkg" "$case_dir/tmp"
  export PADLINK_TEST_ASSETS="$case_dir" PADLINK_TEST_MARKER="$case_dir/installed" TMPDIR="$case_dir/tmp"
  export PADLINK_VERSION=1.6.1 PADLINK_TEST_OS=Linux
  if [ "$scenario" = invalid-version ]; then PADLINK_VERSION='../invalid'; fi
  if [ "$scenario" = windows-shell ]; then PADLINK_TEST_OS=MINGW64_NT; fi
  if [ "$scenario" = missing-installer ]; then
    echo readme > "$case_dir/pkg/README"
  elif [ "$scenario" = installer-failed ]; then
    echo 'exit 7' > "$case_dir/pkg/install.sh"
  else
    echo 'echo done > "$PADLINK_TEST_MARKER"' > "$case_dir/pkg/install.sh"
  fi
  asset=padlink_1.6.1_linux_amd64.tar.gz
  (cd "$case_dir/pkg" && tar -czf "$case_dir/$asset" *)
  if [ "$scenario" = unpack-failed ]; then echo invalid > "$case_dir/$asset"; fi
  if command -v sha256sum >/dev/null 2>&1; then
    hash=$(sha256sum "$case_dir/$asset" | cut -d ' ' -f 1)
  else
    hash=$(shasum -a 256 "$case_dir/$asset" | cut -d ' ' -f 1)
  fi
  case "$scenario" in
    checksum-mismatch) hash=0000000000000000000000000000000000000000000000000000000000000000 ;;
    missing-checksum) asset=other.tar.gz ;;
  esac
  printf '%s  %s\n' "$hash" "$asset" > "$case_dir/checksums.txt"
  if [ "$scenario" = duplicate-checksum ]; then printf '%s  %s\n' "$hash" "$asset" >> "$case_dir/checksums.txt"; fi
  code=0
  sh "$script_dir/install.sh" > "$case_dir/output" 2>&1 || code=$?
  if [ "$scenario" = success ]; then
    [ "$code" -eq 0 ] && [ -f "$PADLINK_TEST_MARKER" ] || { cat "$case_dir/output"; exit 1; }
  else
    [ "$code" -ne 0 ] && [ ! -f "$PADLINK_TEST_MARKER" ] || { cat "$case_dir/output"; exit 1; }
    if grep -q '^完成。' "$case_dir/output"; then echo '失败场景不得报告完成'; exit 1; fi
  fi
  [ -z "$(ls -A "$case_dir/tmp")" ] || { echo '临时下载目录未清理'; exit 1; }
  echo "PASS $scenario"
done
