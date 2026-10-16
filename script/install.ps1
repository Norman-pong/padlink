# PadLink Windows 一行远程安装：解析最新 Release → 下载 zip → SHA-256 校验 → 执行包内 install.ps1。
# 用法：irm https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.ps1 | iex
$ErrorActionPreference = "Stop"
$repo = "Norman-pong/padlink"

# 版本：PADLINK_VERSION 环境变量可指定（vX.Y.Z），默认取 releases/latest 的 302 Location 末段
$tag = $env:PADLINK_VERSION
if (-not $tag) {
  $req = [System.Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
  $req.AllowAutoRedirect = $false
  $resp = $req.GetResponse()
  $loc = $resp.Headers["Location"]
  $resp.Close()
  if (-not $loc) { throw "无法解析最新版本号（releases/latest 跳转失败）" }
  $tag = $loc.Split("/")[-1]
}
$ver = $tag.TrimStart("v")
$arch = if ([Environment]::Is64BitOperatingSystem -and [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64) { "arm64" } else { "amd64" }
$pkg = "padlink_${ver}_windows_${arch}.tar.gz"
$base = "https://github.com/$repo/releases/download/$tag"

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("padlink-install-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force $tmp | Out-Null
try {
  Write-Host "下载 ${pkg}（${tag}）…"
  Invoke-WebRequest "$base/$pkg" -OutFile (Join-Path $tmp $pkg)
  Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt")

  $want = (Get-Content (Join-Path $tmp "checksums.txt") | Where-Object { $_ -match [regex]::Escape($pkg) }) -split '\s+' | Select-Object -First 1
  if (-not $want) { throw "checksums.txt 中未找到 $pkg" }
  $got = (Get-FileHash (Join-Path $tmp $pkg) -Algorithm SHA256).Hash.ToLower()
  if ($got -ne $want.ToLower()) { throw "SHA-256 校验失败（期望 $want 实得 $got），安装中止" }

  # Windows 11 自带 bsdtar，可直接解 tar.gz（Expand-Archive 只认 zip）
  tar -xzf (Join-Path $tmp $pkg) -C $tmp
  & (Join-Path $tmp "install.ps1")
  Write-Host "完成。手机端打开 padlink 搜索本机即可配对。"
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
