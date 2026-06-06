# Decision Log

## AMD NPU(XDNA2) 対応: 自作 Ryzen AI ONNX シム経由、NPU 優先 + 自動フォールバック (20260607 01:25)

### Status
Accepted

### Context

直前の決定（22:33）は「Lemonade Server(OGA) で LFM2 を NPU 実行」前提だったが、再調査と実機検証で
**Lemonade(OGA) は LFM2 を実行できない**ことが確定（`genai_config.json` 必須＝OGA 専用、AMD 公式も
「LFM2 は OGA フロー対象外」と明記）。LFM2-on-NPU の唯一の正規ルートは **AMD Ryzen AI Software 1.7.1
ネイティブ + `Run-LFM2.py`**（token-fusion ONNX + `RyzenAILightExecutionProvider`）で、これは CLI のため
OpenAI 互換 HTTP サーバが無い。実機で LFM2-on-NPU を動作させ、NPU/GPU/CPU を実測した
（docs/records/20260607/0125-npu-shim-and-benchmark.md, docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md）。

### Decision

- NPU ランタイムは **自作の薄い OpenAI 互換シム `npu/npu_server.py`**（`Run-LFM2.py` の prefill/decode ループを
  Python 標準ライブラリ HTTP で包む）。`POST /v1/chat/completions` + `GET /health` を **llama.cpp と同じパス**で出すため、
  proxy 側はパス上書き不要・ランタイム非依存のまま。Ryzen AI conda env で動かし、追加 pip 依存は入れない。
- モデルは AMD プリビルド **`amd/LFM2-1.2B-ONNX_rai_1.7.1`**（ローカル dir を指定）。`reason_decision_prompt`
  プロファイル（schema 無し）+ 寛容パースで判定（NPU は grammar 制約不可）。
- `start.ps1` 既定は **`-Backend auto`（NPU → Vulkan → CPU）** を維持。NPU は health + 1トークン補完で受理し、
  未導入/不調なら透過的に次段へ。`-Backend npu|vulkan|cpu` で単一強制（vulkan/cpu が NPU 無効化）。
- **デフォルト NPU 優先の根拠**: NPU は全機種同一（XDNA2 50 TOPS）。本番機 **Ryzen AI 340** は iGPU が
  Radeon 840M（4 CU, 開発機 8060S=40CU の約 1/10）で、分類（長い入力＋短い出力＝prefill 支配的）では
  **NPU が e2e 最速の見込み**。開発機 Strix Halo では GPU が速いので、開発時は `-Backend vulkan` も可。

### Consequences

- AMD APU 単体で LFM2 を NPU 実行できる。NPU 不可環境でも auto が Vulkan/CPU に落ちるため運用は壊れない。
- 前提が増える（AMD NPU ドライバ / Ryzen AI Software 1.7.1 + conda env `ryzen-ai-1.7.1` / ローカル LFM2 ONNX）。
  Vulkan/CPU 経路は従来どおり `winget install ggml.llamacpp` のみ。
- **Lemonade は LFM2-NPU には不要**（OGA 専用）。Go の chat_path/health_path 配線は将来 Llama 等 OGA モデルを
  NPU で使う余地として残置（LFM2-NPU では未使用＝既定パス）。
- 開発機ベンチでは GPU 最速・NPU 最遅という結果だが、これは 8060S が異常に強い iGPU だったため。本番 340 では
  逆転見込み。340 実機が入手でき次第、同ハーネスで再計測し記録を更新する。
- 日本語専用 fine-tune は NPU ビルド無し（docs/todo.md に Deferred）。

### Related Records
- docs/records/20260607/0125-npu-shim-and-benchmark.md
- docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md

## AMD NPU(XDNA2) 対応: Lemonade Server 経由、NPU 優先 + 自動フォールバック (20260606 22:33)

### Status
Superseded (20260607 01:25) — Lemonade(OGA) では LFM2 を NPU 実行できないため、自作 Ryzen AI ONNX シムに刷新。上の決定を参照。

### Context

Vulkan(iGPU) バックエンドが安定稼働したため、XDNA2 NPU での推論を試し、有効なら第一候補に
する。調査の結論: **llama.cpp / Ollama は NPU を駆動できない**（GGUF=CPU/GPU のみ）。NPU の
実体は **ONNX(onnxruntime-genai / OGA) + VitisAI EP** で、AMD が LFM2 のプリビルド NPU ONNX
モデルを配布（`amd/LFM2-1.2B-ONNX_rai_1.7.1` ほか）。**Lemonade Server** が OGA をラップして
**OpenAI 互換 HTTP**（`/api/v1/...`）を提供するため、既存の「サイドカーへ HTTP 委譲」設計に
最小変更で載る。詳細は docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md。

### Decision

- NPU ランタイムは **Lemonade Server**（OpenAI 互換, NPU/hybrid OGA）。proxy は endpoint と
  リクエストパスを切り替えるだけ（Go の配線変更は最小）。
- モデルは AMD プリビルド **`amd/LFM2-1.2B-ONNX_rai_1.7.1`**（現行 LFM2.5-1.2B-Instruct と
  同系・低レイテンシ。"Thinking" 版は思考トークンを出すため分類用途では不採用）。
- `start.ps1` の既定を **`-Backend auto`（NPU → Vulkan → CPU）** に変更。各段は health で受理
  （NPU は「モデルが実際に応答するか」まで確認）し、未導入/不調なら透過的に次段へ。
  `-Backend npu|vulkan|cpu` で単一強制（`vulkan`/`cpu` が NPU 無効化）。
- Go 側の最小変更: `LlamaClient` の chat/health パスを設定可能化（Lemonade の `/api/v1`）、
  `inference.chat_path/health_path/backend` 追加、proxy に `-endpoint/-model/-profile/-chat-path/`
  `-health-path/-backend` 上書きフラグ、監査ログの backend を **実ランタイム**に修正。
- NPU では **`reason_decision_prompt` プロファイル**（schema 無し）を使う。OGA は llama.cpp の
  GBNF JSON schema 制約を強制しないため、サーバ側 `response_format` に頼らず prompt + 寛容
  パース（既存 fail-closed）で判定する。

### Consequences

- AMD APU 単体で NPU 推論を試せる。NPU が使えない環境でも auto が Vulkan/CPU に落ちるため
  既存運用は壊れない。NPU を切りたい場合は `-Backend vulkan`。
- 前提パッケージが増える（AMD NPU ドライバ / Ryzen AI Software 1.7.x / Lemonade、+ conda）。
  Vulkan/CPU 経路は従来どおり `winget install ggml.llamacpp` のみで動く。
- 日本語専用 fine-tune は現状未使用（既定は stock Instruct）。JP fine-tune の OGA/VitisAI 変換と
  精度再検証は将来課題（docs/todo.md）。
- Lemonade の細部（`serve` サブコマンド、モデル登録名、health パス）は導入版で要確認。設計上は
  start.ps1 パラメータ + config に隔離済みで「値の変更」で吸収できる。

### Related Records
- docs/records/20260606/2233-npu-backend.md
- docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md

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
- ~~**NPU(XDNA2) は今回スコープ外**。ONNX + Vitis AI EP + 新規 Go バックエンドが必要で、
  spec の Milestone 6 として将来対応（ドキュメント記載のみ）。~~
  → **Superseded (20260606 22:33)**: NPU は Lemonade Server(OGA) 経由で対応済み。上の決定を参照。
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
