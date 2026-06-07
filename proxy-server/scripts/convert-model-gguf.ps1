<#
.SYNOPSIS
    Convert an akiFQC LFM2 *-Conf-Extract Japanese checkpoint (safetensors) into a
    quantized GGUF that the llama.cpp sidecar (start.ps1) can load.

.DESCRIPTION
    The Conf-Extract family is published as safetensors only — no GGUF — but the
    DLP proxy runs llama.cpp, which needs GGUF. This script does the one-time
    conversion locally:

        HuggingFace snapshot  ->  convert_hf_to_gguf.py (f16 GGUF)  ->  llama-quantize

    The whole family shares one I/O contract (config profile
    `jp_confidential_extraction`), so swapping model size is just converting a
    different -Repo and pointing `start.ps1 -Model` at the resulting .gguf — no
    code or profile change. Once a prebuilt *-GGUF repo exists upstream you can skip
    this entirely and pass `-Model <repo>-GGUF:<quant>` (an -hf ref) to start.ps1.

.PARAMETER Repo
    HuggingFace repo id to convert. Default is the 1.2B checkpoint. For the 350M:
        -Repo akiFQC/LFM2-350M-Conf-Extract-Japanese

.PARAMETER Quant
    llama-quantize type (e.g. Q4_K_M, Q8_0, Q5_K_M). Default Q4_K_M (matches the
    iGPU profile in use). Use Q8_0 for higher fidelity on these small models.
    Pass F16 to keep the unquantized f16 GGUF as the final output.

.PARAMETER OutDir
    Where the final .gguf is written. Default .\models (matches start.ps1 -Model).

.PARAMETER CacheDir
    Working area for the HF snapshot and a llama.cpp checkout. Default .\.cache.

.PARAMETER LlamaCppDir
    Path to a llama.cpp source checkout providing convert_hf_to_gguf.py. Default
    <CacheDir>\llama.cpp; shallow-cloned on first run if absent.

.PARAMETER HfToken
    Optional HuggingFace token (for gated/rate-limited downloads). Falls back to a
    prior `huggingface-cli login` / $env:HF_TOKEN.

.PARAMETER Python
    Python executable. Default "python".

.PARAMETER SkipPipInstall
    Skip installing convert_hf_to_gguf.py's pip requirements (use if already set up).

.PARAMETER Force
    Re-run conversion even if the target .gguf already exists.

.EXAMPLE
    .\scripts\convert-model-gguf.ps1
    # -> .\models\LFM2.5-1.2B-JP-202606-Conf-Extract-Q4_K_M.gguf  (start.ps1 default)

.EXAMPLE
    .\scripts\convert-model-gguf.ps1 -Repo akiFQC/LFM2-350M-Conf-Extract-Japanese -Quant Q8_0

.NOTES
    Prereqs (Windows): Python 3, git, and llama-quantize on PATH (ships with
    `winget install ggml.llamacpp`). The pip requirements pulled for the convert
    step include torch and are large. LFM2 (Lfm2ForCausalLM) needs a recent
    llama.cpp; the shallow clone tracks master so it is current.
    Run from the proxy-server directory (paths are relative to the current dir).
#>
[CmdletBinding()]
param(
    [string]$Repo = "akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract",
    [string]$Quant = "Q4_K_M",
    [string]$OutDir = ".\models",
    [string]$CacheDir = ".\.cache",
    [string]$LlamaCppDir = "",
    [string]$HfToken = "",
    [string]$Python = "python",
    [switch]$SkipPipInstall,
    [switch]$Force
)

$ErrorActionPreference = "Stop"

function Require-Cmd {
    param([string]$Name, [string]$Hint)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command not found on PATH: '$Name'. $Hint"
    }
}

function Exec {
    # NOTE: do not name this param $Args — it collides with PowerShell's automatic
    # $Args variable and the splat silently becomes empty.
    param([string]$File, [string[]]$CmdArgs)
    Write-Host ">> $File $($CmdArgs -join ' ')" -ForegroundColor DarkGray
    & $File @CmdArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed (exit $LASTEXITCODE): $File $($CmdArgs -join ' ')"
    }
}

# --- preconditions -----------------------------------------------------------
Require-Cmd $Python "Install Python 3 and ensure it is on PATH (or pass -Python)."
Require-Cmd "git" "Install Git (needed to fetch convert_hf_to_gguf.py)."
Require-Cmd "llama-quantize" "Install a llama.cpp build (winget install ggml.llamacpp) so llama-quantize is on PATH."

