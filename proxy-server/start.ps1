# PromptGate — sidecar + proxy + admin UI launcher.
#
# Starts the local LFM sidecar (llama.cpp `llama-server`), the proxy, AND the admin
# web UI (Next.js, localhost only). Target hardware is the AMD Ryzen 5 350 APU; no
# NVIDIA/CUDA required.
#
#   .\start.ps1                       # iGPU (Vulkan) sidecar + proxy + admin UI
#   .\start.ps1 -Backend cpu          # CPU sidecar + proxy + admin UI  (fallback)
#   .\start.ps1 -Classifier keyword   # no model: deterministic keyword fallback
#   .\start.ps1 -NoSidecar            # proxy + admin UI only (sidecar elsewhere)
#   .\start.ps1 -NoWeb                # proxy (and sidecar) only, no admin UI
#   .\start.ps1 -SidecarOnly          # sidecar only, FOREGROUND (service RunOnDemand task)
#   .\start.ps1 -WebOnly              # admin UI only, FOREGROUND (service RunOnDemand task)
#   .\start.ps1 -Offline on           # force cache-only sidecar (fails if not cached)
#
# Model caching: by default (-Offline auto) the sidecar starts with llama.cpp's
# --offline flag whenever the requested GGUF is already in the local cache, so it
# loads straight from disk with no download and no per-start HF etag/manifest check.
# A machine that has not cached the model yet downloads it once via -hf (then it is
# cached for next time); pass -Offline off to always re-check the network.
#
# The admin UI starts on http://127.0.0.1:3939 (loopback only). It reads the proxy's
# admin API; this script passes the API address + admin.auth_token from the chosen
# config to the UI automatically so they match. Ctrl+C stops everything.
#
# SERVICE MODEL: in transparent mode the proxy runs as a Windows service (see
# install.ps1). The GPU-bound sidecar and the per-user node admin UI must run in the
# *user session* (a Session-0 service cannot reach the iGPU). The install-registered
# RunOnDemand tasks run this script with -SidecarOnly / -WebOnly; the service's
# supervisor triggers them on start and terminates them on stop. Both run in the
# FOREGROUND so the task's process tree owns the child — ending the task (or the
# supervisor's port-scoped taskkill /T) leaves nothing orphaned. Use the plain form
# (no -SidecarOnly/-WebOnly) for console/dev runs (sidecar + proxy + UI together).
#
# GPU acceleration uses the **Vulkan** build of llama.cpp on the integrated
# Radeon (RDNA 3.5). ROCm does not support AMD iGPUs on Windows, so Vulkan is the
# path; CPU (-Backend cpu) is the always-works fallback. Verify the iGPU is
# visible to llama.cpp with:  llama-server --list-devices
# If more than one Vulkan device shows up, pin it:  $env:GGML_VK_VISIBLE_DEVICES=0
#
# Then point Claude Code at the proxy and run it:
#       $env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"   # note: no /v1 suffix
#       claude
# Persist:  setx ANTHROPIC_BASE_URL "http://127.0.0.1:8787"
# Undo:     reg delete HKCU\Environment /F /V ANTHROPIC_BASE_URL

param(
    [string]$Config = ".\config\config.example.yaml",
    [string]$Classifier = "",                  # "" (config default / llama) or "keyword"
    [ValidateSet("vulkan", "cpu")]
    [string]$Backend = "vulkan",               # iGPU (Vulkan) by default; "cpu" to fall back
    [string]$Model = "akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M",  # -hf ref (auto-download) or local .gguf path
    [string]$LlamaServer = "llama-server",     # path to llama-server(.exe); default: on PATH
    [string]$LlamaHost = "127.0.0.1",
    [int]$LlamaPort = 8791,                     # must match inference.endpoint in config
    [int]$HealthTimeoutSec = 600,              # first run downloads the GGUF + compiles Vulkan shaders
    [ValidateSet("auto", "on", "off")]
    [string]$Offline = "auto",                 # "auto": use the cache offline if the model is already cached, else download once; "on": force --offline; "off": always allow network
    [switch]$NoSidecar,
    [switch]$SidecarOnly,                      # FOREGROUND sidecar only; no proxy/UI (service RunOnDemand task)
    [switch]$WebOnly,                          # FOREGROUND admin UI only; no proxy/sidecar (service RunOnDemand task)
    [switch]$NoWeb                             # skip launching the admin web UI
)

$ErrorActionPreference = "Stop"

if ($SidecarOnly -and $NoSidecar) { throw "-SidecarOnly and -NoSidecar are mutually exclusive." }
if ($SidecarOnly -and $WebOnly)   { throw "-SidecarOnly and -WebOnly are mutually exclusive." }

