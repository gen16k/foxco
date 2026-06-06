# Local LFM DLP Proxy — day-to-day control helper.
#
#   .\proxyctl.ps1 status     # service + sidecar task + hosts-redirect state
#   .\proxyctl.ps1 start      # start sidecar task (user session) then the service
#   .\proxyctl.ps1 stop       # stop the service (removes redirect) then the sidecar
#   .\proxyctl.ps1 restart    # stop then start
#   .\proxyctl.ps1 logs       # tail the service log
#
# start/stop/restart need an elevated shell (they control a service). status/logs
# do not.

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet("status", "start", "stop", "restart", "logs")]
    [string]$Command = "status",
    [string]$ServiceName = "LocalLfmDlpProxy",
    [string]$InstallRoot = (Join-Path $env:ProgramData "LocalLfmDlpProxy"),
    [int]$Tail = 40
)

$ErrorActionPreference = "Stop"
$taskName = "LocalLfmDlpProxy-Sidecar"
$logFile = Join-Path $InstallRoot "logs\proxy.log"
$hosts = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"

function Show-Status {
    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    Write-Host ("Service {0}: {1}" -f $ServiceName, ($(if ($svc) { $svc.Status } else { "not installed" })))
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($task) {
        $info = Get-ScheduledTaskInfo -TaskName $taskName
        Write-Host ("Sidecar task: {0} (last result {1})" -f $task.State, $info.LastTaskResult)
    } else {
        Write-Host "Sidecar task: not installed"
    }
    $redirect = (Test-Path $hosts) -and ((Get-Content -Raw $hosts) -match "LocalLfmDlpProxy")
    Write-Host ("Hosts redirect active: {0}" -f $redirect)
    try {
        $h = Invoke-WebRequest -Uri "http://127.0.0.1:8791/health" -UseBasicParsing -TimeoutSec 2
        Write-Host ("Sidecar /health: {0}" -f $h.StatusCode)
    } catch {
        Write-Host "Sidecar /health: unreachable"
    }
}

switch ($Command) {
    "status" { Show-Status }
    "start" {
        Start-ScheduledTask -TaskName $taskName
        Start-Service -Name $ServiceName
        Show-Status
    }
    "stop" {
        Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        Show-Status
    }
    "restart" {
        Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 1
        Start-ScheduledTask -TaskName $taskName
        Start-Service -Name $ServiceName
        Show-Status
    }
    "logs" {
        if (Test-Path $logFile) { Get-Content -Path $logFile -Tail $Tail -Wait }
        else { Write-Host "No log file at $logFile yet." }
    }
}
