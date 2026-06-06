# Local LFM DLP Proxy — sidecar + proxy launcher.
#
# Starts the local LFM sidecar, the proxy, AND the admin web UI (Next.js, localhost
# only) in one command. Target hardware is an AMD Ryzen AI APU (XDNA2 NPU + RDNA 3.5
# iGPU); no NVIDIA/CUDA required.
#
#   .\start.ps1                       # auto: NPU -> Vulkan(iGPU) -> CPU + proxy + admin UI
#   .\start.ps1 -Backend npu          # AMD NPU only (Ryzen AI ONNX shim)
#   .\start.ps1 -Backend vulkan       # iGPU (Vulkan) only  (disables NPU)
#   .\start.ps1 -Backend cpu          # CPU only            (disables NPU)
#   .\start.ps1 -Classifier keyword   # no model: deterministic keyword fallback
#   .\start.ps1 -NoSidecar            # proxy + admin UI only (sidecar elsewhere)
#   .\start.ps1 -NoWeb                # proxy (and sidecar) only, no admin UI
#   .\start.ps1 -SidecarOnly          # sidecar only, stay running (logon task; no proxy/UI)
#
# The admin UI starts on http://127.0.0.1:3939 (loopback only). It reads the proxy's
# admin API; this script passes the API address + admin.auth_token from the chosen
# config to the UI automatically so they match. Ctrl+C stops everything.
#
# SERVICE MODEL: in transparent mode the proxy runs as a Windows service (see
# install.ps1), and the GPU-bound sidecar must run in the *user session* (a Session-0
# service cannot reach the iGPU). The install-registered logon task runs this script
# with -SidecarOnly so the sidecar keeps the iGPU and the service connects to it at
# 127.0.0.1:8791. Use the non-SidecarOnly form for console/dev runs (proxy + UI).
#
# Backends:
#   * NPU    — AMD Ryzen AI NPU (XDNA2) via the project's own OpenAI-compatible
#              shim (npu\npu_server.py) wrapping AMD's LFM2 token-fusion ONNX +
#              RyzenAILightExecutionProvider. llama.cpp/Ollama cannot drive the
#              NPU, and Lemonade(OGA) cannot run LFM2, so this shim is the path.
#              Requires: AMD NPU driver + Ryzen AI Software 1.7.1 (conda env
#              'ryzen-ai-1.7.1') + the local LFM2 ONNX model. See npu\README.md.
#   * Vulkan — llama.cpp Vulkan build offloading to the integrated Radeon (-ngl 99).
#              ROCm does not support AMD iGPUs on Windows, so Vulkan is the GPU path.
#   * CPU    — llama.cpp on CPU (-ngl 0); the always-works fallback.
# In `auto`, each backend is tried in order and accepted only if it becomes healthy
# (NPU also has to actually serve the model), so a missing/flaky NPU transparently
# falls back to Vulkan then CPU.
#
# Verify the iGPU is visible to llama.cpp with:  llama-server --list-devices
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
    [ValidateSet("auto", "npu", "vulkan", "cpu")]
    [string]$Backend = "auto",                 # auto = NPU -> Vulkan -> CPU; or force one

    # --- llama.cpp sidecar (vulkan / cpu backends) ---
    [string]$Model = "LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M",  # -hf ref or local .gguf path
    [string]$LlamaServer = "llama-server",     # path to llama-server(.exe); default: on PATH
    [string]$LlamaHost = "127.0.0.1",
    [int]$LlamaPort = 8791,                     # must match inference.endpoint in config

    # --- AMD NPU sidecar (custom Ryzen AI ONNX shim, npu backend) ---
    # The shim (npu\npu_server.py) runs inside AMD's Ryzen AI conda env and serves
    # the llama.cpp-style /v1/chat/completions + /health on its own port, so no
    # request-path override is needed (unlike Lemonade's /api/v1).
    [string]$Conda = "conda",                                   # Miniforge conda launcher; default: on PATH
    [string]$CondaEnv = "ryzen-ai-1.7.1",                       # AMD Ryzen AI env that has the NPU EP
    [string]$NpuServerScript = ".\npu\npu_server.py",           # the OpenAI-compatible shim
    [string]$NpuModel = "C:\Users\gen16k\ryzenai-lfm2\LFM2-1.2B-ONNX_rai_1.7.1",  # local LFM2 ONNX dir
    [string]$NpuHost = "127.0.0.1",
    [int]$NpuPort = 8792,                                       # shim port (8791 is llama.cpp)

    [int]$HealthTimeoutSec = 600,              # first run downloads the model + compiles (Vulkan shaders / NPU graph)
    [switch]$NoSidecar,
    [switch]$SidecarOnly,                      # start the sidecar and stay running; do not run the proxy/UI (logon task)
    [switch]$NoWeb                             # skip launching the admin web UI
)

$ErrorActionPreference = "Stop"

# Build the proxy if the binary is missing (not needed in sidecar-only mode — the
# service owns proxy.exe there).
if (-not $SidecarOnly -and -not (Test-Path ".\proxy.exe")) {
    Write-Host "Building proxy.exe..."
    go build -o proxy.exe .\cmd\proxy
}

