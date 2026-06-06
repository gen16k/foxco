# Local LFM DLP Proxy — PoC one-command launcher.
#
# Starts the local LFM sidecar (llama.cpp `llama-server`) AND the proxy in one
# command. Target hardware is the AMD Ryzen 5 350 APU; no NVIDIA/CUDA required.
#
#   .\start.ps1                       # iGPU (Vulkan) sidecar + proxy   (default)
#   .\start.ps1 -Backend cpu          # CPU sidecar + proxy             (fallback)
#   .\start.ps1 -Classifier keyword   # no model: deterministic keyword fallback
#   .\start.ps1 -NoSidecar            # proxy only (sidecar already running)
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
    [switch]$NoSidecar
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

$proxyArgs = @("-config", $Config)
if ($Classifier -ne "") { $proxyArgs += @("-classifier", $Classifier) }

Write-Host "Starting Local LFM DLP Proxy on 127.0.0.1:8787 ..."
try {
    & .\proxy.exe @proxyArgs
}
finally {
    # Only stop a sidecar this script started; leave a pre-existing one running.
    if ($sidecar -and -not $sidecar.HasExited) {
        Write-Host "Stopping LFM sidecar (pid $($sidecar.Id)) ..."
        Stop-Process -Id $sidecar.Id -Force -ErrorAction SilentlyContinue
    }
}
