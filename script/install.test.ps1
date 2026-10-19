param([string]$Scenario)

# 网络、解包和包内安装均替换为临时目录中的测试操作，不修改实际安装。
if (-not $Scenario) {
  $shell = (Get-Process -Id $PID).Path
  foreach ($case in @('success', 'restricted-policy', 'retry', 'download-failed', 'checksum-mismatch', 'missing-checksum', 'duplicate-checksum', 'unpack-failed', 'missing-installer', 'installer-failed', 'invalid-version')) {
    & $shell -NoProfile -ExecutionPolicy Bypass -File $PSCommandPath -Scenario $case
    if ($LASTEXITCODE -ne 0) { throw "安装脚本回归失败：$case" }
  }
  return
}

if ($Scenario -eq 'restricted-policy' -and [Environment]::OSVersion.Platform -ne 'Win32NT') {
  Write-Host 'SKIP restricted-policy (requires Windows)'
  return
}

$ErrorActionPreference = 'Continue'
$env:OS = 'Windows_NT'
$env:PADLINK_VERSION = if ($Scenario -eq 'invalid-version') { '../invalid' } else { '1.6.1' }
$root = Join-Path ([IO.Path]::GetTempPath()) ('padlink-bootstrap-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory $root | Out-Null
$env:PADLINK_TEST_MARKER = Join-Path $root 'installed'
$global:PadLinkTestDownloads = 0
$global:PadLinkTestDownloadDir = $null

function Invoke-WebRequest {
  param([switch]$UseBasicParsing, [string]$Uri, [string]$OutFile, [int]$TimeoutSec)
  $global:PadLinkTestDownloadDir = Split-Path $OutFile
  if ($Uri.EndsWith('.tar.gz')) {
    $global:PadLinkTestDownloads++
    if ($Scenario -eq 'download-failed' -or ($Scenario -eq 'retry' -and $global:PadLinkTestDownloads -eq 1)) { throw '模拟网络中断' }
    [IO.File]::WriteAllText($OutFile, 'test archive')
  } else {
    $asset = Get-ChildItem $global:PadLinkTestDownloadDir -Filter '*.tar.gz' | Select-Object -First 1
    $hash = (Get-FileHash $asset.FullName -Algorithm SHA256).Hash
    if ($Scenario -eq 'checksum-mismatch') { $hash = '0' * 64 }
    $line = "$hash  $($asset.Name)"
    if ($Scenario -eq 'missing-checksum') { $line = "$hash  other.tar.gz" }
    if ($Scenario -eq 'duplicate-checksum') { $line = "$line`n$line" }
    [IO.File]::WriteAllText($OutFile, $line)
  }
}

function Start-Sleep { param([int]$Seconds) }

function powershell {
  & (Get-Process -Id $PID).Path @args
  $global:LASTEXITCODE = $LASTEXITCODE
}

function tar {
  $global:LASTEXITCODE = 0
  if ($Scenario -eq 'unpack-failed') { $global:LASTEXITCODE = 9; return }
  if ($Scenario -eq 'missing-installer') { return }
  $body = if ($Scenario -eq 'installer-failed') { "throw '模拟安装失败'" } else { 'Set-Content -Path $env:PADLINK_TEST_MARKER -Value done' }
  [IO.File]::WriteAllText((Join-Path $args[-1] 'install.ps1'), $body)
}

try {
  if ($Scenario -eq 'restricted-policy' -and [Environment]::OSVersion.Platform -eq 'Win32NT') {
    Set-ExecutionPolicy -Scope Process -ExecutionPolicy Restricted -Force
  }
  $failure = $null
  try { Invoke-Expression (Get-Content (Join-Path $PSScriptRoot 'install.ps1') -Raw) } catch { $failure = $_ }
  $expectSuccess = $Scenario -in @('success', 'restricted-policy', 'retry')
  if ($expectSuccess -and $failure) { throw $failure }
  if (-not $expectSuccess -and -not $failure) { throw '失败场景不得报告成功' }
  if ((Test-Path $env:PADLINK_TEST_MARKER) -ne $expectSuccess) { throw '安装步骤是否执行不符合预期' }
  if ($failure -and $failure.ToString() -notmatch 'PadLink 安装失败') { throw "缺少阶段错误上下文：$failure" }
  if ($Scenario -eq 'retry' -and $global:PadLinkTestDownloads -ne 2) { throw '瞬时错误未按预期重试' }
  if ($Scenario -eq 'download-failed' -and $global:PadLinkTestDownloads -ne 3) { throw '重试次数未受限' }
  if ($global:PadLinkTestDownloadDir -and (Test-Path $global:PadLinkTestDownloadDir)) { throw '临时下载目录未清理' }
  if ($ErrorActionPreference -ne 'Continue') { throw '脚本泄漏了调用方的错误处理设置' }
  Write-Host "PASS $Scenario"
} catch {
  Write-Error $_
  exit 1
} finally {
  Remove-Item $root -Recurse -Force
}
