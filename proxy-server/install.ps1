# Local LFM DLP Proxy — one-time installer (run elevated, from this directory).
#
# Sets up transparent HTTPS interception:
#   1. builds proxy.exe and copies it + config + start.ps1 to %ProgramData%\LocalLfmDlpProxy
#   2. generates a Name-Constrained root CA and installs it into LocalMachine\Root
#   3. registers the proxy as an auto-start Windows service (runs as LocalSystem)
#   4. registers a logon Scheduled Task that runs the GPU sidecar in your user session
#
# It deliberately does NOT start the service now: starting it would immediately
# redirect api.anthropic.com -> 127.0.0.1 on this machine, which (until the sidecar
# is healthy) would fail-closed and could disrupt any Claude session running right
# now. The redirect activates on the next boot (auto-start) or when you start the
# service manually. Run uninstall.ps1 to revert everything.

[CmdletBinding()]
param(
    [string]$ServiceName = "LocalLfmDlpProxy",
    [string]$InstallRoot = (Join-Path $env:ProgramData "LocalLfmDlpProxy"),
    [ValidateSet("vulkan", "cpu")]
    [string]$Backend = "vulkan",
    [string]$Model = "LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M",
    [int]$LlamaPort = 8791,
    [switch]$StartNow   # opt-in: start the service immediately (will redirect api.anthropic.com now)
)

$ErrorActionPreference = "Stop"

function Assert-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p = New-Object Security.Principal.WindowsPrincipal($id)
    if (-not $p.IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)) {
        throw "install.ps1 must be run from an elevated (Administrator) PowerShell."
    }
}

Assert-Admin

$repoDir = $PSScriptRoot
$exeSrc = Join-Path $repoDir "proxy.exe"
$exeDst = Join-Path $InstallRoot "proxy.exe"
$cfgDst = Join-Path $InstallRoot "config.yaml"
$startDst = Join-Path $InstallRoot "start.ps1"
$caDir = Join-Path $InstallRoot "ca"
$caCrt = Join-Path $caDir "ca.crt"

Write-Host "== Local LFM DLP Proxy installer ==" -ForegroundColor Cyan
Write-Host "Install root: $InstallRoot"

# 1. Build proxy.exe.
Write-Host "Building proxy.exe..."
& go build -o $exeSrc .\cmd\proxy
if ($LASTEXITCODE -ne 0) { throw "go build failed" }

# 2. Create the data tree and lock down the CA directory (SYSTEM + Administrators only).
foreach ($d in @($InstallRoot, $caDir, (Join-Path $InstallRoot "state"), (Join-Path $InstallRoot "logs"))) {
    New-Item -ItemType Directory -Force -Path $d | Out-Null
}
Copy-Item $exeSrc $exeDst -Force
Copy-Item (Join-Path $repoDir "start.ps1") $startDst -Force
if (-not (Test-Path $cfgDst)) {
    Copy-Item (Join-Path $repoDir "config\config.example.yaml") $cfgDst
    Write-Host "Wrote default config: $cfgDst (edit as needed)"
}

# Restrict the CA dir: disable inheritance, grant only SYSTEM + Administrators.
& icacls $caDir /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "Administrators:(OI)(CI)F" | Out-Null
Write-Host "CA directory locked to SYSTEM + Administrators."

# 3. Generate (or reuse) the Name-Constrained CA, then trust it machine-wide.
Write-Host "Generating interception CA..."
& $exeDst -config $cfgDst -init-ca | Out-Null
if (-not (Test-Path $caCrt)) { throw "CA generation did not produce $caCrt" }
Write-Host "Installing CA into Cert:\LocalMachine\Root ..."
Import-Certificate -FilePath $caCrt -CertStoreLocation Cert:\LocalMachine\Root | Out-Null

# 4. Register the Windows service (LocalSystem, auto-start) with restart recovery.
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    Write-Host "Service $ServiceName already exists; reconfiguring."
    & sc.exe stop $ServiceName | Out-Null
    & sc.exe delete $ServiceName | Out-Null
    Start-Sleep -Seconds 1
}
$binPath = '"{0}" -config "{1}"' -f $exeDst, $cfgDst
New-Service -Name $ServiceName -BinaryPathName $binPath -DisplayName "Local LFM DLP Proxy" `
    -Description "Inspects outbound Claude/Anthropic API traffic for sensitive-data egress (DLP)." `
    -StartupType Automatic | Out-Null
# Restart on crash so the hosts redirect is reconciled rather than left dangling.
& sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null
Write-Host "Service '$ServiceName' registered (auto-start, restart-on-failure)."

# 5. Register the logon Scheduled Task that runs the GPU sidecar in the user session.
$taskName = "LocalLfmDlpProxy-Sidecar"
$user = "$env:USERDOMAIN\$env:USERNAME"
$psArgs = '-NoProfile -ExecutionPolicy Bypass -File "{0}" -SidecarOnly -Backend {1} -LlamaPort {2} -Model "{3}"' -f `
    $startDst, $Backend, $LlamaPort, $Model
$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $psArgs -WorkingDirectory $InstallRoot
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero)
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
Write-Host "Logon task '$taskName' registered (runs the Vulkan sidecar in your session)."

Write-Host ""
Write-Host "Install complete." -ForegroundColor Green
Write-Host "The hosts redirect + HTTPS interception activate when the service runs."
if ($StartNow) {
    Write-Host "Starting sidecar task and service now (-StartNow)..." -ForegroundColor Yellow
    Start-ScheduledTask -TaskName $taskName
    Start-Service -Name $ServiceName
    Write-Host "Started. api.anthropic.com is now routed through the proxy."
} else {
    Write-Host "NOT started now (current Claude sessions stay untouched)." -ForegroundColor Yellow
    Write-Host "It will auto-start on next boot/logon, or start manually with:"
    Write-Host "    Start-ScheduledTask -TaskName $taskName    # sidecar (user session)"
    Write-Host "    Start-Service -Name $ServiceName           # proxy (redirect + 443)"
}
