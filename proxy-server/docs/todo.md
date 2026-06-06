# TODO / Deferred Issues

## matched_snippet に正確な機密スパンを格納する

- Status: Deferred
- Discovered: 20260606 (docs/records/20260606/2117-admin-observability-and-ui.md)

### Detail

`store_raw_text=true` のとき、検知でブロックされた「該当箇所」だけを `matched_snippet` に格納し、
管理UIでハイライト表示したい。現状は `prompt_text`（ライブターン全文）と `reason`（安全な理由）
のみを保存し、`matched_snippet` は常に NULL。

### Why deferred / Blocked by

`dlp.Evaluation`（internal/dlp/detector.go）はブロック理由とソースのみを返し、該当セグメントの
テキストや一致スパンを単独で露出しない。ルール検知は正規表現の一致位置が取れるが、LFM 検知は
理由文のみで位置情報を持たない。両者を一様に扱う API 拡張（例: `Evaluation.BlockSegment string`）が
必要で、今回のデモ範囲では `prompt_text` 全文表示で代替する。

### Unblock condition

`dlp.Detector.Evaluate` / `Classify` が該当セグメント（およびルール一致時はスパン）を返すよう
拡張し、handler から `matched_snippet` に truncate して格納する。
## LFM fail-closes on benign input (false-positive blocks)

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2151-e2e-tool-call-coverage-and-robustness.md)

### Detail

The LFM2.5-1.2B occasionally returns a verdict the tolerant parser cannot read as a
clean `ALLOW` for plainly benign input, so the proxy fail-closes to BLOCK. Observed
in the e2e harness: a benign "What is 2+2?" turn was blocked with reason
`inert data with no sensitive information` (the model reasoned it was benign, but the
output didn't yield an `ALLOW` token). Nondeterministic across sidecar warm states.
For sensitive content fail-closed is correct; for benign content it is a usability
false positive.

### Why deferred / Blocked by

The e2e harness worked around it (the deterministic allow/block/sanitize sequence
uses the rule guardrail; the LFM is asserted only in its reliable BLOCK-sensitive
direction — `TestLFMBlocksSensitive`). The real fix is in the LFM I/O contract:
stronger output constraint / grammar so a clean `ALLOW`/`BLOCK` token is always
produced, or a more robust parse, possibly a fine-tuned model. Touches
`internal/inference/profile.go` (PromptProfile) and the policy, so it needs
deliberate design + an eval set, not an ad-hoc patch.

### Unblock condition

A benign-input eval (e.g. ordinary coding prompts) showing an acceptable
false-positive rate after the I/O-contract change.

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
