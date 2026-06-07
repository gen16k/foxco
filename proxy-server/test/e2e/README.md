# End-to-end multi-turn verification (real `claude` child)

**Opt-in** tests that drive the actual `claude` binary, headless, through the proxy
— verifying the proxy the way it is really used. Gated behind the `e2e` build tag,
and they self-skip when prerequisites are missing, so plain `go test ./...` never
runs them.

The obstacle this solves: `claude` is interactive. But it has a headless mode
(`-p`), and multi-turn state survives across separate process invocations via a
session id (`--session-id` / `--resume`). So the Go test spawns one child per turn,
reusing one conversation, and asserts on the proxy's authoritative audit trail.

## Tests

| Test | Upstream | Classifier | What it proves |
|------|----------|-----------|----------------|
| `TestMultiTurnAllowBlockSanitize` | mock | rule guardrail | the full **allow → block → sanitize** sequence across 3 turns (user text), plus byte-level no-egress — deterministic |
| `TestToolResultSecretBlockedAndSanitized` | mock | rule guardrail | the **tool-call** channel: a real child runs `Read`; the secret in the `tool_result` is **blocked**, and the `tool_use`/`tool_result` pair is **sanitized** from history on the next turn — byte-verified |
| `TestLFMBlocksSensitive` | mock | **real LFM** | the LFM (on the iGPU) **blocks the sensitive memo** (`source=lfm`), no egress |
| `TestRealUpstreamForwarding` | **real API** | allow-all | a real `claude` child gets a real model answer back **through the proxy** from `api.anthropic.com` (subscription auth + forwarding) |
| `TestRealUpstreamBlocksSensitive` | **real API** | **real LFM** | the sensitive memo is **blocked from the real egress channel** and never sent |
| `TestDiagnoseNonBareContext` | mock | allow-all | diagnostic: dumps what non-bare `claude` injects + which auth headers reach upstream |

### The allow → block → sanitize sequence

| Turn | Prompt | Decision | Verified |
|------|--------|----------|----------|
| 1 | benign question | **ALLOW**, forwarded | child gets a normal answer; `ALLOW upstream_called=true` |
| 2 | synthetic AWS key | **BLOCK** `source=rule`, `upstream_called=false` | child receives the DLP notice; nothing egressed |
| 3 | benign follow-up | **ALLOW**, history **sanitized** | child gets a normal answer; proxy logs `sanitize removed_units>=1`; the key is gone from the forwarded body |

`TestToolResultSecretBlockedAndSanitized` runs the same shape but the secret arrives
as a **`tool_result`** (the child actually executes a `Read` of a file containing an
AWS key), exercising the tool-call egress channel and the structure-aware removal of
`tool_use`/`tool_result` units.

## Why the sequence uses the rule guardrail, not the LFM

The allow/block/sanitize sequence depends on **benign turns reliably being
allowed**. A 1.2B LFM occasionally **fail-closes on benign input** (it emits a
verdict the tolerant parser can't read as `ALLOW`, so the proxy — correctly —
blocks; observed reason e.g. `inert data with no sensitive information`). For
*sensitive* content that fail-closed default is exactly right, so an LFM **block**
of the unambiguous memo is 100% reliable — which is what `TestLFMBlocksSensitive`
asserts. The deterministic sequence therefore uses the always-on rule guardrail, and
the LFM is exercised only in its reliable direction. (See
`docs/todo.md` → "LFM fail-closes on benign input".)

## How it works

The **real proxy** code path is wired exactly as `cmd/proxy/main.go` does
(`dlp.NewDetector` + `dlp.NewRuleEngine` + the real `inference.LlamaClient` +
`anthropic.NewForwarder` + `proxy.New`), hosted in-process on an `httptest` server
only so the test can attach an in-memory audit recorder and a buffered logger. The
child is pointed at that server via `ANTHROPIC_BASE_URL`, run with
`--permission-mode dontAsk`, `--strict-mcp-config`, a fixed minimal `--system-prompt`,
and `--tools ""` (no tools → one predictable `/v1/messages` per turn) — except the
tool test, which uses `--tools Read --allowedTools Read` and scripts the `tool_use`
from the mock so the child executes the tool.

## Running

Prerequisites:
- `claude` on PATH (all tests).
- LFM sidecar healthy at `http://127.0.0.1:8791` (the `LFM`/`RealUpstreamBlocks` tests only):
  ```powershell
  llama-server -hf LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M --host 127.0.0.1 --port 8791 --jinja -ngl 99
  ```

```powershell
# Free, deterministic mock pass (no sidecar needed for these two):
go test -tags e2e -run "MultiTurn|ToolResult" ./test/e2e/ -v

# Real LFM showcase (needs the sidecar):
go test -tags e2e -run LFMBlocksSensitive ./test/e2e/ -v

# Real Anthropic API via your Claude subscription (no per-call charge):
$env:FOXCO_E2E_REAL = "1"
go test -tags e2e -run RealUpstream ./test/e2e/ -v
Remove-Item Env:\FOXCO_E2E_REAL
```

## Environment toggles

| Var | Default | Meaning |
|-----|---------|---------|
| `FOXCO_E2E_REAL` | unset | `1` → run the real-API (subscription) tests; else they skip |
| `FOXCO_E2E_MODEL` | `claude-haiku-4-5-20251001` | model the child uses |
| `FOXCO_E2E_LFM_ENDPOINT` | `http://127.0.0.1:8791` | llama.cpp sidecar base URL |

## Notes

- The real-API tests are split (forwarding vs. egress-protection) because non-bare
  Claude Code — the mode subscription auth requires — injects account context
  (email, device id, billing, MCP) that the DLP legitimately flags; a clean benign
  turn can't pass against the real API with the real LFM. See
  `docs/knowledges/20260606/2100-claude-code-headless-multiturn-and-context-injection.md`.
- The child runs in a `t.TempDir()` working directory; its session files are
  discarded with it.
- No DLP/security invariant is touched by this directory; it is test-only.