$healthUrl = "http://{0}:{1}/health" -f $LlamaHost, $LlamaPort

function Test-LlamaHealth {
    try {
        $r = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2
        return $r.StatusCode -eq 200
    } catch {
        return $false
    }
}

# True if the given -hf model ref is already in llama.cpp's local cache. We ask
# llama-server itself (`--cache-list`) so the answer matches whatever cache dir it
# actually uses (HF hub layout under HF_HOME / LLAMA_CACHE / %LOCALAPPDATA%). Used
# to decide whether we can start with --offline (no network) on this machine.
function Test-LlamaModelCached([string]$ModelRef) {
    try {
        $list = & $LlamaServer --cache-list 2>$null
        foreach ($line in $list) {
            if ($line -match [regex]::Escape($ModelRef)) { return $true }
        }
    } catch { }
    return $false
}

# Parse the proxy admin API address + token from the chosen config and export them
# so the Next.js admin UI talks to this proxy without extra setup. Shared by the
# console web start and the -WebOnly task branch.
function Set-AdminEnvFromConfig([string]$cfgPath) {
    $cfgText = if (Test-Path $cfgPath) { Get-Content -Raw $cfgPath } else { "" }
    $listen = if ($cfgText -match '(?m)^\s*listen_addr:\s*"?([^"\r\n#]+?)"?\s*(#.*)?$') { $Matches[1].Trim() } else { "127.0.0.1:8787" }
    $adminToken = if ($cfgText -match '(?m)^\s*auth_token:\s*"?([^"\r\n#]*?)"?\s*(#.*)?$') { $Matches[1].Trim() } else { "" }
    $env:PROXY_ADMIN_BASE_URL = "http://$listen"
    $env:PROXY_ADMIN_TOKEN = $adminToken
}

# Build the llama-server argument list (vulkan -> offload all layers to the iGPU;
# cpu -> keep everything on CPU). Accepts a HuggingFace ref (-hf, auto-download) or
# a local GGUF path (-m).
function Get-LlamaArgs {
    $ngl = if ($Backend -eq "vulkan") { 99 } else { 0 }
    $isHfRef = -not (Test-Path $Model)
    $modelArg = if ($isHfRef) { @("-hf", $Model) } else { @("-m", $Model) }
    $extra = @()
    # Model caching: when the GGUF is already cached, start with --offline so
    # llama-server reads it straight from cache and never hits the network — no
    # per-start re-download or HF etag/manifest check. On a machine that has not
    # cached it yet (e.g. a fresh box), we must NOT pass --offline or the first run
    # would fail with no way to download; -hf fetches it once, then it is cached for
    # next time. Local -m paths are already on disk. Override with -Offline on/off.
    $useOffline = switch ($Offline) {
        "on"  { $true }
        "off" { $false }
        default { $isHfRef -and (Test-LlamaModelCached $Model) }
    }
    if ($useOffline) {
        $extra += "--offline"
        Write-Host "Model '$Model' is cached -> starting offline (no download, no network check)."
    } elseif ($isHfRef) {
        Write-Host "Model '$Model' not in cache -> will download via -hf (cached for next time)."
    }
    return $modelArg + @("--host", $LlamaHost, "--port", "$LlamaPort", "--jinja", "-ngl", "$ngl") + $extra
}

# ---- Service RunOnDemand task: FOREGROUND sidecar only -----------------------
# Exec llama-server in the foreground so this powershell host is its parent. The
# service supervisor stops it by ending the task / port-scoped taskkill /T, which
# kills this whole tree — nothing is orphaned. No health gate needed: the proxy
# does its own health check and fail-closes until the sidecar is up.
if ($SidecarOnly) {
    if (Test-LlamaHealth) {
        Write-Host "LFM sidecar already healthy at $healthUrl — holding the task while it lives."
        while (Test-LlamaHealth) { Start-Sleep -Seconds 5 }
        return
    }
    $llamaArgs = Get-LlamaArgs
    Write-Host "Starting LFM sidecar ($Backend) on ${LlamaHost}:${LlamaPort} (foreground; task-managed) ..."
    Write-Host "  $LlamaServer $($llamaArgs -join ' ')"
    & $LlamaServer @llamaArgs
    return
}

