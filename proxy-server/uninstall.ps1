# Local LFM DLP Proxy — uninstaller (run elevated). Reverts install.ps1:
# stops + deletes the service, removes the logon task, strips the hosts-file
# redirect block, removes the CA from the trust store, and (optionally) deletes
# the data tree. After this, api.anthropic.com resolves normally again.

[CmdletBinding()]
param(
    [string]$ServiceName = "LocalLfmDlpProxy",
    [string]$InstallRoot = (Join-Path $env:ProgramData "LocalLfmDlpProxy"),
    [string]$CaSubjectMatch = "*Local LFM DLP Proxy CA*",
    [switch]$KeepData   # keep %ProgramData%\LocalLfmDlpProxy (config, audit DB, CA files)
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
Write-Host "== Local LFM DLP Proxy uninstaller ==" -ForegroundColor Cyan

# 1. Stop + delete the service.
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    & sc.exe stop $ServiceName | Out-Null
    Start-Sleep -Seconds 1
    & sc.exe delete $ServiceName | Out-Null
    Write-Host "Service '$ServiceName' removed."
} else {
    Write-Host "Service '$ServiceName' not present."
}

# 2. Remove the sidecar logon task.
$taskName = "LocalLfmDlpProxy-Sidecar"
if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    Write-Host "Logon task '$taskName' removed."
}

# 3. Strip the hosts-file redirect block (in case the service left it behind).
$hosts = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
$begin = "# >>> LocalLfmDlpProxy >>>"
$end = "# <<< LocalLfmDlpProxy <<<"
if (Test-Path $hosts) {
    $content = Get-Content -Raw $hosts
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
        # Use CRLF (hosts file convention) and drop trailing blank lines.
        $joined = ($out -join "`r`n").TrimEnd("`r", "`n") + "`r`n"
        Set-Content -Path $hosts -Value $joined -NoNewline -Encoding ascii
        Write-Host "Removed hosts-file redirect block."
    } else {
        Write-Host "No hosts-file redirect block present."
    }
}

# 4. Remove the CA from the machine trust store.
$removed = 0
Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.Subject -like $CaSubjectMatch } | ForEach-Object {
    Remove-Item $_.PSPath -Force
    $removed++
}
Write-Host "Removed $removed CA certificate(s) from LocalMachine\Root."

# 5. Remove the data tree.
if (-not $KeepData -and (Test-Path $InstallRoot)) {
    Remove-Item -Recurse -Force $InstallRoot
    Write-Host "Removed data tree: $InstallRoot"
} elseif ($KeepData) {
    Write-Host "Kept data tree (-KeepData): $InstallRoot"
}

Write-Host ""
Write-Host "Uninstall complete. api.anthropic.com now resolves normally." -ForegroundColor Green
