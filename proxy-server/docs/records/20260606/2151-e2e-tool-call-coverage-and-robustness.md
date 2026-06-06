# E2E: tool-call coverage + deterministic robustness (20260606 21:51) #pending

## Motivation

Review feedback on the e2e harness: the tests passed `--tools ""`, so they only
exercised the user-text path — **tool calls were excluded**. But `tool_result` is a
primary DLP egress channel (a `Read`/`Bash` result can carry secrets), and the
sanitizer's structure-aware removal of `tool_use`/`tool_result` pairs is its own code
path. Neither was verified end-to-end with a real `claude`. Separately, re-running
the LFM multi-turn test exposed flakiness: the 1.2B model occasionally fail-closes on
benign input, so the benign→ALLOW turns were nondeterministic.

## Goal

Verify the **tool-call** egress + sanitize path end-to-end with a real child, and
make the whole e2e suite **deterministic** (no reliance on LFM judgment of benign
content) while keeping a reliable real-LFM showcase.

## Records

Added `TestToolResultSecretBlockedAndSanitized` (mock upstream, rule guardrail, no
sidecar/real API needed):
- The mock scripts a `tool_use(Read, <file>)` response to the main request only
  (skips the background session-title request and any follow-up already carrying a
  `tool_result`, served once) so the real child actually executes `Read`.
- Turn 1: the file's synthetic AWS key returns as a `tool_result` → proxy
  `BLOCK source=rule`, `upstream_called=false`; byte-check the key never egressed.
  (The prompt request *does* forward — that is how the child receives the tool_use —
  so the invariant is "secret absent from every forwarded body", not "nothing
  forwarded".)
- Turn 2: a benign follow-up forces structure-aware sanitization of the
  `tool_use`/`tool_result` pair; assert `sanitize removed_units>=1` and the key
  absent from the forwarded body.
- Driver extended with `tools` / `allowedTools` (`--tools Read --allowedTools Read`);
  mock extended with a `firstToolUse` plan + `writeMockToolUseSSE`.

Robustness restructure:
- `TestMultiTurnAllowBlockSanitize` now uses an allow-all classifier + the rule
  guardrail (turn 2 = synthetic AWS key), so benign turns reliably ALLOW and the
  secret reliably BLOCKs — deterministic, no sidecar.
- New `TestLFMBlocksSensitive` keeps the real-LFM showcase but asserts only the
  reliable direction (sensitive memo → `BLOCK source=lfm`, no egress); for sensitive
  content both a true positive and the tolerant fail-closed parse yield BLOCK.
- Removed `TestRuleAnchorBlock` (its single-turn rule-block case is now covered
  within the multi-turn sequence and the tool test).

Finding recorded: the LFM fail-closes on benign input (`reason: inert data with no
sensitive information` on "2+2"), a usability false positive →
`docs/todo.md` "LFM fail-closes on benign input".

## Results

All green; stable across repeated runs (`-count`):
- `TestMultiTurnAllowBlockSanitize`: ALLOW → `BLOCK source=rule` (no egress) → ALLOW
  with `sanitize removed_units=1`; `AKIAIOSFODNN7EXAMPLE` absent from all forwarded
  bodies. 2/2.
- `TestToolResultSecretBlockedAndSanitized`: real `Read` executed; `tool_result`
  key → `BLOCK source=rule`, key never egressed; follow-up turn sanitized the tool
  pair and forwarded without it. 3/3 + 2/2.
- `TestLFMBlocksSensitive`: memo → `BLOCK source=lfm` ×2 (main + title request),
  no egress. 2/2.
- Real-upstream tests unchanged (subscription forwarding + real-LFM egress block).

Local CI: `gofmt -l` (mine) clean, `go vet ./...` ok, `go vet -tags e2e ./test/e2e/`
ok, `go build ./...` ok, `go test ./...` ok (e2e excluded by tag).

## Refs
- docs/records/20260606/2100-e2e-multiturn-claude-driver.md
- docs/knowledges/20260606/2100-claude-code-headless-multiturn-and-context-injection.md
- proxy-server/test/e2e/README.md
