# Decision Log

## 既定モデル取得を公式GGUFの -hf 直DLへ切替（ローカル変換はフォールバックに） (20260607 13:20)

### Status
Accepted

### Context

直前の決定（下記 12:24）ではモデルが safetensors のみだったため `scripts/convert-model-gguf.ps1` で
ローカル GGUF 変換する運用にしていた。その後、上流に公式 GGUF
`akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF`（Q4_K_M / Q8_0 / F16 / BF16、単一ファイル、chat
template は GGUF メタデータ埋め込み）が公開された。

### Decision

- `start.ps1` の既定 `-Model` を `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M`（`-hf` 自動DL）に変更。
- ローカル変換スクリプトは削除せず、**GGUF 未公開のチェックポイント（例: 350M）向けフォールバック**として存置。
- 12:24 決定のうち「GGUF はローカル変換運用」を本決定で更新（差し替え1ノブ・抽出契約既定・セキュリティ
  トレードオフ等は不変）。

### Consequences

- セットアップが簡素化（Python/変換不要、llama.cpp のみで初回 `-hf` DL）。`--jinja` が GGUF 埋め込みの
  chat template を使用。
- 実機検証：`-hf` で Q4_K_M を取得し llama-server にロード、日本語サンプルで 11キー抽出JSON→BLOCK/ALLOW を確認。

### Related Records
- docs/records/20260607/1320-use-upstream-gguf.md

## 既定DLPモデルを akiFQC Conf-Extract 日本語ファミリへ切替（GGUFローカル変換 + 抽出契約を既定化） (20260607 12:24)

### Status
Accepted

### Context

既定モデルを汎用2値分類器 `LFM2.5-1.2B-Instruct`（プロファイル `reason_decision`）から、社外秘抽出に
特化した **akiFQC Conf-Extract 日本語ファミリ**へ切り替える（ユーザー指定）。既定チェックポイントは
`akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract`（1.2B）、軽量版は `akiFQC/LFM2-350M-Conf-Extract-Japanese`。
将来も同系統の別サイズ/版へ容易に差し替えたい要件がある。これらは **safetensors のみで GGUF 未提供**だが、
推論は llama.cpp（GGUF 必須）。抽出契約自体は別ブランチの commit `d3c8ad1` で実装済みの
`jp_confidential_extraction` プロファイルを移植して用いる。

### Decision

- **既定プロファイルを `jp_confidential_extraction` に変更**（`config` 既定 + `config.example.yaml`）。
  11カテゴリ抽出の非空→BLOCK。`reason_decision`/`ng_boolean` は選択肢として存続。
- **GGUF はローカル変換運用**。補助スクリプト `scripts/convert-model-gguf.ps1`（`-Repo` でファミリ指定、
  HF snapshot→`convert_hf_to_gguf.py` f16→`llama-quantize`）を追加。`start.ps1 -Model` はローカル `.gguf` を
  指す（既定）。将来 `*-GGUF` リポジトリ公開後は `-Model <repo>-GGUF:<quant>`（`-hf`）で直 DL に切替可。
- **差し替えは1ノブ**：同系統は同 I/O 契約のため、サイズ変更は `start.ps1 -Model`（と監査ラベル
  `inference.model`）のみ。コード/`inference.profile` は不変。
- **`<<<DATA>>>` 不活性データラッパの省略を既定化**。`d3c8ad1` では opt-in だったが、抽出モデルを既定に
  するため本省略も既定の挙動になる（学習分布一致のため。CLAUDE.md 不変条件「検査テキストは不活性データ」を
  本プロファイル限定で弱める。ユーザー承認済み）。

### Consequences

- 利点：日本語社外秘の抽出精度を学習分布通りに引き出せる。系統内サイズ変更がコード変更不要。
- リスク（カバレッジ変化）：11カテゴリ外・英語前提の秘密（汎用 API キー/パスワード等）は抽出モデルが
  拾わない可能性。→ **`dlp.rule_guardrail.enabled: true` を維持**して決定論層で担保することを前提とする。
- リスク（注入耐性低下）：ラッパ省略で命令注入に弱くなるが、抽出器に反転し得る判定フィールドが無く最悪でも
  FN。rule guardrail + fail-closed が後段で補う。
- プロンプト厳密性：モデルカードがスタブで実学習プロンプト非公開。`-sft` 系統由来の byte-exact プロンプトを
  流用し、ずれる場合は `inference.system_prompt_file` で固定（`docs/todo.md` 参照）。

### Related Records
- docs/records/20260607/1224-switch-conf-extract-model.md

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
