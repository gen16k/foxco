# Local LFM DLP Proxy — PoC launcher.
#
# 1) (optional) Start the LFM sidecar first, e.g. llama-server with an LFM2 GGUF:
#       llama-server -m .\models\LFM2-1.2B-Q4_K_M.gguf --port 8791 --host 127.0.0.1
#    If you don't have a model yet, run the proxy with -classifier keyword.
#
# 2) Start the proxy:
#       .\start.ps1
#
# 3) Point Claude Code at the proxy and run it:
#       $env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"   # note: no /v1 suffix
#       claude
#
# To make the setting persistent:  setx ANTHROPIC_BASE_URL "http://127.0.0.1:8787"
# To undo it:                      reg delete HKCU\Environment /F /V ANTHROPIC_BASE_URL

param(
    [string]$Config = ".\configs\config.example.yaml",
    [string]$Classifier = ""   # "llama" (default via config) or "keyword"
)

$ErrorActionPreference = "Stop"

# Build the proxy if the binary is missing.
if (-not (Test-Path ".\proxy.exe")) {
    Write-Host "Building proxy.exe..."
    go build -o proxy.exe .\cmd\proxy
}

$args = @("-config", $Config)
if ($Classifier -ne "") { $args += @("-classifier", $Classifier) }

Write-Host "Starting Local LFM DLP Proxy on 127.0.0.1:8787 ..."
& .\proxy.exe @args
