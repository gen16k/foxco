# PromptGate — one-time installer (run elevated, from this directory).
#
# Sets up transparent HTTPS interception with a single service-owned lifecycle:
#   1. builds proxy.exe and copies it + config + start.ps1 + web/ to %ProgramData%\PromptGate
#   2. generates a Name-Constrained root CA and installs it into LocalMachine\Root
#   3. registers the proxy as a MANUAL-start Windows service (runs as LocalSystem)
#   4. registers two RunOnDemand tasks (sidecar + admin UI) in your user session;
#      the service's supervisor starts them on service start and stops them on stop
#   5. migrates a prior "LocalLfmDlpProxy" install (keeps its CA + audit DB)
#
# Re-running is idempotent and brings an existing install up to date: it rebuilds +
# recopies the binary/start.ps1/web, and syncs the deployed config's
# inference.model + inference.profile to the installed -ModelLabel / -Profile
# (defaults: the akiFQC Conf-Extract JP GGUF + jp_confidential_extraction). The
# sidecar task is re-registered to load -Model. All other config settings are kept.
#
# The service is MANUAL start and is NOT started by this installer: starting it
# redirects api.anthropic.com -> 127.0.0.1, which would disrupt any Claude session
# running right now. Start it deliberately (while logged in) with:
#     Start-Service PromptGate     # brings up proxy + sidecar + admin UI together
# Stopping the service tears all three down. Run uninstall.ps1 to revert everything.

[CmdletBinding()]
param(
    [string]$ServiceName = "PromptGate",
    [string]$InstallRoot = (Join-Path $env:ProgramData "PromptGate"),
    [ValidateSet("vulkan", "cpu")]
    [string]$Backend = "vulkan",
    [string]$Model = "akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M",  # GGUF the sidecar loads (-hf ref or local .gguf path)
    [string]$ModelLabel = "LFM2.5-1.2B-JP-202606-Conf-Extract",                # inference.model label written into config (audit/UI only)
    [string]$Profile = "jp_confidential_extraction",                          # inference.profile (LFM I/O contract) written into config
    [int]$LlamaPort = 8791,
    [switch]$StartNow,   # opt-in: start the service immediately (will redirect api.anthropic.com now)
    [switch]$SkipBuild   # reuse an existing proxy.exe instead of building (CI / sandbox tests)
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
$sidecarTask = "PromptGate-Sidecar"
$webTask = "PromptGate-WebUI"
$webPort = 3939

Write-Host "== PromptGate installer ==" -ForegroundColor Cyan
Write-Host "Install root: $InstallRoot"

# 0. Migrate a prior "LocalLfmDlpProxy" install to "PromptGate" (option A: keep the
#    already-trusted CA + the audit DB; remove the old service + logon task). The
#    data move must happen BEFORE we create the new InstallRoot below.
$oldRoot = Join-Path $env:ProgramData "LocalLfmDlpProxy"
$oldService = "LocalLfmDlpProxy"
$oldTask = "LocalLfmDlpProxy-Sidecar"
if (Get-Service -Name $oldService -ErrorAction SilentlyContinue) {
    Write-Host "Migrating: removing old service '$oldService' ..."
    & sc.exe stop $oldService | Out-Null
    Start-Sleep -Seconds 1
    & sc.exe delete $oldService | Out-Null
}
if (Get-ScheduledTask -TaskName $oldTask -ErrorAction SilentlyContinue) {
    Write-Host "Migrating: removing old logon task '$oldTask' ..."
    Unregister-ScheduledTask -TaskName $oldTask -Confirm:$false
}
if ((Test-Path $oldRoot) -and -not (Test-Path $InstallRoot)) {
    Write-Host "Migrating data: $oldRoot -> $InstallRoot (keeps CA + audit DB)"
    Move-Item -Path $oldRoot -Destination $InstallRoot
}
# Strip any stale old-marker hosts block left behind by a prior crash.
$hostsPath = Join-Path $env:WINDIR "System32\drivers\etc\hosts"
if (Test-Path $hostsPath) {
    $h = Get-Content -Raw $hostsPath
    $stripped = [regex]::Replace($h, "(?ms)\r?\n?# >>> LocalLfmDlpProxy >>>.*?# <<< LocalLfmDlpProxy <<<\r?\n?", "")
    if ($stripped -ne $h) {
        Set-Content -Path $hostsPath -Value $stripped -NoNewline
        Write-Host "Migrating: stripped stale LocalLfmDlpProxy hosts block."
    }
}

# 1. Build proxy.exe (or, with -SkipBuild, reuse one built elsewhere — e.g. a host
#    build mapped into a Windows Sandbox, or a CI artifact, where Go is not present).
if ($SkipBuild) {
    if (-not (Test-Path $exeSrc)) {
        throw "-SkipBuild set but $exeSrc not found. Build it first: go build -o proxy.exe .\cmd\proxy"
    }
    Write-Host "Skipping build; using existing $exeSrc"
} else {
    Write-Host "Building proxy.exe..."
    & go build -o $exeSrc .\cmd\proxy
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
}

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

# Copy the admin UI source (minus node_modules/.next) so the -WebOnly task can run
# it from the install root. node_modules is excluded from the mirror, so an existing
# one is preserved; the first -WebOnly run installs it if absent.
$webSrc = Join-Path $repoDir "web"
$webDst = Join-Path $InstallRoot "web"
if (Test-Path $webSrc) {
    Write-Host "Copying admin UI source -> $webDst ..."
    & robocopy $webSrc $webDst /MIR /XD node_modules .next /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "robocopy of web/ failed (code $LASTEXITCODE)" }
    $global:LASTEXITCODE = 0
}

