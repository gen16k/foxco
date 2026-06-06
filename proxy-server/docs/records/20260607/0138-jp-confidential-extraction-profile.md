# jp_confidential_extraction プロファイル追加（FT抽出モデル対応） (20260607 01:38) #d3c8ad1

## Motivation

別メンバーが `akiFQC/japanese-confidential-information-extraction-sft` で LFM2系モデルを
ファインチューニング中。公開された当該データセットの入出力契約を確認したところ、現行の
`reason_decision` プロファイル（英語の ALLOW/BLOCK 分類器）とは根本的に異なる：

- system: 日本語の「11カテゴリ機密情報抽出」指示
- user: 区切り（`<<<DATA>>>`）も `segment_type` ヒントも無い **素のテキスト**
- output: ALLOW/BLOCK ではなく 11キー固定の **抽出 JSON**（各値は抽出文字列の配列）

ユーザ要望：モデルが想定する入出力形式を確認し、合っていなければ proxy 側を合わせる。

## Goal

FT 抽出モデルの入出力契約に一致する新 `PromptProfile`（`jp_confidential_extraction`）を
追加し、`inference.profile` で選択可能にする。呼び出し側は変更しない（プロファイル機構が
まさにこの差し替えのために存在する）。判定方針：抽出 11 カテゴリのいずれかが非空なら BLOCK。

## Records

- `internal/inference/profile.go`
  - `PromptProfile` に `MaxTokens int` を追加（0 = client 既定にフォールバック）。
  - `jpConfidentialExtractionProfile()` を追加し `init()` で登録。
    - System: データセットの日本語プロンプトをバイト一致でコピー（全/半角・改行に敏感な
      1.2B モデルのため逐語）。datasets-server から `Invoke-RestMethod` で取得し全行同一を確認。
    - BuildUser: `in.Text` を素通し（`<<<DATA>>>`・`segment_type` 無し＝学習分布に一致）。
    - Schema: 11キー object（各 `array<string>`、全 required、`additionalProperties:false`）。
    - Parse: `map[string][]string` にデコードし `jpExtractionSensitiveCategories` の順で非空
      カテゴリを収集。1つでもあれば NG。`ShortReason` は **カテゴリ名のみ**（抽出値は載せない
      ＝監査ログ/ブロック文言に機密値を残さない不変条件）。完全なデコード失敗時は error を
      返し caller を fail-closed させる。`MaxTokens: 384`。
- `internal/inference/client.go`
  - `defaultMaxTokens=128` と `(*LlamaClient).maxTokens()` を追加し、ハードコードの
    `MaxTokens:128` を `c.maxTokens()` に置換。抽出 JSON はテンプレだけで ~90-120 トークン
    あり 128 では切れて parse 失敗→誤ブロックの恐れがあるため。
- `internal/inference/profile_jp_extraction_test.go`（新規）
  - parse のユニットテスト（空→ALLOW／単一・複数カテゴリ→BLOCK かつ理由はカテゴリ名のみで
    値を含まない／キー欠落・null は非発火／prose 包み許容／truncated・非JSON・scalar は
    error=fail-closed）、登録・スキーマ形状・素通し BuildUser、httptest で end-to-end と
    `max_tokens=384`（既定プロファイルは 128 維持）を検証。

## Results

- `go build ./...` / `go vet ./...` OK。`go test ./internal/inference/...` OK。
- `gofmt`：Windows チェックアウトは CRLF（`core.autocrlf=true`）のため作業ツリーでは全 .go が
  `-l` に出るが、コミット時 LF 正規化され CI は green（LF 正規化コピーで逐一クリーンを確認）。
- 設定：`config/config.example.yaml` の `profile` コメントに `jp_confidential_extraction` を
  追記（FT チェックポイントを sidecar にロードし `model` を合わせた上で opt-in）。既定は
  `reason_decision` のまま（FT 未配備）。`rule_guardrail.enabled` コメントに「false=LFM単独 /
  true=両層（本番推奨）」を明記（concern #1「フラグで切替」への回答）。

## セキュリティ不変条件の明示的な弱体化（ユーザ確認済み）

`CLAUDE.md` の不変条件「検査テキストは不活性データとして `<<<DATA>>>` で包む」を、本プロファイル
では **意図的に外す**（素テキスト送出）。理由：抽出モデルには注入で反転し得る ALLOW/BLOCK の
判定フィールドが無く、最悪でも抽出漏れ（false negative）に留まり、決定論的 rule guardrail と
fail-closed が後段で担保する。実例付きで説明し承認済み。詳細は `docs/decisions.md`。

## Refs
- https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft
- docs/decisions.md（20260607 jp_confidential_extraction の <<<DATA>>> ラッパ除去）
- docs/todo.md（抽出トリガ集合の構成化 ほか）