# `hf` is the HuggingFace downloader (the old `huggingface-cli` is deprecated and
# now refuses to run). Install huggingface_hub[cli] into the chosen Python if absent.
if (-not (Get-Command "hf" -ErrorAction SilentlyContinue)) {
    Write-Host "hf CLI not found; installing huggingface_hub[cli] into $Python ..."
    Exec $Python @("-m", "pip", "install", "-U", "huggingface_hub[cli]")
}

# --- resolve paths -----------------------------------------------------------
$leaf = ($Repo -split "/")[-1]
if ([string]::IsNullOrWhiteSpace($LlamaCppDir)) { $LlamaCppDir = Join-Path $CacheDir "llama.cpp" }
$snapshotDir = Join-Path (Join-Path $CacheDir "hf") $leaf
$f16Path     = Join-Path $OutDir "$leaf-f16.gguf"
$isF16Final  = $Quant -match '^(f16|bf16)$'
$finalPath   = if ($isF16Final) { $f16Path } else { Join-Path $OutDir "$leaf-$Quant.gguf" }

New-Item -ItemType Directory -Force -Path $OutDir   | Out-Null
New-Item -ItemType Directory -Force -Path $CacheDir | Out-Null

if ((Test-Path $finalPath) -and -not $Force) {
    Write-Host "Already converted: $finalPath" -ForegroundColor Green
    Write-Host "Use it with:  .\start.ps1 -Model `"$finalPath`"   (or pass -Force to rebuild)"
    return
}

# --- 1. download the safetensors snapshot ------------------------------------
Write-Host "`n[1/3] Downloading $Repo -> $snapshotDir" -ForegroundColor Cyan
$dlArgs = @("download", $Repo, "--local-dir", $snapshotDir, "--exclude", "checkpoint-*/*")
if ($HfToken -ne "") { $dlArgs += @("--token", $HfToken) }
Exec "hf" $dlArgs

# --- 2. ensure llama.cpp convert script + deps, then convert to f16 GGUF ------
Write-Host "`n[2/3] Converting to f16 GGUF" -ForegroundColor Cyan
$convertPy = Join-Path $LlamaCppDir "convert_hf_to_gguf.py"
if (-not (Test-Path $convertPy)) {
    Write-Host "Fetching llama.cpp (shallow) -> $LlamaCppDir"
    Exec "git" @("clone", "--depth", "1", "https://github.com/ggml-org/llama.cpp", $LlamaCppDir)
}
if (-not $SkipPipInstall) {
    # Install convert_hf_to_gguf.py's deps as WHEELS ONLY. We deliberately do NOT
    # use llama.cpp's requirements file: it pins numpy~=1.26.4, which has no wheel
    # for newer Python (3.13+) and falls back to a source build needing an MSVC
    # toolchain (fails on a compiler-less box). An unpinned numpy (2.x) works with
    # the converter and the in-repo gguf-py. --only-binary guarantees pip never
    # invokes a compiler (a missing wheel errors clearly instead of building).
    Write-Host "Installing convert dependencies (wheels only; includes torch CPU) ..."
    $pipPkgs = @("numpy", "torch", "transformers", "sentencepiece", "protobuf", "pyyaml", "tqdm", "safetensors")
    Exec $Python (@("-m", "pip", "install", "-U", "--only-binary=:all:",
            "--extra-index-url", "https://download.pytorch.org/whl/cpu") + $pipPkgs)
}
Exec $Python @($convertPy, $snapshotDir, "--outfile", $f16Path, "--outtype", "f16")

# --- 3. quantize -------------------------------------------------------------
if ($isF16Final) {
    Write-Host "`n[3/3] Quant=$Quant -> keeping f16 GGUF as final output." -ForegroundColor Cyan
} else {
    Write-Host "`n[3/3] Quantizing $f16Path -> $finalPath ($Quant)" -ForegroundColor Cyan
    Exec "llama-quantize" @($f16Path, $finalPath, $Quant)
}

Write-Host "`nDone. GGUF ready:" -ForegroundColor Green
Write-Host "  $finalPath"
Write-Host "`nLaunch the proxy with this model:"
Write-Host "  .\start.ps1 -Model `"$finalPath`""
if (-not $isF16Final) {
    Write-Host "`n(The intermediate $f16Path can be deleted, or kept to re-quantize without re-downloading.)" -ForegroundColor DarkGray
}
