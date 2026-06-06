# Local LFM DLP Proxy — PoC one-command launcher.
#
# Starts the local LFM sidecar (llama.cpp `llama-server`), the proxy, AND the
# admin web UI (Next.js, localhost only) in one command. Target hardware is the
# AMD Ryzen 5 350 APU; no NVIDIA/CUDA required.
#
#   .\start.ps1                       # iGPU (Vulkan) sidecar + proxy + admin UI
#   .\start.ps1 -Backend cpu          # CPU sidecar + proxy + admin UI  (fallback)
#   .\start.ps1 -Classifier keyword   # no model: deterministic keyword fallback
#   .\start.ps1 -NoSidecar            # proxy + admin UI only (sidecar elsewhere)
#   .\start.ps1 -NoWeb                # proxy (and sidecar) only, no admin UI
#
# The admin UI starts on http://127.0.0.1:3939 (loopback only). It reads the
# proxy's admin API; this script passes the API address + admin.auth_token from
# the chosen config to the UI automatically so they match. Ctrl+C stops everything.
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
    [string]$Model = "LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M",  # -hf ref or local .gguf path
    [string]$LlamaServer = "llama-server",     # path to llama-server(.exe); default: on PATH
    [string]$LlamaHost = "127.0.0.1",
    [int]$LlamaPort = 8791,                     # must match inference.endpoint in config
    [int]$HealthTimeoutSec = 600,              # first run downloads the GGUF + compiles Vulkan shaders
    [switch]$NoSidecar,
    [switch]$NoWeb                              # skip launching the admin web UI
)

$ErrorActionPreference = "Stop"

# Build the proxy if the binary is missing.
if (-not (Test-Path ".\proxy.exe")) {
    Write-Host "Building proxy.exe..."
    go build -o proxy.exe .\cmd\proxy
}

# The keyword classifier needs no model, so never start a sidecar for it.
$useSidecar = (-not $NoSidecar) -and ($Classifier -ne "keyword")

$healthUrl = "http://{0}:{1}/health" -f $LlamaHost, $LlamaPort

function Test-LlamaHealth {
    try {
        $r = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2
        return $r.StatusCode -eq 200
    } catch {
        return $false
    }
}

$sidecar = $null
if ($useSidecar) {
    if (Test-LlamaHealth) {
        Write-Host "LFM sidecar already healthy at $healthUrl — reusing it."
    } else {
        # vulkan -> offload all layers to the iGPU; cpu -> keep everything on CPU.
        $ngl = if ($Backend -eq "vulkan") { 99 } else { 0 }
        # Accept either a HuggingFace ref (auto-download via -hf) or a local GGUF path.
        $modelArg = if (Test-Path $Model) { @("-m", $Model) } else { @("-hf", $Model) }
        $llamaArgs = $modelArg + @(
            "--host", $LlamaHost, "--port", "$LlamaPort", "--jinja", "-ngl", "$ngl"
        )
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
# 127.0.0.1 (see web/package.json) so it is reachable only from this machine. We
# pass the admin API address + token parsed from the chosen config so the UI talks
# to this proxy without extra setup.
$web = $null
if (-not $NoWeb) {
    $webDir = Join-Path $PSScriptRoot "web"
    try {
        if (-not (Test-Path (Join-Path $webDir "node_modules"))) {
            Write-Host "Installing admin UI dependencies (first run; this can take a minute)..."
            Push-Location $webDir
            try { & npm install } finally { Pop-Location }
        }
        $cfgText = if (Test-Path $Config) { Get-Content -Raw $Config } else { "" }
        $listen = if ($cfgText -match '(?m)^\s*listen_addr:\s*"?([^"\r\n#]+?)"?\s*(#.*)?$') { $Matches[1].Trim() } else { "127.0.0.1:8787" }
        $adminToken = if ($cfgText -match '(?m)^\s*auth_token:\s*"?([^"\r\n#]*?)"?\s*(#.*)?$') { $Matches[1].Trim() } else { "" }
        $env:PROXY_ADMIN_BASE_URL = "http://$listen"
        $env:PROXY_ADMIN_TOKEN = $adminToken
        Write-Host "Starting admin UI (localhost only) -> http://127.0.0.1:3939 ..."
        $web = Start-Process -FilePath "npm.cmd" -ArgumentList @("run", "dev") -WorkingDirectory $webDir -PassThru
    } catch {
        Write-Warning "Could not start the admin UI ($($_.Exception.Message)). Continuing with the proxy only; run it manually with:  cd web; npm install; npm run dev"
        $web = $null
    }
}

$proxyArgs = @("-config", $Config)
if ($Classifier -ne "") { $proxyArgs += @("-classifier", $Classifier) }

Write-Host "Starting Local LFM DLP Proxy on 127.0.0.1:8787 ..."
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
