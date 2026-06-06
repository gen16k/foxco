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

| Test | Upstream | Classifier | Auth | Proves |
|------|----------|-----------|------|--------|
| `TestMultiTurnAllowBlockSanitize` | mock | **real LFM** | dummy (`--bare`) | the full **allow → block → sanitize** sequence across 3 turns, plus byte-level no-egress |
| `TestRuleAnchorBlock` | mock | keyword + rule | dummy (`--bare`) | deterministic anchor: a synthetic AWS key is blocked (`source=rule`); harness is sound without the model |
| `TestRealUpstreamForwarding` | **real API** | allow-all | **subscription** | a real `claude` child gets a real model answer back **through the proxy** from `api.anthropic.com` (auth + forwarding) |
| `TestRealUpstreamBlocksSensitive` | **real API** | **real LFM** | **subscription** | the sensitive memo is **blocked from the real egress channel** by the LFM and never sent |
| `TestDiagnoseNonBareContext` | mock | allow-all | subscription | diagnostic: dumps what non-bare `claude` injects + which auth headers reach upstream |

### The clean multi-turn run

| Turn | Prompt | Decision | Verified |
|------|--------|----------|----------|
| 1 | benign question | **ALLOW**, forwarded | child gets a normal answer; `ALLOW upstream_called=true` |
| 2 | fictional confidential memo (no regex secret) | **BLOCK** `source=lfm`, `upstream_called=false` | child receives the DLP notice; nothing egressed |
| 3 | benign follow-up | **ALLOW**, history **sanitized** | child gets a normal answer; proxy logs `sanitize removed_units>=1` |

It additionally proves byte-for-byte that the turn-2 memo marker
(`AURORA-OPLAN-7F3KQ`) never appears in any forwarded body, on either egress
channel.

## How it works

The **real proxy** code path is wired exactly as `cmd/proxy/main.go` does
(`dlp.NewDetector` + `dlp.NewRuleEngine` + the real `inference.LlamaClient` +
`anthropic.NewForwarder` + `proxy.New`), hosted in-process on an `httptest` server
only so the test can attach an in-memory audit recorder and a buffered logger. The
child is pointed at that server via `ANTHROPIC_BASE_URL`, run with `--tools ""`
(no tools → one predictable `/v1/messages` per turn), `--permission-mode dontAsk`,
`--strict-mcp-config`, and a fixed minimal `--system-prompt`.

## Running

Prerequisites:
- `claude` on PATH (all tests).
- LFM sidecar healthy at `http://127.0.0.1:8791` (LFM tests only):
  ```powershell
  llama-server -hf LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M --host 127.0.0.1 --port 8791 --jinja -ngl 99
  ```

```powershell
# Free, deterministic mock pass (the core verification):
go test -tags e2e -run "MultiTurn|RuleAnchor" ./test/e2e/ -v

# Real Anthropic API via your Claude subscription (no per-call charge):
$env:FOXCO_E2E_REAL = "1"
go test -tags e2e -run RealUpstream ./test/e2e/ -v
Remove-Item Env:\FOXCO_E2E_REAL
```

## Why the real-upstream tests are split

The clean allow/block/sanitize sequence runs in **`--bare`** mode, which forces
`ANTHROPIC_API_KEY` auth and strips hooks/CLAUDE.md/MCP — so the request body
carries *exactly* our content.

Subscription/OAuth auth, however, is **only** read in **non-bare** mode. And
non-bare Claude Code injects machine/account context into every request: the
subscription account email, a `device_id`/`account_uuid`, billing headers, and
(unless `--strict-mcp-config`) the user's MCP tool definitions. The LFM correctly
flags that context as sensitive (`reason: contains internal network info` /
personal data), so a *benign* turn gets **blocked before it can reach the real
API**. That is the proxy doing its job, not a defect — but it means a clean
3-turn benign/allow assertion can't pass against the real API with the real LFM.

So the real-API checks are factored into what they can robustly prove:
- **Forwarding/auth** is verified with an allow-all classifier (a real answer comes
  back) — `TestRealUpstreamForwarding`.
- **Egress protection** is verified with the real LFM and the unambiguously
  sensitive memo, which blocks regardless of injected context —
  `TestRealUpstreamBlocksSensitive`.

Run `TestDiagnoseNonBareContext` to see the injected context and auth headers
yourself.

## Environment toggles

| Var | Default | Meaning |
|-----|---------|---------|
| `FOXCO_E2E_REAL` | unset | `1` → run the real-API (subscription) tests; else they skip |
| `FOXCO_E2E_MODEL` | `claude-haiku-4-5-20251001` | model the child uses |
| `FOXCO_E2E_LFM_ENDPOINT` | `http://127.0.0.1:8791` | llama.cpp sidecar base URL |

## Notes

- LFM decisions are model judgments: a flaky allow/block on the memo surfaces as a
  legible test failure (a real finding), not a harness bug. `TestRuleAnchorBlock` is
  deterministic by construction.
- The child runs in a `t.TempDir()` working directory; its session files are
  discarded with it.
- No DLP/security invariant is touched by this directory; it is test-only.