# Restrict the CA dir: disable inheritance, grant only SYSTEM + Administrators.
& icacls $caDir /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "Administrators:(OI)(CI)F" | Out-Null
Write-Host "CA directory locked to SYSTEM + Administrators."

# Fix any migrated config paths (LocalLfmDlpProxy -> PromptGate); no-op on a fresh
# config (already PromptGate). Preserves all other user settings.
if (Test-Path $cfgDst) {
    $c = Get-Content -Raw $cfgDst
    $c2 = $c -replace 'LocalLfmDlpProxy', 'PromptGate'
    if ($c2 -ne $c) {
        Set-Content -Path $cfgDst -Value $c2 -NoNewline
        Write-Host "Migrated config paths LocalLfmDlpProxy -> PromptGate."
    }
}

# Sync the deployed config's inference model label + I/O contract to what this
# install delivers, so re-running install.ps1 brings an existing config up to date
# (e.g. after the default model changes). Only these two single-line keys are
# rewritten; every other setting is preserved. inference.model / inference.profile
# are the only top-level-quoted `model:` / `profile:` keys in the config, and
# commented lines (leading '#') never match. (Neither label nor profile contains a
# '$', so the .NET replacement strings are literal.)
$cfgNow = Get-Content -Raw $cfgDst
$cfgSync = [regex]::Replace($cfgNow, '(?m)^(?<i>[ \t]*)model:[ \t]*".*"[ \t]*$', ('${i}model: "' + $ModelLabel + '"'))
$cfgSync = [regex]::Replace($cfgSync, '(?m)^(?<i>[ \t]*)profile:[ \t]*".*"[ \t]*$', ('${i}profile: "' + $Profile + '"'))
if ($cfgSync -ne $cfgNow) {
    Set-Content -Path $cfgDst -Value $cfgSync -NoNewline
    Write-Host "Synced config: inference.model='$ModelLabel', inference.profile='$Profile'."
}
else {
    Write-Host "Config inference.model/profile already current ($ModelLabel / $Profile)."
}

