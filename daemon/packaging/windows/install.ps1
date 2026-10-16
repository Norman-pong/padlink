# PadLink Windows 安装/升级脚本（用户级；防火墙一步需管理员，失败给出指引不中断）。
# 用法：解压 release zip 后在包根目录执行 .\install.ps1
# 形态说明：二进制装在 %LOCALAPPDATA%\padlink\bin，HKCU Run 键登录自启（免管理员、
# 不做 SCM 服务——Session 0 隔离与桌面注入冲突，见 docs/PLAN-WINDOWS.md）。
$ErrorActionPreference = "Stop"

$pkg = $PSScriptRoot
$root = Join-Path $env:LOCALAPPDATA "padlink"
$bin = Join-Path $root "bin"
$logDir = Join-Path $root "logs"

Write-Host "PadLink 安装到 $bin"
New-Item -ItemType Directory -Force $bin, $logDir | Out-Null

# 升级场景：先停旧 daemon（镜像锁：运行中的 exe 不能覆盖，只能改名/先杀）。
# 用 Get-Process 守卫而非 taskkill 2>$null：ErrorActionPreference=Stop 下原生命令的
# stderr 重定向会被升格为终止错误（实测踩坑）。
if (Get-Process padlinkd -ErrorAction SilentlyContinue) {
  taskkill /f /im padlinkd.exe | Out-Null
}

# 运行中的 padlinkctl（padlinkctl update 场景）不能覆盖，改名腾位
foreach ($name in @("padlinkd.exe", "padlinkctl.exe")) {
  $dst = Join-Path $bin $name
  if (Test-Path $dst) {
    try { Copy-Item (Join-Path $pkg $name) $dst -Force }
    catch {
      Rename-Item $dst "$name.old" -Force
      Copy-Item (Join-Path $pkg $name) $dst -Force
      Write-Host "  $name 正在运行，旧版已改名为 $name.old（进程退出后可删）"
    }
  } else {
    Copy-Item (Join-Path $pkg $name) $dst -Force
  }
}

# 用户级 PATH（padlinkctl 全局可用；已含则跳过）
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$bin*") {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$bin", "User")
  Write-Host "已将 $bin 加入用户 PATH（新开的终端生效）"
}

# HKCU Run 登录自启（免管理员）
Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name "PadLink" -Value "`"$bin\padlinkd.exe`""
Write-Host "已设置登录自启（HKCU Run）"

# 防火墙放行 UDP/TCP 53021（入站发现与会话）：需管理员，失败给出手动命令
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if ($isAdmin) {
  netsh advfirewall firewall delete rule name="PadLink" >$null 2>netsh advfirewall firewall delete rule name="PadLink" | Out-Null 2>$null1
  netsh advfirewall firewall add rule name="PadLink" dir=in action=allow protocol=UDP localport=53021 | Out-Null
  netsh advfirewall firewall add rule name="PadLink" dir=in action=allow protocol=TCP localport=53021 | Out-Null
  Write-Host "防火墙已放行 53021（UDP/TCP 入站）"
} else {
  Write-Warning "防火墙规则需要管理员：请以管理员运行一次 PowerShell 执行下面两行（否则手机发现不了本机）："
  Write-Host '  netsh advfirewall firewall add rule name="PadLink" dir=in action=allow protocol=UDP localport=53021'
  Write-Host '  netsh advfirewall firewall add rule name="PadLink" dir=in action=allow protocol=TCP localport=53021'
}

# 启动 daemon（隐藏窗口；日志 %LOCALAPPDATA%\padlink\logs\padlinkd.log）
Start-Process -WindowStyle Hidden (Join-Path $bin "padlinkd.exe")
Write-Host "padlinkd 已启动（日志：$logDir\padlinkd.log）"
Write-Host ""
Write-Host "手机端打开 padlink 搜索本机即可配对；padlinkctl status 验证。"