$llamaBase = "http://{0}:{1}" -f $LlamaHost, $LlamaPort
$npuBase = "http://{0}:{1}" -f $NpuHost, $NpuPort

if ($SidecarOnly -and $NoSidecar) {
    throw "-SidecarOnly and -NoSidecar are mutually exclusive."
}

# The keyword classifier needs no model, so never start a sidecar for it.
$useSidecar = (-not $NoSidecar) -and ($Classifier -ne "keyword")

function Test-HttpOk($url) {
    try {
        return (Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200
    } catch {
        return $false
    }
}

# Confirm the shim actually serves the model with a 1-token completion (not just
# that the port is open). This makes `auto` fall back correctly if the model failed
# to load. The shim only opens its socket after the model is loaded, so in practice
# /health implies ready, but the probe is cheap insurance.
function Test-NpuReady {
    if (-not (Test-HttpOk ($npuBase + "/health"))) { return $false }
    $body = @{
        model       = $NpuModel
        messages    = @(@{ role = "user"; content = "ping" })
        max_tokens  = 1
        temperature = 0
    } | ConvertTo-Json -Depth 5
    try {
        $r = Invoke-WebRequest -Uri ($npuBase + "/v1/chat/completions") -Method Post -Body $body `
            -ContentType "application/json" -UseBasicParsing -TimeoutSec $HealthTimeoutSec
        return $r.StatusCode -eq 200
    } catch {
        return $false
    }
}

# Whether `auto` should ATTEMPT the NPU. Requires both the local LFM2 ONNX model
# and the Ryzen AI conda env (the shim launches via `conda run -n <env> python`);
# without either the shim cannot start, so skip straight to Vulkan. The real
# acceptance gate is still Test-NpuReady above. (-Backend npu forces an attempt and
# surfaces the Hint on failure, bypassing this pre-filter.)
function Test-NpuAvailable {
    if (-not (Test-Path $NpuModel)) {
        Write-Host "NPU model dir not found ($NpuModel); skipping NPU."
        return $false
    }
    try {
        $envs = & $Conda env list 2>$null
        if ($LASTEXITCODE -eq 0 -and (($envs -join "`n") -match [regex]::Escape($CondaEnv))) {
            return $true
        }
    } catch {}
    Write-Host "conda env '$CondaEnv' not found; skipping NPU (install AMD Ryzen AI Software 1.7.1)."
    return $false
}

# Returns a backend "spec": how to launch its sidecar and how to wire the proxy.
function Get-Spec($name) {
    switch ($name) {
        "npu" {
            return @{
                Name         = "npu"
                Exe          = $Conda
                Args         = @("run", "--no-capture-output", "-n", $CondaEnv, "python",
                                 $NpuServerScript, "--model", $NpuModel,
                                 "--host", $NpuHost, "--port", "$NpuPort")
                HealthTest   = { Test-HttpOk ($npuBase + "/health") }
                ReadyTest    = { Test-NpuReady }
                Endpoint     = $npuBase
                ChatPath     = ""    # shim serves llama.cpp-style /v1/chat/completions
                HealthPath   = ""    # and /health -> keep client defaults
                Profile      = "reason_decision_prompt"   # NPU cannot grammar-constrain output
                Model        = $NpuModel
                BackendLabel = "npu"
                Hint         = "Install AMD Ryzen AI Software 1.7.1 (conda env '$CondaEnv') + NPU driver, clone the LFM2 ONNX model to '$NpuModel', and put '$Conda' on PATH. See npu\README.md."
            }
        }
        { $_ -eq "vulkan" -or $_ -eq "cpu" } {
            $ngl = if ($name -eq "vulkan") { 99 } else { 0 }
            $modelArg = if (Test-Path $Model) { @("-m", $Model) } else { @("-hf", $Model) }
            return @{
                Name         = $name
                Exe          = $LlamaServer
                Args         = $modelArg + @("--host", $LlamaHost, "--port", "$LlamaPort", "--jinja", "-ngl", "$ngl")
                HealthTest   = { Test-HttpOk ($llamaBase + "/health") }
                ReadyTest    = $null
                Endpoint     = $llamaBase
                ChatPath     = ""    # llama.cpp defaults
                HealthPath   = ""
                Profile      = ""    # keep config default (reason_decision, schema-constrained)
                Model        = ""    # keep config model label
                BackendLabel = $name
                Hint         = "Install a Vulkan build of llama.cpp (winget install ggml.llamacpp) and put llama-server on PATH, or pass -LlamaServer <path>."
            }
        }
    }
}

