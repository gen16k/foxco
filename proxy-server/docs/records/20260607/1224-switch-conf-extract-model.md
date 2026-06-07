# 既定DLPモデルを akiFQC Conf-Extract 日本語ファミリへ切替 (20260607 12:24) #bc0e514

## Motivation

ユーザー要望により、既定 DLP モデルを汎用2値分類器 `LFM2.5-1.2B-Instruct`（`reason_decision`）から、
社外秘抽出に特化した **akiFQC Conf-Extract 日本語ファミリ**へ切り替える。将来は同系統の別サイズ/版へ
容易に差し替えたい（350M↔1.2B↔将来版）。既定チェックポイントは `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract`。

## Goal

- 既定プロファイルを抽出契約 `jp_confidential_extraction` にする。
- 対象が safetensors のみ（GGUF 未提供）なので、ローカル GGUF 変換スクリプトを用意。
- ファミリ差し替えを「1ノブ」化（`start.ps1 -Model` のみ。プロファイル/コードは不変）。
- セキュリティ・カバレッジ上のトレードオフを文書化。

## Records

- 作業ブランチ: `feat/switch-conf-extract-model`（main 起点の worktree）。既存
  `feat/jp-confidential-extraction` には抽出プロファイルがあるが MITM/管理UI 等を巻き込むため、
  そのプロファイル実装 commit `d3c8ad1` のみを移植する方針（ユーザー確認済み）。
- 移植: `git cherry-pick -n d3c8ad1` でコード3点（`profile.go` の `MaxTokens` 追加 +
  `jpConfidentialExtractionProfile`、`client.go` の profile-aware `max_tokens`、
  `profile_jp_extraction_test.go`）を取り込み。流入したドキュメント類は本タスク向けに書き直し
  （他ブランチ由来の作業記録 `0138-...` は削除、`decisions.md` は main 版へ戻して新規エントリ追記、
  `todo.md` は関連項目のみで新規作成）。
- 既定切替: `internal/config/config.go` の `Default()` と `config/config.example.yaml` を
  `profile: jp_confidential_extraction` / `model: LFM2.5-1.2B-JP-202606-Conf-Extract`（ラベル）に変更。
- GGUF 変換: `scripts/convert-model-gguf.ps1` を新規作成（`-Repo`/`-Quant`/`-OutDir`/`-CacheDir`/
  `-LlamaCppDir`/`-HfToken` パラメータ。HF snapshot → `convert_hf_to_gguf.py` f16 → `llama-quantize`）。
  `start.ps1` の既定 `-Model` をローカル変換 GGUF パスへ。未変換 `.gguf` 指定時は変換スクリプトを案内する
  分岐を追加。ヘッダにモデルファミリ早見表を追記。`.gitignore` 新設（`/models/`・`/.cache/`・`/proxy.exe`）。
- ドキュメント: `README.md`（既定契約・変換/差し替え手順）、`docs/spec-proxy.md` §1.1 追補
  （2026-06-07）、`docs/decisions.md`・`docs/todo.md` を更新。

## Results

- `go build ./...` OK / `go test ./...`（後述CIで確認）。移植した jp/max_tokens テスト全12件パス。
- 変換スクリプトと `start.ps1` は PowerShell パーサで構文検証済み。`llama-quantize`/`python`/
  `huggingface-cli` の存在は確認済み（実変換はモデルDL+torch導入を伴う重処理のため、実機検証は別途）。
- 既知のトレードオフ（要運用前提）:
  - `jp_confidential_extraction` は `<<<DATA>>>` 不活性データラッパを外す（学習分布一致）。注入耐性は
    下がるが抽出器に反転し得る判定フィールドが無く最悪でも FN。rule guardrail + fail-closed が後段で担保。
  - 11カテゴリ外・英語前提の秘密は抽出モデルが拾わない可能性 → `dlp.rule_guardrail.enabled: true` 維持前提。
  - 実学習プロンプトが非公開のため `-sft` 系統由来 byte-exact を流用。ズレ時は `system_prompt_file` で固定。

## Refs
- docs/decisions.md（20260607 12:24 エントリ）
- docs/todo.md
- 移植元: commit d3c8ad1（feat/jp-confidential-extraction）
- https://huggingface.co/akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract
- https://huggingface.co/akiFQC/LFM2-350M-Conf-Extract-Japanese
