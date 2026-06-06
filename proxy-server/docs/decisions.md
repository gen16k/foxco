# Decision Log

## 管理UI向けオプトイン生プロンプト保存 + 読み取り専用 admin API (20260606 21:20)

### Status
Accepted

### Context

ローカル proxy の検知件数・検知内容・全プロンプト履歴を確認する Grafana 風の管理UI
（Next.js）をユーザーが要望。現状の監査DB（`audit_events`）は **メタデータのみ**
（decision/reason/source/latency/backend/時刻）を保存し、生のプロンプト本文・機密値は
一切保存しない（CLAUDE.md「Never log or persist raw content」不変条件、`store_raw_text:false`）。
「プロンプト履歴」「検知内容」を表示するには生データ保存が必須で、これは不変条件を緩める。
ユーザーに明示的に確認し、(1) プロンプト全文を保存、(2) Go に読み取り専用 admin API を追加、
(3) 秘密情報を保存するため UI に ID/PW 認証＋ admin API に任意の Bearer トークン、で合意。

### Decision

- `storage.store_raw_text`（既存・既定 false）を実際に配線。**true のときのみ**、各リクエストの
  **ライブターン（最新のユーザーターン）本文**を `audit_events.prompt_text` に保存する
  （ALLOW/BLOCK 双方）。Claude Code は毎回全履歴を再送するため、配列全体ではなく新規ターンのみ
  を保存して重複を回避。本文は ~16KiB で rune 安全に切り詰め。`request_unparseable` 等の
  解析不能リクエストは本文を保存しない。
- 監査スキーマに nullable 列 `prompt_text`/`matched_snippet`/`path` を追加。既存DBには
  `PRAGMA table_info` ベースの冪等マイグレーション（`ALTER TABLE ADD COLUMN`）で追加。
- 読み取り専用 `internal/admin`（`GET /admin/stats|events|events/{id}|meta`）を追加。proxy と
  同一 mux・同一 localhost バインド。`admin.enabled`（既定 true）で切替、`admin.auth_token`
  が非空なら `Authorization: Bearer` を必須化（store_raw_text=true 時は設定を強く推奨）。
- `matched_snippet` は当面未使用（`dlp.Evaluation` が該当セグメントを単独露出しないため、
  正確な機密スパン抽出は先送り。docs/todo.md 参照）。

### Consequences

- `store_raw_text:true` の監査DBは**秘密情報を含む**。保護は retention（既定30日）・localhost
  バインド・OSファイル権限・admin トークンのみ＝**advisory であり強制境界ではない**。本番は
  既定 false を維持。デモ時のみ true + auth_token を設定する運用。
- 既定（false）の動作は不変＝メタデータのみ。後方互換のマイグレーションで旧DBもそのまま読める。
- admin API は読み取り専用で上流送信を一切行わない（egress 経路を増やさない）。

### Related Records
- docs/records/20260606/2117-admin-observability-and-ui.md

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
