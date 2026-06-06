# Driving Claude Code headlessly + its request-context injection (20260606 21:00)

## Issue

To verify the proxy with a real client we need to drive `claude` non-interactively
across multiple turns, and to point it at the proxy with usable auth. Two things
were non-obvious: how multi-turn works headlessly, and what `claude` puts in the
request body (which the DLP then inspects).

## Learnings

### Headless multi-turn (claude 2.1.167)

- `claude -p <prompt> --output-format json` prints one JSON object:
  `{result, is_error, session_id, subtype, ...}`. A DLP block is a *normal*
  assistant message, so `is_error=false`; detect a block by the text (the notice
  contains `ローカルDLP` and the sentinel `<!-- LOCAL_DLP_NOTE -->`).
- **Multi-turn across separate processes**: pass `--session-id <uuid>` on turn 1,
  then `--resume <uuid>` on later turns (same cwd — sessions are per-project). State
  persists on disk, so the proxy sees the **accumulated history**, including the
  block-notice assistant turn — which is what lets the sanitizer strip the blocked
  turn on the next request. This is far simpler than bidirectional `stream-json`.
- Keep a turn predictable: `--tools ""` (disables all tools → one `/v1/messages`),
  `--permission-mode dontAsk`, `--strict-mcp-config` (ignore the user's MCP servers),
  a minimal `--system-prompt`, and `--model <small>`.

### Auth: `--bare` vs subscription

- `--bare` reads Anthropic auth **strictly** from `ANTHROPIC_API_KEY` (or
  `apiKeyHelper` via `--settings`); it **never** reads subscription OAuth or the
  keychain, and it strips hooks/CLAUDE.md/MCP. Ideal for a clean, isolated child
  with a dummy key against a mock upstream.
- Subscription/OAuth is read **only in non-`--bare` mode**. It **does** flow through
  a custom `ANTHROPIC_BASE_URL`: the child sends `Authorization: Bearer <108-char>`
  + `anthropic-beta: oauth-2025-04-20,…` + `anthropic-version: 2023-06-01`, and the
  forwarder's allow-list (`internal/anthropic/forwarder.go`) passes all three
  upstream, so `api.anthropic.com` authenticates it. (Verified: a real answer comes
  back through the proxy.)

### What non-`--bare` Claude Code injects into the request body

Observed (via a mock upstream that records bodies/headers), even with
`--tools "" --system-prompt … --strict-mcp-config`:

- A `<system-reminder>` user block containing the **subscription account email**
  (`# userEmail …`) and the current date.
- `metadata.user_id` carrying `device_id` (64-hex), `account_uuid`, `session_id`.
- A `system` block with `x-anthropic-billing-header: cc_version=…; cc_entrypoint=sdk-cli; …`.
- Without `--strict-mcp-config`, the full **MCP tool definitions** (Gmail/Calendar/
  Drive) including `…googleapis.com/mcp/v1` URLs.
- A **separate background request** that asks the model to generate a session
  **title** (its content is the session summary — i.e. it echoes your prompt).

**Implication for the proxy:** the LFM (correctly, per its mission) flags this
injected context — typically as "internal network info" (the MCP/googleapis URLs,
device hashes) or personal data (the email) — so a *benign* turn is BLOCKED before
egress when using subscription auth. In `--bare` mode none of this is present, so
benign turns pass. This is product-relevant: for real subscription users, the proxy
will block on Claude Code's own telemetry/context unless that machine/account
context is recognized and allowed. Worth considering an allowance for known-benign
client-injected metadata, or documenting `--bare`-style usage, separately from
user-authored content.

## Refs
- docs/records/20260606/2100-e2e-multiturn-claude-driver.md
- proxy-server/test/e2e/multiturn_test.go (`TestDiagnoseNonBareContext` reproduces the dump)
- internal/anthropic/forwarder.go (header allow-list)
