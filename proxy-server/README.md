# Local LFM DLP Proxy

A local Data-Loss-Prevention proxy that sits between **Claude Code** and the
Anthropic API. It inspects every outbound request, asks a local **LFM (Liquid
Foundation Model)** whether the content is safe to send, **blocks** sensitive
egress (secrets, credentials, internal/PII/proprietary data), and sanitizes
re-sent history so a once-blocked secret never leaks on a later turn.

This is primarily a **showcase for on-device LFM classification**: the LFM is the
primary NG/OK classifier, with a small deterministic rule guardrail as insurance.

> **Threat model — read this first.** This is an *advisory* control for the
> honest-mistake case (a developer accidentally pasting a secret). By default it
> runs in **transparent mode**: a Windows service redirects `api.anthropic.com`
> via the hosts file and terminates TLS with a locally-trusted CA, so no
> `ANTHROPIC_BASE_URL` is needed and casual bypass is harder than the env-var
> mode. It is still **not** a tamper-proof enforcement boundary — a user with
> Administrator rights can stop the service, remove the hosts entry, or uninstall
> the CA. Real enforcement requires a network egress chokepoint (forward proxy /
> firewall) the user cannot disable. See `docs/spec-proxy.md` §5.

## How it works

```
Claude Code --(transparent: hosts api.anthropic.com->127.0.0.1, TLS :443 via local CA)--> Proxy
            --(or legacy: ANTHROPIC_BASE_URL=http://127.0.0.1:8787)-----------------------> Proxy
  parse  -> normalize -> segment -> [cache] -> rule guardrail -> LFM (NG/OK)
         -> BLOCK (assistant message / SSE, no egress)          (live turn NG)
         -> sanitize history (remove sensitive tool/turn units) -> forward upstream
              (upstream resolved via external DNS so the redirect doesn't loop back)
```

- **Binary classifier.** The LFM returns `{reason, decision: ALLOW|BLOCK}`;
  `BLOCK -> block`, `ALLOW -> forward`. The rule guardrail short-circuits to
  BLOCK on unambiguous secrets (AWS/Anthropic/Google keys, private-key blocks,
  ...). The full prompt/schema/parse contract is a swappable `PromptProfile`
  (`internal/inference/profile.go`) — select via `inference.profile`, or
  override just the prompt with `inference.system_prompt_file`, when you
  fine-tune a model. Parsing is tolerant (llama.cpp does not always enforce the
  JSON schema), and the segment is wrapped in `<<<DATA>>>` delimiters so the
  small model treats it as inert data, not instructions.
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

### Launch

`start.ps1` is a one-command launcher: it starts the sidecar, waits for it to become
healthy, then starts the proxy **and the admin web UI** (`web/`, Next.js) on
`http://127.0.0.1:3939` — loopback only. The launcher passes the admin API address
and `admin.auth_token` from the chosen config to the UI automatically. Pass
`-NoWeb` to skip the UI. Ctrl+C stops everything it started.

```powershell
# Option A: real LFM on the integrated Radeon iGPU via Vulkan (LFM2.5 GGUF,
# auto-downloaded on first run). This is the default backend.
.\start.ps1

# Same, but keep inference on the CPU (always-works fallback)
.\start.ps1 -Backend cpu

# Option B: no model yet — deterministic keyword fallback (dev/demo, no sidecar)
.\start.ps1 -Classifier keyword
```

If more than one Vulkan device shows up, pin the iGPU with
`$env:GGML_VK_VISIBLE_DEVICES=0`. NPU (XDNA2) execution is future scope (see
`docs/spec-proxy.md` §8.5). To manage the sidecar yourself, start `llama-server`
separately and run `.\start.ps1 -NoSidecar`.

### Connect Claude Code

**Transparent mode (default).** No `ANTHROPIC_BASE_URL` needed — a Windows service
redirects `api.anthropic.com` to the proxy and terminates TLS with a locally-trusted
CA. One-time setup, from an **elevated** PowerShell in this directory:

```powershell
.\install.ps1          # builds, installs the CA, registers the service + sidecar logon task
```

`install.ps1` does **not** start the service immediately (so it won't disturb a
running Claude session). The redirect activates on next boot/logon, or start it now:

```powershell
.\proxyctl.ps1 start   # start sidecar (your session) + service (redirect + :443)
.\proxyctl.ps1 status  # service / sidecar / redirect state
.\uninstall.ps1        # revert everything (elevated)
```

**Legacy proxy mode (fallback).** Set `mode: proxy` in the config and point Claude
Code at the plain-HTTP listener:

```powershell
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"   # no /v1 suffix
claude
```

Open the admin UI at `http://127.0.0.1:3939` to see detection counts, contents, and
prompt history. Prompt bodies are shown only when the proxy runs with
`storage.store_raw_text: true` (off by default). See `web/README.md` for details.

## Layout

| Path | Responsibility |
|------|----------------|
| `cmd/proxy` | entrypoint, config + classifier wiring, dual listeners, Windows service |
| `internal/proxy` | request flow handler (parse → evaluate → sanitize → forward) + passthrough |
| `internal/anthropic` | Messages API types (round-trip safe), block response, SSE, forwarder |
| `internal/dlp` | normalize, segment, rule guardrail, cache, LFM detector/policy |
| `internal/inference` | llama.cpp client (LFM) + keyword fallback classifier |
| `internal/sanitizer` | structure-aware history unit removal + validation |
| `internal/storage` | SQLite audit log (no raw text / secrets) |
| `internal/config` | YAML config + safe defaults |
| `internal/mitm` | Name-Constrained root CA + on-the-fly leaf certs (transparent TLS) |
| `internal/upstreamdial` | hosts-bypassing transport so the upstream forward reaches the real API |
| `internal/hostsfile` | crash-safe hosts-file redirect block (add on start / remove on stop) |

Configuration: see `config/config.example.yaml`. Full design: `docs/spec-proxy.md`
and the implementation plan it links to.
