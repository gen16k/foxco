# TODO / Deferred Issues

## 抽出プロファイルのブロック対象カテゴリを構成可能にする

- Status: Open
- Discovered: 20260607 (docs/records/20260607/0138-jp-confidential-extraction-profile.md)

### Detail

`jp_confidential_extraction` は 11 カテゴリのいずれか非空で BLOCK する faithful な既定。だが
`human_name` / `company_name` / `address` は一般的なエンジニアリング文・公開情報にも現れ、
`reason_decision` 比で誤検知（過剰ブロック）が増えうる。現状トリガ集合は profile.go の定数
スライス `jpExtractionSensitiveCategories` で構成不可。

### Why deferred / Blocked by

FT モデル自体が未配備で実運用の誤検知傾向が未測定。まず faithful 既定で配備し、観測後に
`inference.extraction_block_categories`（既定=全11）でサブセット指定できるようにするのが妥当。

## 抽出プロファイルの max_tokens 上書きとプロファイル切替時のキャッシュ陳腐化

- Status: Open
- Discovered: 20260607 (docs/records/20260607/0138-jp-confidential-extraction-profile.md)

### Detail

(1) `max_tokens` はプロファイル付随（抽出=384）で `inference.max_tokens` の上書きは未提供。密な
入力で 384 でも切れるなら 512 へ引き上げが要る。(2) `cache.persist_sqlite=true` でプロファイルを
跨いで再起動すると、指紋→判定キャッシュが旧プロファイルの判定を返し得る（プロファイル名を
キャッシュキー/名前空間に含めていない）。本変更では未対応。

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
