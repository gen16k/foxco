# TODO / Deferred Issues

## 透過モード(:443のみ)だと admin UI が admin API に到達できない

- Status: Open
- Discovered: 20260607 (admin-UI 機能を透過インターセプションへマージした際に判明)

### Detail

admin API は proxy と同じ mux に登録され、`app.servers` の全リスナで提供される。既定
`mode: transparent` ではリスナは `:443`（TLS, SNI=api.anthropic.com のリーフ）のみで、平文
`:8787` は上がらない。一方 admin UI（`start.ps1` が起動、`web/`）は `server.listen_addr`
（既定 `127.0.0.1:8787`）の平文 HTTP へ接続する想定。よって transparent 単独だと admin UI は
admin API に繋がらない（`mode: both` か `proxy` で `:8787` を上げれば到達）。さらに `start.ps1`
のコンソール起動は transparent 構成だと `:443` バインド＋hosts 編集（要管理者・ホスト影響）を
行うため、dev フローとして整理が要る。

### Why deferred / Blocked by

両機能の既定ワークフローが異なるための統合課題。設計判断が必要：admin API を常に平文
localhost（`:8787` か専用 admin ポート）でも提供するか、admin UI を transparent 前提へ寄せるか、
デモ時は `mode: both` を案内するか。

### Unblock condition

admin UI ＋ transparent モード併用の方針を決め、リスナ構成（または専用 admin リスナ）を実装する。

## Claude Code の CA 信頼（当初「Windowsストアを見ない」懸念 → 実機で否定）

- Status: Resolved
- Discovered: 20260606 (docs/records/20260606/2230-sandbox-integration-test.md)
- Resolved: 20260606 (docs/records/20260606/2339-real-claude-code-lfm-verify.md)

### Detail

当初は「Claude Code は Node.js で、Node は既定で OS 証明書ストアを見ずバンドル CA を使うため、
`install.ps1` の `LocalMachine\Root` 導入だけでは信頼されず `NODE_EXTRA_CA_CERTS` が要る」と
懸念していた。**Sandbox で実 Claude Code 2.1.167 を使って検証した結果、これは否定された。**
Claude Code は既定 `CLAUDE_CODE_CERT_STORE="bundled,system"` で **Windows システムストアを参照**
するため、`install.ps1` の CA 導入だけで透過インターセプトが成立した（`NODE_EXTRA_CA_CERTS`
無しの run でも `/v1/messages` が proxy に到達）。`NODE_EXTRA_CA_CERTS` を明示しても成立。

### 結論

`install.ps1` の現行 CA 導入で実 Claude Code に十分。追加の環境変数設定は不要（任意で併用可）。
schannel 系の他クライアント（curl 既定等）は失効確認のため `--ssl-revoke-best-effort` 相当が要る
点は別項参照。

### Unblock condition

実 Claude Code を Sandbox 等で起動し、透過モードでブロック/許可が成立することを確認する段。

## 動的発行リーフ/CA に CRL/OCSP が無く schannel が失効確認で失敗

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2230-sandbox-integration-test.md)

### Detail

`internal/mitm` が発行するリーフ/ルートCAには CRL Distribution Point / OCSP(AIA) が無い。
schannel 系クライアント（Windows 同梱 `curl` 既定・.NET `HttpClient`・WinHTTP）は失効確認を
既定で強制するため、チェーン自体は信頼されても `CRYPT_E_NO_REVOCATION_CHECK (0x80092012)` で
ハンドシェイクがハードフェイルする。Sandbox テストでは `curl --ssl-revoke-best-effort` で回避
した（失効確認のみ緩和、チェーン信頼・ホスト名検証は維持）。主対象の Claude Code(Node/OpenSSL)
は既定で失効確認しないため影響しないが、同一マシンの他 schannel クライアントが
`api.anthropic.com` を叩くと失敗し得る。

### Why deferred / Blocked by

主対象クライアント（Node）には無影響で、テストはフラグで回避済み。恒久対応するなら発行証明書に
失効情報を持たせない MITM CA の慣行に合わせて「クライアント側で soft-fail させる」前提を明文化
するか、必要なら CRL/OCSP を持たせる実装を足す。

## 透過パススルー経路は DLP 検査外

- Status: Deferred
- Discovered: 20260606 (docs/records/20260606/2101-transparent-https-interception.md)

### Detail

透過モードでは `api.anthropic.com` への全パスが proxy に届くが、DLP 検査するのは
`/v1/messages` と `/v1/messages/count_tokens` のみ。それ以外（例 `GET /v1/models`、
将来の `POST /v1/files` 等）は内容を検査せず上流へ透過転送する。Claude Code を壊さない
ための意図的なカバレッジ外で、監査ログには `decision=PASSTHROUGH`（メソッド+パスのみ、
本文は不記録）として残し、黙示のバイパスにはしていない。

### Why deferred / Blocked by

現状 Claude Code が本文を運ぶのは `/v1/messages` のみ。他経路のDLP化は parse 方式が
パスごとに異なり、誤検査で正規エンドポイントを壊すリスクがある。新たな本文付き egress
経路（ファイルアップロード等）を Claude Code が常用し始めたら検査対象に加える。

### Unblock condition

`/v1/messages` 以外に本文（プロンプト/機密になり得るデータ）を運ぶ経路が実利用される。

## パススルー要求ボディは 32MiB でバッファ上限

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2101-transparent-https-interception.md)

### Detail

catch-all パススルーは要求ボディを `defaultMaxBody`(32MiB) まで読み切ってから転送する。
巨大アップロードがあると切り詰められうる。通常の Claude Code 利用では問題ないが、必要に
なれば `ForwardRaw` を `io.Reader` ストリーミングに変更してボディ無制限で素通しする。

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
