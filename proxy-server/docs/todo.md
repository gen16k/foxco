# TODO / Deferred Issues

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

## JP fine-tune (LFM2.5-1.2B-JP-202606) を NPU 化して精度再検証

- Status: Deferred
- Discovered: 20260606 (docs/records/20260606/2233-npu-backend.md)
- 調査: 20260607 (docs/knowledges/20260607/0134-lfm2-finetune-npu-conversion.md)

### Detail

本番想定モデルは JP fine-tune `LFM2.5-1.2B-JP-202606`。NPU 経路は現状 AMD プリビルドの
`amd/LFM2-1.2B-ONNX_rai_1.7.1`（stock）のみで、**JP fine-tune を NPU で動かすには AMD の
token-fusion 変換（Quark 量子化 → token-fusion ONNX グラフ + seq 長別 DPU 制御パケット生成）が必要**。
LFM2 は標準 OGA フロー（`model_builder`）対象外（ハイブリッド conv/attention のため）で、AMD は
プリビルドのみ配布し **token-fusion 変換レシピを公開していない**。詳細・選択肢は調査 note 参照。

要点（調査 note 0134 より）:
- 前提: fine-tune が LFM2.5-1.2B と**同一アーキ・同一トークナイザ/語彙**であること（語彙変更があると graph/ctrlpkt 再生成が必須で難度上昇）。
- 選択肢A（低工数・未サポート）: 既存 graph に **FT 重みを再量子化して差し込む**（ctrlpkt は形状依存で再利用可）。ただし AMD の重みレイアウトが非公開でリバースエンジニアリング。
- 選択肢B（正攻法）: **AMD の token-fusion 変換ツール**で FT から再生成（要 AMD サポート/公開待ち、Quark + Ryzen AI 1.7.1 + おそらく VS2022 C++）。
- つなぎ: JP fine-tune は **Vulkan/CPU 上の GGUF**（`convert_hf_to_gguf.py` + `llama-quantize` Q4_K_M）で即運用可。その間 NPU は stock LFM2 のため**モデル差（NPU=stock / GPU・CPU=JP-FT）が生じる**点に注意。

### Why deferred / Blocked by

LFM2 の token-fusion 変換レシピが AMD 非公開。現状 proxy 既定は stock。NPU MVP はプリビルドで
動作・ベンチ確認を優先。JP-FT は当面 Vulkan/CPU(GGUF) で運用すれば機能要件は満たせる。

### Unblock condition

AMD が LFM2 token-fusion 変換ツール/JP 系プリビルドを提供する、または AMD サポートで FT の
token-fusion 変換が可能になったとき。あるいは選択肢A（重み差し込み）の実現可能性が確認できたとき。

## NPU 既定化の最終判断（ベンチゲート）と OGA 出力の堅牢性

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2233-npu-backend.md)

### Detail

`-Backend auto` は既に NPU を優先するが、デプロイ先（Ryzen AI 5 340 / Krackan Point）で `evalbench`
を回し、NPU の精度（false-positive 率が Vulkan 以下）・warm レイテンシ・フォールバック動作を確認し、
必要なら timeout を調整・記録する。NPU は schema 非強制（`reason_decision_prompt`）で寛容パースに
依存するため、上の「LFM fail-closes on benign input」と同じ benign 偽陽性リスクを共有する点に注意。
開発機 Strix Halo では GPU 最速だが 340 は iGPU 約 1/10 で NPU 優位の見込み（外挿、要実機確認）。

### Unblock condition

AMD NPU ドライバ + Ryzen AI 1.7.1 + ローカル LFM2 ONNX 導入済みの Ryzen AI 340 実機が用意できたら。
