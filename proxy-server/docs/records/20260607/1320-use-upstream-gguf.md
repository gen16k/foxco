# 公式GGUFの -hf 直DLへ切替 (20260607 13:20) #a9d8375

## Motivation

直前の切替（docs/records/20260607/1224-switch-conf-extract-model.md）では対象モデルが safetensors のみ
だったためローカル GGUF 変換運用にしていた。その後、上流に公式 GGUF
`akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF` が公開されたため、これを使うよう切り替える（ユーザー指示）。

## Goal

- 既定のモデル取得を `-hf` 直DLにし、ローカル変換は GGUF 未公開チェックポイント向けフォールバックに。

## Records

- 公式 GGUF リポジトリのファイル: `*-Q4_K_M.gguf` / `*-Q8_0.gguf` / `*-F16.gguf` / `*-BF16.gguf`（単一
  ファイル、shard なし、chat template は GGUF メタデータ埋め込み）。
- `start.ps1`: 既定 `-Model` を `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M` に変更。ヘッダの
  モデル早見表も `-hf` 既定 + 変換フォールバックに更新。既存の「ローカルパス=-m / それ以外=-hf」分岐は不変。
- `README.md`: DLP モデル節を `-hf` 既定に書き換え、変換は GGUF 未公開時の手段として記載。Launch 例も更新。
- `CLAUDE.md`: サイドカー手動起動例の `-hf` ref を新 GGUF に更新。
- `docs/decisions.md`: 追補（13:20）。`docs/todo.md`: 「-hf 直DL へ切替」を Resolved。

## Results

- 実機検証（CPU, -ngl 0）: `llama-server -hf akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M` で
  初回DL→ロード成功。byte-exact 日本語システムプロンプトで:
  - 機密サンプル → `human_name`/`phone_number`/`network_identifier` を抽出（非空→BLOCK）。
  - 良性サンプル → 全11キー空（→ALLOW）。
- `go build` / `go test ./...` は本セグメントでコード非変更のため前セグメントの結果が有効（再実行で確認）。

## Refs
- https://huggingface.co/akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF
- docs/records/20260607/1224-switch-conf-extract-model.md
- docs/decisions.md（20260607 13:20）
