# TODO / Deferred Issues

## Proxy blocks Claude Code's own injected context for subscription users

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2100-e2e-multiturn-claude-driver.md)

### Detail

In non-`--bare` mode — the only mode that reads subscription/OAuth auth — Claude
Code injects machine/account context into every request body: the subscription
account email, `device_id`/`account_uuid`, an `x-anthropic-billing-header` system
block, MCP tool definitions (unless `--strict-mcp-config`), and a background
session-title request. The LFM correctly classifies this as sensitive (commonly
"internal network info" / personal data), so a **benign** user turn is BLOCKED
before egress. A real subscription user would see false-positive blocks on ordinary
prompts.

See docs/knowledges/20260606/2100-claude-code-headless-multiturn-and-context-injection.md
for the captured payloads.

### Why deferred / Blocked by

Out of scope for the e2e harness change (which only needed to verify the proxy's
invariants). Needs a product decision: how to treat client-injected metadata vs.
user-authored content — e.g. recognize/allow known-benign Claude Code context
blocks, exclude non-message metadata from classification, or document the
constraint. Touches the DLP policy, so it must be designed deliberately, not patched
ad hoc.
