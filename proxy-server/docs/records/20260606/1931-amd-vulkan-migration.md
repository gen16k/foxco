# AMD Ryzen AI APU 環境への移行（Vulkan iGPU 化）(20260606 19:31) #bf96b8f

## Motivation

開発環境を Intel+NVIDIA 機から、最終実行環境である **AMD Ryzen AI シリーズ APU**（RDNA 3.5
内蔵 Radeon iGPU + XDNA2 NPU。実機例: 開発機 Ryzen AI MAX+ 395 / Radeon 8060S、デプロイ先
Ryzen 5 350）へ移行する。特定SKUに依存しない構成とする。NVIDIA 実行環境は不要。リポジトリ
本体（Go）に CUDA/NVIDIA 依存はなく、LFM 推論は外部 `llama-server`（llama.cpp）へ HTTP 委譲
する設計のため、移行は「サイドカーのバックエンド選定」と起動スクリプト・設定・ドキュメントの
AMD 化に帰着する。

## Goal

- 内蔵 Radeon iGPU で GPU 加速したローカル LFM 推論を、ワンコマンドで起動できるようにする。
- NVIDIA/CUDA 前提の記述を除去し、ドキュメントを AMD 構成に整合させる。
- NPU(XDNA2) は将来課題（Milestone 6）として明記し、今回はスコープ外とする。

## Records

- 調査: リポジトリに CUDA/NVIDIA 依存コードは存在せず、推論は `internal/inference/client.go`
  から `llama-server` の OpenAI 互換 `/v1/chat/completions` へ HTTP 委譲。Windows の AMD iGPU は
  ROCm 非対応 → GPU 加速は Vulkan 一択。NPU は llama.cpp 非対応。
  詳細は docs/knowledges/20260606/1931-amd-igpu-vulkan-rocm.md。
- 決定: バックエンド = llama.cpp Vulkan(iGPU, `-ngl 99`)、CPU(`-ngl 0`) フォールバック。
  docs/decisions.md 参照。
- 導入手段: Vulkan版 `llama-server` は **`winget install ggml.llamacpp`** で入る（公式 winget
  パッケージが Windows Vulkan ビルド `llama-*-bin-win-vulkan-x64.zip` を導入、依存
  `Microsoft.VCRedist.2015+.x64` 自動）。Vulkan ランタイムは AMD ドライバ同梱。
- 環境確認: 開発機は実際には **Ryzen AI MAX+ 395 / Radeon 8060S**（Strix Halo）と判明。
  ユーザ確認のうえ、ドキュメントは特定SKU非依存の「AMD Ryzen AI シリーズ APU」表記に統一。
- 実装:
  - `start.ps1` をワンコマンド統合起動に拡張（`-Backend vulkan|cpu`, `-Model`,
    `-LlamaServer`, `-LlamaHost/-LlamaPort`, `-HealthTimeoutSec`, `-NoSidecar`）。
    サイドカー起動→`/health` ポーリング→`proxy.exe` 起動→proxy 終了時に当スクリプトが
    起動したサイドカーのみ停止。`-Classifier keyword`/`-NoSidecar`/既存稼働時は不起動。
  - `config/config.example.yaml`: `classify_timeout_ms` と `inference` のコメントを
    iGPU(Vulkan)/CPU 前提に更新（値は据え置き）。
  - ドキュメント: `README.md`(proxy)、`CLAUDE.md`、`docs/spec-proxy.md`(§1.1/§3.1/§3.4/
    §8.4/§8.6/§8.7.1)、ルート `README.md` を AMD Ryzen AI APU + iGPU Vulkan + ワンコマンド
    起動に更新。NVIDIA/CUDA を非対象と明記。
- Go コード・`go.mod`・設定スキーマは無改修。

## Results

- 静的チェック（worktree, go1.26.4）: `go vet ./...` exit 0 / `go build ./...` exit 0 /
  `go test ./...` 全パッケージ ok。`gofmt -l .` は全Goファイルを列挙するが、原因は
  `core.autocrlf=true` による作業ツリーCRLF（LF正規化すると無出力、`git status -- *.go` も空）。
  既存の環境事情で本変更とは無関係（Goファイルは未変更）。再フォーマットはしない。
- `start.ps1` は PowerShell パーサで構文エラーなしを確認。
- プロキシ実機スモークテスト（`-classifier keyword`）: 127.0.0.1:8787 待受、公開AWS例示キーで
  ルールガードレールが `BLOCK`（source=rule, 9ms, upstream未呼出）→ ローカルブロック応答を返却、
  監査ログ記録を確認。DLP ブロックパスが AMD 機で正常動作。
- 実機 E2E 検証（`winget install ggml.llamacpp` = llama.cpp b9538 Windows Vulkan ビルド導入後）:
  - `llama-server --list-devices` → `Vulkan0: AMD Radeon(TM) 8060S Graphics`（iGPU）を検出。
  - Vulkan サイドカー（`-ngl 99`, port 8791）起動 → `/health` 200、model loaded・listening。
  - プロキシ（実 LFM バックエンド）起動 → ログ `LFM warm endpoint=http://127.0.0.1:8791`。
  - 良性プロンプト → **ALLOW**（LFM 非ブロック→転送試行。検証では upstream を無効アドレスに
    向け外部送信ゼロ）。
  - 機微な社内情報（正規表現の秘密形状に非該当）→ **BLOCK `source=lfm`**
    `reason="contains internal company information"`、**iGPU warm 191ms**、upstream 未呼出。
  - CPU フォールバック（`-ngl 0`）でも同入力を **BLOCK `source=lfm`**、latency 711ms。
    iGPU 191ms / CPU 711ms（約3.7倍差）で Vulkan オフロードの加速も確認。
  - 結論: AMD iGPU(Vulkan) 経由の実 LFM 分類が ALLOW/BLOCK 両方で正常動作。検証完了。

## Refs
- docs/decisions.md
- docs/knowledges/20260606/1931-amd-igpu-vulkan-rocm.md
- docs/spec-proxy.md（§1.1 / §3 / §8.4 / §8.6 / §8.7）
