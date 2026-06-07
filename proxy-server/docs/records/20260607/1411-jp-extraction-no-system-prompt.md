# jp_confidential_extraction：システムプロンプトを廃止（チューニング前提に整合） (20260607 14:11) #TBD

## Motivation

実機で良性入力（`hi` / `this is safe`）が誤ブロックされた件を調査した結果、原因は
**「新・抽出モデルに旧 `reason_decision` プロンプトを食わせていたデプロイ不整合」** だった
（稼働 config が `profile: reason_decision` のまま）。これは PR #14（install.ps1 の config 同期）
で解消済み。

加えてユーザーより、akiFQC Conf-Extract 系チェックポイントは **チューニングとして system
プロンプト不要に設計されている**（指示は重みに内在）との情報を得た。`jp_confidential_extraction`
プロファイルは従来データセットの system 文をバイト一致で送っていたが、これは学習分布外であり
不要。送らない方が正しい。

## Goal

- `jp_confidential_extraction` から system プロンプトを廃止し、ユーザーターン（生テキスト）のみを
  送る。空 System のときはクライアントが system メッセージ自体を送らない（空メッセージも送らない）。
- 既存の挙動（生テキスト・DATAラッパ無し・11カテゴリ抽出→非空でBLOCK・MaxTokens 384）は不変。
- `inference.system_prompt_file` による上書き経路は温存（将来チェックポイントが必要とする場合に備える）。

## Records

- `internal/inference/client.go`:
  - `Classify` のメッセージ構築を条件分岐化。`c.profile.System != ""` のときだけ system
    メッセージを付与し、空なら user メッセージのみ送る。デフォルト系プロファイル（System 非空）は
    従来どおり system を送るため無影響。
- `internal/inference/profile.go`:
  - `jpConfidentialExtractionProfile()` の `System` を空に。プロファイルコメントの「2つの逸脱」の
    うち System 関連を「system プロンプトを送らない（チューニング前提）」へ改訂。
  - 未使用となった定数 `jpConfidentialExtractionSystem`（データセット system 文）を削除。
- テスト:
  - `internal/inference/client_test.go`:
    `TestClassifyOmitsSystemMessageWhenProfileHasNone` を追加。空 System プロファイルでは
    送信リクエストに system ロールが**無い**こと、非空（DefaultProfile）では**有る**ことを、
    httptest でリクエストボディを捕捉して検証。
  - `internal/inference/profile_jp_extraction_test.go`:
    登録テストのアサーションを `System != ""` 失敗 → `System == ""` 必須に変更。

## Results

- `go build ./...` / `go test ./...` 緑。`go test ./internal/inference/...` 緑。
- gofmt：変更4ファイルとも clean（CRLF差分を除く）。
- ライブ検証（参考・本コミット前に実施）: Conf-Extract GGUF を llama-server で起動し、実プロファイル
  （schema/temp 0）で `hi`/`this is safe`/コードタスク → 全カテゴリ空（ALLOW）、日本語PII →
  `email_address`/`human_name`/`phone_number` 抽出（BLOCK）。生出力は常に strict JSON、パース正常。
  → 誤ブロックはモデル/パースではなくプロファイル不整合が原因と確定。

## 運用メモ

- 反映には新 `proxy.exe` の再デプロイが必要: 管理者で `.\install.ps1`（config 同期込み）→
  `Start-Service PromptGate`。System を送らないだけなので空 System プロファイルでも sidecar 側は無変更。
- 別チェックポイントで system が必要になった場合は `inference.system_prompt_file` で個別に上書き可能。

## Refs

- https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft
- docs/records/20260607/0138-jp-confidential-extraction-profile.md
- docs/records/20260607/1345-install-sync-latest-model.md（config 同期＝PR #14）
