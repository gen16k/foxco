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

```powershell
# Option A: with the real LFM via a llama.cpp sidecar (LFM2.5 GGUF, auto-downloaded)
llama-server -hf LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M --host 127.0.0.1 --port 8791 --jinja
.\start.ps1

# Option B: no model yet — deterministic keyword fallback (dev/demo)
.\start.ps1 -Classifier keyword
```

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
| `internal/inference` | llama.cpp client (LFM) + keyword fallback classifier |
| `internal/sanitizer` | structure-aware history unit removal + validation |
| `internal/storage` | SQLite audit log (no raw text / secrets) |
| `internal/config` | YAML config + safe defaults |

Configuration: see `config/config.example.yaml`. Full design: `docs/spec-proxy.md`
and the implementation plan it links to.
