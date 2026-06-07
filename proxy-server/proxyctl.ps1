# PromptGate — day-to-day control helper.
#
#   .\proxyctl.ps1 status     # service + sidecar/web tasks + hosts-redirect state
#   .\proxyctl.ps1 start      # start the service (supervisor brings up sidecar + admin UI)
#   .\proxyctl.ps1 stop       # stop the service (tears down sidecar + admin UI too)
#   .\proxyctl.ps1 restart    # stop then start
#   .\proxyctl.ps1 logs       # tail the service log
#
# The service owns the full lifecycle: starting it triggers the user-session
# sidecar + admin UI tasks; stopping it ends them. start/stop/restart need an
# elevated shell; status/logs do not. Start the service only while logged in
# (the sidecar needs your interactive session for the iGPU).

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet("status", "start", "stop", "restart", "logs")]
    [string]$Command = "status",
    [string]$ServiceName = "PromptGate",
    [string]$InstallRoot = (Join-Path $env:ProgramData "PromptGate"),
    [int]$Tail = 40
)

$ErrorActionPreference = "Stop"
$sidecarTask = "PromptGate-Sidecar"
$webTask = "PromptGate-WebUI"
$logFile = Join-Path $InstallRoot "logs\proxy.log"
$hosts = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"

function Show-TaskState([string]$name, [string]$label) {
    $task = Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
    if ($task) {
        $info = Get-ScheduledTaskInfo -TaskName $name -ErrorAction SilentlyContinue
        Write-Host ("{0}: {1} (last result {2})" -f $label, $task.State, $info.LastTaskResult)
    } else {
        Write-Host ("{0}: not installed" -f $label)
    }
}

function Show-Status {
    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    Write-Host ("Service {0}: {1}" -f $ServiceName, ($(if ($svc) { $svc.Status } else { "not installed" })))
    Show-TaskState $sidecarTask "Sidecar task"
    Show-TaskState $webTask "Web UI task"
    $redirect = (Test-Path $hosts) -and ((Get-Content -Raw $hosts) -match "PromptGate")
    Write-Host ("Hosts redirect active: {0}" -f $redirect)
    try {
        $h = Invoke-WebRequest -Uri "http://127.0.0.1:8791/health" -UseBasicParsing -TimeoutSec 2
        Write-Host ("Sidecar /health: {0}" -f $h.StatusCode)
    } catch {
        Write-Host "Sidecar /health: unreachable"
    }
    try {
        $w = Invoke-WebRequest -Uri "http://127.0.0.1:3939" -UseBasicParsing -TimeoutSec 2
        Write-Host ("Admin UI: {0}" -f $w.StatusCode)
    } catch {
        Write-Host "Admin UI: unreachable"
    }
}

switch ($Command) {
    "status" { Show-Status }
    "start" {
        Start-Service -Name $ServiceName
        Show-Status
    }
    "stop" {
        Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Show-Status
    }
    "restart" {
        Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 1
        Start-Service -Name $ServiceName
        Show-Status
    }
    "logs" {
        if (Test-Path $logFile) { Get-Content -Path $logFile -Tail $Tail -Wait }
        else { Write-Host "No log file at $logFile yet." }
    }
}