# Enable supervise (service owns the user-session sidecar + admin UI). Append an
# active block only if the config has no active 'supervise:' key yet.
$cfgRaw = Get-Content -Raw $cfgDst
if ($cfgRaw -notmatch '(?m)^\s*supervise:') {
    $superviseBlock = @"

# Added by install.ps1: the service starts/stops the user-session sidecar + admin UI.
supervise:
  enabled: true
  sidecar_task: "$sidecarTask"
  web_task: "$webTask"
  web_port: $webPort
  stop_timeout_ms: 8000
"@
    Add-Content -Path $cfgDst -Value $superviseBlock
    Write-Host "Enabled supervise in $cfgDst (service owns sidecar + admin UI)."
} else {
    Write-Host "config already has a supervise block; leaving it as-is."
}

# 3. Generate (or reuse) the Name-Constrained CA, then trust it machine-wide. A
#    migrated CA is reused as-is (trust is by key, not name), so no re-trust needed.
Write-Host "Ensuring interception CA..."
& $exeDst -config $cfgDst -init-ca | Out-Null
if (-not (Test-Path $caCrt)) { throw "CA generation did not produce $caCrt" }
Write-Host "Installing CA into Cert:\LocalMachine\Root ..."
Import-Certificate -FilePath $caCrt -CertStoreLocation Cert:\LocalMachine\Root | Out-Null

# 4. Register the Windows service (LocalSystem, MANUAL start) with restart recovery.
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    Write-Host "Service $ServiceName already exists; reconfiguring."
    & sc.exe stop $ServiceName | Out-Null
    & sc.exe delete $ServiceName | Out-Null
    Start-Sleep -Seconds 1
}
$binPath = '"{0}" -config "{1}"' -f $exeDst, $cfgDst
New-Service -Name $ServiceName -BinaryPathName $binPath -DisplayName "PromptGate" `
    -Description "PromptGate: inspects outbound Claude/Anthropic API traffic for sensitive-data egress (DLP)." `
    -StartupType Manual | Out-Null
# Restart on crash so the hosts redirect is reconciled rather than left dangling.
& sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null
Write-Host "Service '$ServiceName' registered (MANUAL start, restart-on-failure)."

# 5. Register the user-session RunOnDemand tasks (NO trigger — the service's
#    supervisor runs them on start and ends them on stop). Interactive principal so
#    the sidecar reaches the iGPU and the UI uses the per-user node install.
$user = "$env:USERDOMAIN\$env:USERNAME"
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew

$sidecarArgs = '-NoProfile -ExecutionPolicy Bypass -File "{0}" -SidecarOnly -Backend {1} -LlamaPort {2} -Model "{3}"' -f `
    $startDst, $Backend, $LlamaPort, $Model
$sidecarAction = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $sidecarArgs -WorkingDirectory $InstallRoot
Register-ScheduledTask -TaskName $sidecarTask -Action $sidecarAction -Principal $principal -Settings $settings -Force | Out-Null
Write-Host "Task '$sidecarTask' registered (RunOnDemand; Vulkan sidecar in your session)."

$webArgs = '-NoProfile -ExecutionPolicy Bypass -File "{0}" -WebOnly -Config "{1}"' -f $startDst, $cfgDst
$webAction = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $webArgs -WorkingDirectory $InstallRoot
Register-ScheduledTask -TaskName $webTask -Action $webAction -Principal $principal -Settings $settings -Force | Out-Null
Write-Host "Task '$webTask' registered (RunOnDemand; admin UI in your session)."

Write-Host ""
Write-Host "Install complete." -ForegroundColor Green
Write-Host "The hosts redirect + HTTPS interception activate when the service runs."
if ($StartNow) {
    Write-Host "Starting the service now (-StartNow): proxy + sidecar + admin UI..." -ForegroundColor Yellow
    Start-Service -Name $ServiceName
    Write-Host "Started. api.anthropic.com is now routed through PromptGate."
} else {
    Write-Host "NOT started now (current Claude sessions stay untouched)." -ForegroundColor Yellow
    Write-Host "Start it deliberately while logged in:"
    Write-Host "    Start-Service PromptGate     # proxy + sidecar + admin UI together"
    Write-Host "    Stop-Service  PromptGate     # stops all three"
}
