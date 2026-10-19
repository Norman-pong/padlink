# PadLink Windows 一行安装：下载 tar.gz → SHA-256 校验 → 执行包内 install.ps1。
# 用法：irm https://raw.githubusercontent.com/Norman-pong/padlink/main/script/install.ps1 | iex
& {
  $ErrorActionPreference = "Stop"
  $repo = "Norman-pong/padlink"
  $tmp = $null
  $step = "检查环境"

  function Get-PadLinkFile {
    param([string]$Url, [string]$Destination)
    for ($attempt = 1; $attempt -le 3; $attempt++) {
      try {
        Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Destination -TimeoutSec 120
        return
      } catch {
        $status = [int]$_.Exception.Response.StatusCode
        if ($attempt -eq 3 -or ($status -ge 400 -and $status -lt 500 -and $status -ne 429)) { throw }
        Write-Warning "下载暂时失败，正在重试（$attempt/2）…"
        Start-Sleep -Seconds $attempt
      }
    }
  }

  try {
    if ($env:OS -ne "Windows_NT") { throw "请在 Windows PowerShell 中执行此脚本" }
    if (-not (Get-Command tar -ErrorAction SilentlyContinue)) { throw "未找到 tar，请使用 Windows 11 自带的 tar" }
    $arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
      "Arm64" { "arm64" }
      "X64" { "amd64" }
      default { throw "仅支持 Windows x64 和 ARM64" }
    }

    $step = "查询版本"
    $tag = $env:PADLINK_VERSION
    if (-not $tag) {
      $req = [System.Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
      $req.AllowAutoRedirect = $false
      $req.Timeout = 15000
      $resp = $req.GetResponse()
      try { $tag = $resp.Headers["Location"].Split("/")[-1] }
      finally { $resp.Close() }
    }
    if ($tag -notmatch "^v") { $tag = "v$tag" }
    if ($tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$') { throw "无效的版本号：$tag" }
    $ver = $tag.Substring(1)
    $pkg = "padlink_${ver}_windows_${arch}.tar.gz"
    $base = "https://github.com/$repo/releases/download/$tag"
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("padlink-install-" + [guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory $tmp | Out-Null

    $step = "下载安装包"
    Write-Host "Windows $arch · $tag · install.ps1"
    Write-Host "下载 $pkg …"
    Get-PadLinkFile "$base/$pkg" (Join-Path $tmp $pkg)
    Get-PadLinkFile "$base/checksums.txt" (Join-Path $tmp "checksums.txt")

    $step = "校验安装包"
    $hashes = @(Get-Content (Join-Path $tmp "checksums.txt") | ForEach-Object {
      $parts = $_.Trim() -split '\s+'
      if ($parts.Count -eq 2 -and $parts[1] -ceq $pkg) { $parts[0] }
    })
    if ($hashes.Count -ne 1 -or $hashes[0] -notmatch '^[0-9a-fA-F]{64}$') { throw "checksums.txt 中缺少唯一有效的 $pkg 校验值" }
    $got = (Get-FileHash (Join-Path $tmp $pkg) -Algorithm SHA256).Hash
    if ($got -ne $hashes[0]) { throw "SHA-256 校验失败，安装中止" }

    $step = "解压安装包"
    tar -xzf (Join-Path $tmp $pkg) -C $tmp
    if ($LASTEXITCODE -ne 0) { throw "tar 解压失败（退出码 $LASTEXITCODE）" }
    $installer = Join-Path $tmp "install.ps1"
    if (-not (Test-Path $installer -PathType Leaf)) { throw "安装包内缺少 install.ps1" }

    $step = "执行 install.ps1"
    Write-Host "SHA-256 校验通过，执行 install.ps1 …"
    & powershell -NoProfile -ExecutionPolicy Bypass -File $installer
    if ($LASTEXITCODE -ne 0) { throw "install.ps1 执行失败（退出码 $LASTEXITCODE）" }
    Write-Host "安装完成。重新打开终端后运行 padlinkctl status；手机返回发现页重新扫描。"
  } catch {
    throw "PadLink 安装失败（$step）：$($_.Exception.Message)"
  } finally {
    if ($tmp -and (Test-Path $tmp)) { Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue }
  }
}
