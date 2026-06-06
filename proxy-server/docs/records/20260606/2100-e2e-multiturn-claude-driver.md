# E2E: drive a real `claude` child through the proxy, multi-turn (20260606 21:00) #67e3470

## Motivation

The proxy was only covered by in-process unit tests (mock upstream) and a one-off
manual smoke. We wanted to verify it **the way it is actually used** — a real
`claude` process pointed at the proxy via `ANTHROPIC_BASE_URL` — across **multiple
turns**, automatically. The hard part is that `claude` is interactive, so scripted
multi-turn verification looks impossible at first glance.

## Goal

An opt-in, reusable Go e2e harness that spawns a real `claude` child, drives it
through **allow → block → sanitize**, and asserts the proxy's invariants — against
the **real LFM** classifier and (headline) the **real Anthropic upstream** via the
Claude subscription, with a free mock-upstream mode for a byte-level no-egress proof.

## Records

Key enabler: `claude` is non-interactive under `-p`, and multi-turn state survives
**across separate process invocations** via `--session-id` / `--resume`. So a Go
test spawns one child per turn, reusing one conversation; the proxy thus sees the
accumulated history the sanitizer needs.

Added (test-only; no production code changed):
- `test/e2e/multiturn_test.go` (`//go:build e2e`): the real proxy pipeline wired as
  `cmd/proxy/main.go` does, hosted on an `httptest` server with an in-memory audit
  recorder + buffered logger; a mock Anthropic upstream (records bodies, emits real
  SSE); a `claude` child driver; and the assertions.
- `test/e2e/doc.go` (no build tag) so default `go test ./...` stays clean
  (`[no test files]`), unaffected by this directory.
- `test/e2e/README.md`.

Tests: `TestMultiTurnAllowBlockSanitize` (mock + real LFM, the 3-turn sequence),
`TestRuleAnchorBlock` (deterministic rule guardrail anchor — needs no model),
`TestRealUpstreamForwarding` and `TestRealUpstreamBlocksSensitive` (real API via
subscription), `TestDiagnoseNonBareContext` (diagnostic).

Child invocation that keeps requests predictable: `-p <prompt> --output-format json
--tools "" --permission-mode dontAsk --strict-mcp-config --system-prompt <minimal>
--model claude-haiku-4-5-20251001` plus `--session-id`/`--resume`. Mock mode adds
`--bare` (forces `ANTHROPIC_API_KEY`, strips hooks/CLAUDE.md/MCP); real mode omits
`--bare` (the only mode that reads subscription OAuth).

Finding that shaped the design: non-`--bare` Claude Code injects account/machine
context (subscription email, `device_id`/`account_uuid`, billing headers, MCP tool
defs) into every request body. The LFM correctly flags it, so a *benign* turn gets
blocked before reaching the real API. Since subscription auth requires non-`--bare`,
a clean benign-allow turn can't pass against the real API with the real LFM — so the
real-API checks were split into forwarding (allow-all classifier, real answer comes
back) and egress-protection (real LFM blocks the unambiguous memo regardless of
injected context). Details in
`docs/knowledges/20260606/2100-claude-code-headless-multiturn-and-context-injection.md`.

## Results

All green (sidecar = LFM2.5-1.2B on the AMD Radeon iGPU via Vulkan; child =
`claude` 2.1.167):
- **Multi-turn (mock + real LFM):** turn 1 benign → `ALLOW`+`ALLOW` (count_tokens &
  messages, `upstream_called=true`); turn 2 memo → `BLOCK source=lfm
  upstream_called=false`, child received the `⚠️ ローカルDLP…` notice; turn 3 benign
  → `ALLOW` with `sanitize removed_units=1`. Marker `AURORA-OPLAN-7F3KQ` absent from
  all forwarded bodies. ~4.9 s.
- **Rule anchor (mock):** synthetic AWS key → `BLOCK source=rule`, 0 egress. ~1 s.
- **Real upstream forwarding (subscription):** child got a real answer `PINGOK` back
  through the proxy; `ALLOW upstream_status=200` ×2. Confirms subscription OAuth
  (`Authorization: Bearer` + `anthropic-beta: oauth-2025-04-20,…`) flows through a
  custom `ANTHROPIC_BASE_URL` and the forwarder passes it upstream.
- **Real upstream blocks sensitive (subscription + real LFM):** memo → `BLOCK
  source=lfm` ×2 (the main request *and* the background session-title request, which
  summarizes the memo); nothing reached `api.anthropic.com`.

Local CI: `gofmt -l` clean, `go vet ./...` ok, `go build ./...` ok, `go test ./...`
ok (e2e excluded by tag), `go vet -tags e2e ./test/e2e/` ok.

## Refs
- docs/knowledges/20260606/2100-claude-code-headless-multiturn-and-context-injection.md
- proxy-server/test/e2e/README.md
- docs/records/20260606/1931-amd-vulkan-migration.md
