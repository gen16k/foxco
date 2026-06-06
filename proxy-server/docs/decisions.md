# Decision Log

## AMD APU 推論バックエンド = llama.cpp Vulkan(iGPU)、CPU フォールバック (20260606 19:31)

### Status
Accepted

### Context

最終実行環境を Intel+NVIDIA 開発機から **AMD Ryzen AI シリーズ APU**（RDNA 3.5 内蔵
Radeon iGPU + XDNA2 NPU。実機例: 開発機 Ryzen AI MAX+ 395 / Radeon 8060S、デプロイ先
Ryzen 5 350）へ移行する。特定SKUに依存しない構成とする。NVIDIA 実行環境は不要。リポジトリ
本体（Go）に CUDA/NVIDIA 依存はなく、LFM 推論は外部 `llama-server`（llama.cpp）へ HTTP
委譲する設計のため、移行の実体は「サイドカーをどのバックエンドで起動するか」と、起動
スクリプト・設定・ドキュメントの AMD 化に集約される。調査詳細は
docs/knowledges/20260606/1931-amd-igpu-vulkan-rocm.md。

### Decision

- 推論サイドカーは **llama.cpp の Vulkan ビルドで内蔵 Radeon iGPU にオフロード**
  （`-ngl 99`）を既定とし、**CPU（`-ngl 0`）をフォールバック**とする。
- **ROCm は採用しない**（Windows の AMD iGPU は ROCm 非対応）。
- **NPU(XDNA2) は今回スコープ外**。ONNX + Vitis AI EP + 新規 Go バックエンドが必要で、
  spec の Milestone 6 として将来対応（ドキュメント記載のみ）。
- `start.ps1` を拡張し、サイドカー自動起動→`/health` 待ち→proxy 起動までを 1 コマンド化
  （`-Backend vulkan|cpu`、`-Classifier keyword`/`-NoSidecar`/既存稼働時はサイドカー不起動）。
- Go コード・`go.mod`・設定スキーマは無改修（バックエンド差はサイドカー起動オプションで吸収）。

### Consequences

- AMD APU 単体で GPU 加速付きのローカル LFM 推論が動く。NVIDIA 依存は完全に除去。
- Vulkan の初回呼び出しはシェーダコンパイル分のコストがあるため `classify_timeout_ms: 8000`
  を据え置く（暖機後は短縮可）。
- Vulkan ビルドの `llama-server` と AMD グラフィックスドライバ（Vulkan ランタイム）が前提に
  なる。Vulkan版 `llama-server` は **`winget install ggml.llamacpp`**（公式 winget パッケージが
  Windows Vulkan ビルドを導入）で入る。Vulkan ランタイムは AMD ドライバ同梱。未導入時は
  `-Backend cpu` または `-Classifier keyword` で動作可能。
- NPU を使わないため XDNA2 の 50 TOPS は当面活用しないが、1.2B Q4 では iGPU/CPU で十分。

### Related Records
- docs/records/20260606/1931-amd-vulkan-migration.md
