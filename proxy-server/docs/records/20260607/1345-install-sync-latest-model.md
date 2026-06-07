# install.ps1 を再実行で最新モデルへ同期（既定モデル更新 + config同期） (20260607 13:45) #aff27fd

## Motivation

PR #11 で既定 DLP モデルを akiFQC Conf-Extract JP（profile `jp_confidential_extraction`）へ切替えたが、
`install.ps1` が取り残されていた:

- 既定 `-Model` が旧 `LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M` のまま → サイドカーが旧GGUFをロード。
- 既存の `config.yaml` を上書きしない（無い時だけ example をコピー）→ 旧 `profile: reason_decision` /
  `model: LFM2.5-1.2B` が残り、抽出モデルでなく汎用Allow/Block分類のまま動いてしまう。

実機でも、稼働 config が旧プロファイルだったために良性入力（`hi`/`test`）が誤ブロックされていた。
「install を再実行すれば最新化される」状態にしたい（ユーザー要望）。

## Goal

- `install.ps1` を再実行するだけで、稼働デプロイのモデル／I-O契約が最新（Conf-Extract JP +
  `jp_confidential_extraction`）に揃うようにする。既存 config の他設定は保持。

## Records

- `install.ps1`:
  - 既定 `-Model` を `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M` に更新。
  - パラメータ追加: `-ModelLabel`（config `inference.model` ラベル、既定 `LFM2.5-1.2B-JP-202606-Conf-Extract`）、
    `-Profile`（config `inference.profile`、既定 `jp_confidential_extraction`）。
  - 既存 config に対し、`inference.model` / `inference.profile` の値を `-ModelLabel` / `-Profile` へ
    同期する処理を追加（既存の `LocalLfmDlpProxy→PromptGate` 移行と同様の方式）。単一行の該当2キーのみ
    正規表現で書換え、他設定は保持。コメント行（先頭 `#`）や `service.name` は非対象。冪等。
  - ヘッダコメントに「再実行で最新化される」旨を追記。
- サイドカータスクは従来どおり `-Model` で再登録されるため、既定更新によりGGUFも新へ。

## Results

- 検証（PowerShell）:
  - `install.ps1` 構文パース OK（`Parser::ParseFile` エラーなし）。
  - 同期regex単体: 旧 config（`reason_decision`/`LFM2.5-1.2B`）→ 新値へ書換え、コメント
    `# system_prompt_file` と `service.name` は不変、既に新の場合は無変更（冪等）を確認。
- Go 非変更。`go build ./...` / `go test ./...` 緑（参考）。
- gofmt は本変更に Go ファイル無し。

## 運用メモ

- 再配置手順（管理者）: `cd proxy-server; .\install.ps1`（既定で新モデル/プロファイルに同期）→
  `Start-Service PromptGate` → `proxyctl.ps1 status` で sidecar /health 200 を待つ（初回はGGUF DL）。
- 別モデルにしたい場合は `-Model <gguf> -ModelLabel <label> -Profile <name>` を渡す。

## Refs
- https://huggingface.co/akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF
- https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft
- docs/records/20260607/1320-use-upstream-gguf.md
- docs/records/20260607/1224-switch-conf-extract-model.md