# ---- Service RunOnDemand task: FOREGROUND admin UI only ----------------------
# Exec the Next.js server in the foreground (same orphan-free rationale as above).
if ($WebOnly) {
    $webDir = Join-Path $PSScriptRoot "web"
    if (-not (Test-Path (Join-Path $webDir "node_modules"))) {
        Write-Host "Installing admin UI dependencies (first run; this can take a minute)..."
        Push-Location $webDir
        try { & npm install } finally { Pop-Location }
    }
    Set-AdminEnvFromConfig $Config
    Write-Host "Starting admin UI (foreground; task-managed) -> http://127.0.0.1:3939 ..."
    Push-Location $webDir
    try { & npm.cmd run dev } finally { Pop-Location }
    return
}

# ---- Console / dev run: sidecar + proxy + admin UI together ------------------

# Build the proxy if the binary is missing.
if (-not (Test-Path ".\proxy.exe")) {
    Write-Host "Building proxy.exe..."
    go build -o proxy.exe .\cmd\proxy
}

# The keyword classifier needs no model, so never start a sidecar for it.
$useSidecar = (-not $NoSidecar) -and ($Classifier -ne "keyword")

$sidecar = $null
if ($useSidecar) {
    if (Test-LlamaHealth) {
        Write-Host "LFM sidecar already healthy at $healthUrl — reusing it."
    } else {
        $llamaArgs = Get-LlamaArgs
        Write-Host "Starting LFM sidecar ($Backend) on ${LlamaHost}:${LlamaPort} ..."
        Write-Host "  $LlamaServer $($llamaArgs -join ' ')"
        try {
            $sidecar = Start-Process -FilePath $LlamaServer -ArgumentList $llamaArgs -PassThru
        } catch {
            Write-Error ("Could not start '$LlamaServer'. Install a Vulkan build of llama.cpp and " +
                "put llama-server on PATH, or pass -LlamaServer <path>. " +
                "To run without a model: .\start.ps1 -Classifier keyword")
            throw
        }

        Write-Host "Waiting for the sidecar to become healthy (up to ${HealthTimeoutSec}s; first run downloads the model)..."
        $deadline = (Get-Date).AddSeconds($HealthTimeoutSec)
        while (-not (Test-LlamaHealth)) {
            if ($sidecar.HasExited) {
                throw "llama-server exited (code $($sidecar.ExitCode)) before becoming healthy. Check the model ref and that this is a Vulkan-capable build."
            }
            if ((Get-Date) -gt $deadline) {
                Stop-Process -Id $sidecar.Id -Force -ErrorAction SilentlyContinue
                throw "Timed out waiting for $healthUrl. The model may still be downloading; retry, or pre-download and pass -Model <path-to.gguf>."
            }
            Start-Sleep -Seconds 2
        }
        Write-Host "LFM sidecar is healthy."
    }
}

# Start the admin web UI (Next.js) on localhost, unless suppressed. It is bound to
# 127.0.0.1 (see web/package.json) so it is reachable only from this machine.
$web = $null
if (-not $NoWeb) {
    $webDir = Join-Path $PSScriptRoot "web"
    try {
        if (-not (Test-Path (Join-Path $webDir "node_modules"))) {
            Write-Host "Installing admin UI dependencies (first run; this can take a minute)..."
            Push-Location $webDir
            try { & npm install } finally { Pop-Location }
        }
        Set-AdminEnvFromConfig $Config
        Write-Host "Starting admin UI (localhost only) -> http://127.0.0.1:3939 ..."
        $web = Start-Process -FilePath "npm.cmd" -ArgumentList @("run", "dev") -WorkingDirectory $webDir -PassThru
    } catch {
        Write-Warning "Could not start the admin UI ($($_.Exception.Message)). Continuing with the proxy only; run it manually with:  cd web; npm install; npm run dev"
        $web = $null
    }
}

$proxyArgs = @("-config", $Config)
if ($Classifier -ne "") { $proxyArgs += @("-classifier", $Classifier) }

Write-Host "Starting PromptGate proxy on 127.0.0.1:8787 ..."
if ($web) { Write-Host "Admin UI: http://127.0.0.1:3939  (Ctrl+C here stops the UI and proxy)" }
try {
    & .\proxy.exe @proxyArgs
}
finally {
    # Stop the admin UI we started (kill the npm -> node process tree).
    if ($web -and -not $web.HasExited) {
        Write-Host "Stopping admin UI (pid $($web.Id)) ..."
        taskkill /PID $web.Id /T /F 2>$null | Out-Null
    }
    # Only stop a sidecar this script started; leave a pre-existing one running.
    if ($sidecar -and -not $sidecar.HasExited) {
        Write-Host "Stopping LFM sidecar (pid $($sidecar.Id)) ..."
        Stop-Process -Id $sidecar.Id -Force -ErrorAction SilentlyContinue
    }
}
