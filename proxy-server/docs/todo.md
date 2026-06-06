# TODO / Deferred Issues

## matched_snippet に正確な機密スパンを格納する

- Status: Resolved
- Discovered: 20260606 (docs/records/20260606/2117-admin-observability-and-ui.md)
- Resolved: 20260606 (docs/records/20260606/2327-matched-snippet-highlight.md)

### Detail

`store_raw_text=true` のとき、検知でブロックされた「該当箇所」だけを `matched_snippet` に格納し、
管理UIでハイライト表示したい。以前は `prompt_text`（ライブターン全文）と `reason`（安全な理由）
のみを保存し、`matched_snippet` は常に NULL だった。

### Resolution

`dlp.Detector.Classify` の `Result` に `Match`、`Evaluate` の `Evaluation` に `BlockMatch` を追加。
ルール検知は `RuleEngine.MatchSpan` で正規表現の一致部分文字列（=機密の値そのもの）を、LFM 検知は
該当セグメント全文を `Match` に載せる。handler は `store_raw_text=true` のときだけ `snippetPtr` で
truncate して `matched_snippet` に格納する（`prompt_text` と同じオプトインゲート）。`reason` には
従来どおりルール名のみで値は入れない。管理UIの詳細ドロワーは `matched_snippet` を本文中で `<mark>`
ハイライトする。LFM の場合はセグメント単位の粗いスパンに留まる（正規化差で本文と一致しないと
ハイライトされず、別枠表示のみになる）が、ルール検知は正確なスパンが出る。

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