# Starts a sidecar (or reuses an already-healthy one) and waits until it is healthy
# AND, if a ReadyTest is given, actually serving. Returns @{Proc;Reused} on success
# or $null on failure (so `auto` can try the next backend).
function Start-Sidecar($spec) {
    $label = $spec.Name
    if (& $spec.HealthTest) {
        if ($spec.ReadyTest -and -not (& $spec.ReadyTest)) {
            Write-Warning "$label server is up but not serving the model — not reusing."
            return $null
        }
        Write-Host "$label sidecar already healthy — reusing it."
        return @{ Proc = $null; Reused = $true }
    }

    Write-Host "Starting $label sidecar:"
    Write-Host "  $($spec.Exe) $($spec.Args -join ' ')"
    try {
        $proc = Start-Process -FilePath $spec.Exe -ArgumentList $spec.Args -PassThru
    } catch {
        Write-Warning "Could not start '$($spec.Exe)' for $label backend: $($_.Exception.Message)"
        Write-Warning $spec.Hint
        return $null
    }

    Write-Host "Waiting for $label to become healthy (up to ${HealthTimeoutSec}s; first run downloads/compiles)..."
    $deadline = (Get-Date).AddSeconds($HealthTimeoutSec)
    while (-not (& $spec.HealthTest)) {
        if ($proc.HasExited) {
            Write-Warning "$label sidecar exited (code $($proc.ExitCode)) before becoming healthy. $($spec.Hint)"
            return $null
        }
        if ((Get-Date) -gt $deadline) {
            Write-Warning "$label sidecar timed out before /health."
            Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
            return $null
        }
        Start-Sleep -Seconds 2
    }
    if ($spec.ReadyTest -and -not (& $spec.ReadyTest)) {
        Write-Warning "$label server healthy but model not ready (probe failed). $($spec.Hint)"
        Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        return $null
    }
    Write-Host "$label sidecar is healthy."
    return @{ Proc = $proc; Reused = $false }
}

# ---------------------------------------------------------------------------
# Select a backend and (unless -NoSidecar) launch its sidecar.
# ---------------------------------------------------------------------------
$sidecar = $null
$spec = $null

if ($Classifier -eq "keyword") {
    Write-Host "Classifier=keyword: running without an LFM sidecar."
}
elseif ($NoSidecar) {
    # Don't start anything; wire the proxy for the requested backend if explicit.
    if ($Backend -ne "auto") {
        $spec = Get-Spec $Backend
        Write-Host "-NoSidecar: assuming a '$Backend' sidecar is already running at $($spec.Endpoint)."
    } else {
        Write-Host "-NoSidecar with -Backend auto: using config defaults (assuming a sidecar is already running)."
    }
}
else {
    $candidates =
        if ($Backend -eq "auto") {
            $list = @()
            if (Test-NpuAvailable) {
                $list += "npu"
            } else {
                Write-Host "NPU not available; falling back to Vulkan/CPU (see npu\README.md to enable)."
            }
            $list + @("vulkan", "cpu")
        } else {
            @($Backend)
        }

    foreach ($name in $candidates) {
        $s = Get-Spec $name
        Write-Host "--- Trying '$name' backend ---"
        $res = Start-Sidecar $s
        if ($res) {
            $spec = $s
            if (-not $res.Reused) { $sidecar = $res.Proc }
            break
        }
        if ($Backend -ne "auto") {
            throw "Backend '$name' failed to start or become healthy. $($s.Hint)"
        }
    }
    if (-not $spec) {
        throw "No inference backend became healthy (tried: $($candidates -join ', '))."
    }
    Write-Host "Selected backend: $($spec.BackendLabel) at $($spec.Endpoint)"
}

# ---------------------------------------------------------------------------
# Sidecar-only mode / admin UI, then launch the proxy.
# ---------------------------------------------------------------------------
# Sidecar-only mode (logon task): keep this process alive for the sidecar's
# lifetime so the Scheduled Task represents the sidecar and can stop it. The proxy
# runs separately as a Windows service; no proxy or admin UI is started here.
if ($SidecarOnly) {
    if ($sidecar) {
        Write-Host "Sidecar running (pid $($sidecar.Id)); holding session. Ctrl-C or ending the task stops it."
        try {
            Wait-Process -Id $sidecar.Id
        }
        finally {
            if (-not $sidecar.HasExited) { Stop-Process -Id $sidecar.Id -Force -ErrorAction SilentlyContinue }
        }
    } else {
        Write-Host "Sidecar already running elsewhere; nothing to hold. Exiting."
    }
    return
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

# ---------------------------------------------------------------------------
# Launch the proxy, wiring the chosen backend via CLI overrides.
# ---------------------------------------------------------------------------
$proxyArgs = @("-config", $Config)
if ($Classifier -ne "") { $proxyArgs += @("-classifier", $Classifier) }
if ($spec) {
    $proxyArgs += @("-backend", $spec.BackendLabel, "-endpoint", $spec.Endpoint)
    if ($spec.ChatPath)   { $proxyArgs += @("-chat-path", $spec.ChatPath) }
    if ($spec.HealthPath) { $proxyArgs += @("-health-path", $spec.HealthPath) }
    if ($spec.Profile)    { $proxyArgs += @("-profile", $spec.Profile) }
    if ($spec.Model)      { $proxyArgs += @("-model", $spec.Model) }
}

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
