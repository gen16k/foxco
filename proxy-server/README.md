# Local LFM DLP Proxy

A local Data-Loss-Prevention proxy that sits between **Claude Code** and the
Anthropic API. It inspects every outbound request, asks a local **LFM (Liquid
Foundation Model)** whether the content is safe to send, **blocks** sensitive
egress (secrets, credentials, internal/PII/proprietary data), and sanitizes
re-sent history so a once-blocked secret never leaks on a later turn.

This is primarily a **showcase for on-device LFM classification**: the LFM is the
primary NG/OK classifier, with a small deterministic rule guardrail as insurance.

> **Threat model — read this first.** This is an *advisory* control for the
> honest-mistake case (a developer accidentally pasting a secret). It runs as a
> per-user localhost proxy selected via `ANTHROPIC_BASE_URL`, so it is **not** a
> tamper-proof enforcement boundary: a user who unsets the env var or stops the
> process bypasses it. Real enforcement requires a network egress chokepoint
> (forward proxy / firewall) the user cannot disable. See `docs/spec-proxy.md`.

## How it works

```
Claude Code --(ANTHROPIC_BASE_URL=http://127.0.0.1:8787)--> Proxy
  parse  -> normalize -> segment -> [cache] -> rule guardrail -> LFM (NG/OK)
         -> BLOCK (assistant message / SSE, no egress)          (live turn NG)
         -> sanitize history (remove sensitive tool/turn units) -> forward upstream
```

- **Model -> binary verdict.** The proxy reduces the LFM output to
  `BLOCK -> block` / `ALLOW -> forward`. The default model is the akiFQC
  **Conf-Extract** Japanese family (profile `jp_confidential_extraction`): an
  11-category confidential-entity extractor — any non-empty category => BLOCK,
  and only the category *names* (never the extracted values) reach the reason /
  audit log. The deterministic rule guardrail short-circuits to BLOCK on
  unambiguous secrets (AWS/Anthropic/Google keys, private-key blocks, ...) and
  backstops anything outside those 11 categories. The full prompt/schema/parse
  contract is a swappable `PromptProfile` (`internal/inference/profile.go`) —
  select via `inference.profile` (also built in: `reason_decision` / `ng_boolean`
  generic ALLOW/BLOCK classifiers), or pin the exact prompt with
  `inference.system_prompt_file`. Parsing is tolerant (llama.cpp does not always
  enforce the JSON schema). The classifier profiles wrap each segment in
  `<<<DATA>>>` delimiters so the small model treats it as inert data; the
  extraction profile sends raw text to match its training distribution — an
  extraction model has no verdict field for an injection to flip, so the worst
  case is a missed entity, which the rule guardrail + fail-closed still backstop.
- **Stateless + fingerprint cache.** Every request is fully re-evaluated; a
  content-addressed cache means each segment is classified once. No HMAC
  markers, block registry, or session-id reconstruction.
- **Structure-aware sanitize.** Sensitive *history* is removed as whole units —
  matched `tool_use`/`tool_result` pairs and `user`/block-notice turn pairs —
  then the message array is validated (role + tool-id pairing) and the request
  fails closed if a valid structure can't be produced.
- **Request-only inspection.** Model responses stream through untouched (no TTFT
  penalty). `/v1/messages/count_tokens` goes through the same DLP path because it
  is a second egress channel.

## Build & test

Run from this `proxy-server/` directory (the Go module root):

```powershell
go build -o proxy.exe ./cmd/proxy
go test ./...
```

## Run (PoC)

Target hardware is an **AMD Ryzen AI series APU** (RDNA 3.5 integrated Radeon iGPU
+ XDNA2 NPU; e.g. Ryzen AI MAX+ 395 / Ryzen 5 350); no NVIDIA/CUDA is required.

### Setup (one-time)

The LFM runs in a llama.cpp `llama-server` sidecar. Install a **Vulkan**-enabled
build — the official winget package ships the Windows Vulkan binary, which offloads
onto the integrated Radeon:

```powershell
winget install ggml.llamacpp
# Pulls llama-b####-bin-win-vulkan-x64.zip and the VC++ Redistributable dependency.
# Open a NEW terminal afterwards so PATH picks up llama-server, then sanity-check:
llama-server --version
llama-server --list-devices      # the Radeon iGPU should show up as a Vulkan device
```

The Vulkan runtime ships with the AMD Adrenalin graphics driver (`vulkan-1.dll`);
only if it is missing, add it with `winget install KhronosGroup.VulkanRT`. ROCm is
**not** used — it does not support AMD iGPUs on Windows, so Vulkan is the path.

### DLP model (one-time GGUF conversion)

The default DLP model is the akiFQC **Conf-Extract** Japanese family. These
checkpoints ship as safetensors only, so convert one to GGUF once; `start.ps1`
then loads the local `.gguf`. The whole family shares one I/O contract
(`jp_confidential_extraction`), so changing model size is just a different
`-Model` — no code or config-profile change.

```powershell
# 1.2B (default). Produces .\models\LFM2.5-1.2B-JP-202606-Conf-Extract-Q4_K_M.gguf
.\scripts\convert-model-gguf.ps1

# or the smaller/faster 350M
.\scripts\convert-model-gguf.ps1 -Repo akiFQC/LFM2-350M-Conf-Extract-Japanese
```

Needs Python 3, git, and `llama-quantize` (from the winget llama.cpp). See the
script header for options (`-Quant`, `-OutDir`, `-HfToken`, ...). Once a prebuilt
`*-GGUF` repo exists upstream you can skip conversion and pass an `-hf` ref:
`.\start.ps1 -Model akiFQC/<repo>-GGUF:Q4_K_M`.

### Launch

`start.ps1` is a one-command launcher: it starts the sidecar, waits for it to become
healthy, then starts the proxy.

```powershell
# Option A: real LFM on the integrated Radeon iGPU via Vulkan, loading the
# converted Conf-Extract GGUF from .\models (see "DLP model" above). Default backend.
.\start.ps1

# Use a different family member (e.g. the 350M after converting it)
.\start.ps1 -Model .\models\LFM2-350M-Conf-Extract-Japanese-Q4_K_M.gguf

# Same, but keep inference on the CPU (always-works fallback)
.\start.ps1 -Backend cpu

# Option B: no model yet — deterministic keyword fallback (dev/demo, no sidecar)
.\start.ps1 -Classifier keyword
```

If more than one Vulkan device shows up, pin the iGPU with
`$env:GGML_VK_VISIBLE_DEVICES=0`. NPU (XDNA2) execution is future scope (see
`docs/spec-proxy.md` §8.5). To manage the sidecar yourself, start `llama-server`
separately and run `.\start.ps1 -NoSidecar`.

Then point Claude Code at the proxy:

```powershell
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"   # no /v1 suffix
claude
```

## Layout

| Path | Responsibility |
|------|----------------|
| `cmd/proxy` | entrypoint, config + classifier wiring, HTTP server |
| `internal/proxy` | request flow handler (parse → evaluate → sanitize → forward) |
| `internal/anthropic` | Messages API types (round-trip safe), block response, SSE, forwarder |
| `internal/dlp` | normalize, segment, rule guardrail, cache, LFM detector/policy |
| `internal/inference` | llama.cpp client (LFM) + keyword fallback classifier + `PromptProfile` |
| `internal/sanitizer` | structure-aware history unit removal + validation |
| `internal/storage` | SQLite audit log (no raw text / secrets) |
| `internal/config` | YAML config + safe defaults |

Configuration: see `config/config.example.yaml`. Full design: `docs/spec-proxy.md`
and the implementation plan it links to.
