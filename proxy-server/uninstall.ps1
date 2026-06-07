# PromptGate — uninstaller (run elevated). Reverts install.ps1: stops + deletes the
# service, removes the RunOnDemand tasks, strips the hosts-file redirect block,
# removes the CA from the trust store, and (optionally) deletes the data tree. Also
# cleans up a prior "LocalLfmDlpProxy" install. After this, api.anthropic.com
# resolves normally again.

[CmdletBinding()]
param(
    [string]$ServiceName = "PromptGate",
    [string]$InstallRoot = (Join-Path $env:ProgramData "PromptGate"),
    [switch]$KeepData   # keep %ProgramData%\PromptGate (config, audit DB, CA files)
)

$ErrorActionPreference = "Continue"  # best-effort: remove as much as possible

function Assert-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $p = New-Object Security.Principal.WindowsPrincipal($id)
    if (-not $p.IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)) {
        throw "uninstall.ps1 must be run from an elevated (Administrator) PowerShell."
    }
}

Assert-Admin
Write-Host "== PromptGate uninstaller ==" -ForegroundColor Cyan

# 1. Stop + delete the service(s) — current and legacy.
foreach ($svc in @($ServiceName, "LocalLfmDlpProxy")) {
    if (Get-Service -Name $svc -ErrorAction SilentlyContinue) {
        & sc.exe stop $svc | Out-Null
        Start-Sleep -Seconds 1
        & sc.exe delete $svc | Out-Null
        Write-Host "Service '$svc' removed."
    }
}

# 2. Remove the RunOnDemand / legacy logon tasks.
foreach ($taskName in @("PromptGate-Sidecar", "PromptGate-WebUI", "LocalLfmDlpProxy-Sidecar")) {
    if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
        Write-Host "Task '$taskName' removed."
    }
}

# 3. Strip the hosts-file redirect block(s) — current and legacy markers.
$hosts = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
if (Test-Path $hosts) {
    $content = Get-Content -Raw $hosts
    $orig = $content
    foreach ($name in @("PromptGate", "LocalLfmDlpProxy")) {
        $begin = "# >>> $name >>>"
        $end = "# <<< $name <<<"
        if ($content -match [regex]::Escape($begin)) {
            $lines = $content -split "`r?`n"
            $out = New-Object System.Collections.Generic.List[string]
            $inBlock = $false
            foreach ($ln in $lines) {
                $t = $ln.Trim()
                if ($t -eq $begin) { $inBlock = $true; continue }
                if ($t -eq $end) { $inBlock = $false; continue }
                if (-not $inBlock) { $out.Add($ln) }
            }
            $content = ($out -join "`r`n").TrimEnd("`r", "`n") + "`r`n"
        }
    }
    if ($content -ne $orig) {
        Set-Content -Path $hosts -Value $content -NoNewline -Encoding ascii
        Write-Host "Removed hosts-file redirect block(s)."
    } else {
        Write-Host "No hosts-file redirect block present."
    }
}

# 4. Remove the CA(s) from the machine trust store — current and legacy subjects.
$removed = 0
Get-ChildItem Cert:\LocalMachine\Root | Where-Object {
    $_.Subject -like "*PromptGate CA*" -or $_.Subject -like "*Local LFM DLP Proxy CA*"
} | ForEach-Object {
    Remove-Item $_.PSPath -Force
    $removed++
}
Write-Host "Removed $removed CA certificate(s) from LocalMachine\Root."

# 5. Remove the data tree(s).
$roots = @($InstallRoot)
$legacyRoot = Join-Path $env:ProgramData "LocalLfmDlpProxy"
if ($legacyRoot -ne $InstallRoot) { $roots += $legacyRoot }
foreach ($root in $roots) {
    if (-not $KeepData -and (Test-Path $root)) {
        Remove-Item -Recurse -Force $root
        Write-Host "Removed data tree: $root"
    } elseif ($KeepData -and (Test-Path $root)) {
        Write-Host "Kept data tree (-KeepData): $root"
    }
}

Write-Host ""
Write-Host "Uninstall complete. api.anthropic.com now resolves normally." -ForegroundColor Green
