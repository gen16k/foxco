# AMD Ryzen AI NPU(XDNA2) 推論: Lemonade Server / OGA の要点 (20260606 22:33)

## Issue

Vulkan(iGPU) で動いている LFM 推論を AMD NPU(XDNA2) に載せたい。何が NPU を駆動でき、
どのモデル形式・サーバ・前提が要るのかを調べた（2025-2026 時点）。

## 訂正 (20260606 後刻) — Lemonade では LFM2 を NPU 実行できない

当初この note と plan は「**LFM2 を Lemonade Server (OGA) の NPU で動かす**」前提だったが、これは**誤り**。
再調査で確定した事実（出典は末尾 Refs）:

- **LFM2 自体は NPU 公式対応**（AMD が `amd/LFM2-1.2B-ONNX_rai_1.7.1` 等を配布）。問題はモデルではなく
  **Lemonade の実行エンジン(OGA)** の方。
- Lemonade の NPU レシピ `ryzenai-llm` は **onnxruntime-genai (OGA)** 上に構築されており、ロードには
  **`genai_config.json` が必須**。Lemonade v10.6 のレジストリの NPU モデル 79 個（Llama/Qwen/Phi/Gemma/
  Mistral/DeepSeek…）はすべて OGA 形式。**LFM2 は1つも無い**。
- AMD の LFM2 NPU モデルは **OGA 形式ではない**。中身は独自の **token-fusion ONNX グラフ**
  (`lfm2-1.2B-token-fusion.onnx` + `.data` 1.77GB) と専用スクリプト `Run-LFM2.py` / `ryzenai_ep_utils.py`。
  `genai_config.json` は無く、`onnxruntime.InferenceSession` + Ryzen AI EP(`onnxruntime_providers_ryzenai.dll`)
  を**直接**叩く。AMD Ryzen AI 公式ドキュメントも「**LFM2 は OGA フローの対象外**、モデルカード参照」と明記。
  理由は LFM2 のハイブリッド構造(10×短距離conv + 6×GQA)を OGA model_builder が表現できないため。
- **したがって LFM2 を NPU で動かす正規ルートは Lemonade ではなく、AMD Ryzen AI Software 1.7.1 の
  ネイティブランタイム + `Run-LFM2.py` のみ。** Lemonade(OGA) は Llama/Qwen/Phi 等の OGA モデル専用で、
  本 proxy が必要とする LFM2 には使えない。
- `Run-LFM2.py` は **CLI スクリプトで OpenAI 互換 HTTP サーバではない**。本 proxy（HTTP サイドカー前提）に
  載せるには **薄い HTTP シムの自作**が必要。速度ベンチだけなら Run-LFM2.py 単体で測定可能。
- NPU ドライバ: 文書要件 `32.0.203.280`(2025/5/16)。開発機の実機は `32.0.20101.3760`（branch が `203` 系
  でなく `20101` 系のため新旧の単純比較不可）。quicktest がドライバ不一致を出したら AMD 指定版へ更新。

以下「Learnings」の **Lemonade を LFM2 の統合点とする記述は上記訂正で置き換え**。Lemonade 固有の
`/api/v1` パス・health 等の情報は OGA モデル(将来 Llama 等を NPU で使う場合)には依然有効なので残す。

## Learnings

### 何が NPU を駆動できるか
- **llama.cpp / Ollama は NPU 非対応**。バックエンドは Vulkan(iGPU) / ROCm / CPU のみで、GGUF を
  XDNA NPU では動かせない（コミュニティの XDNA2 フォークはあるが非公式で matmul のみ）。
- NPU の実体は **ONNX = onnxruntime-genai (OGA) + VitisAI Execution Provider**。`onnxruntime-genai`
  は「ライブラリ」でありサーバではない（AMD のモデルカードは単体スクリプト `Run-LFM2.py` を案内）。
- **Lemonade Server**（lemonade-sdk）が OGA をラップし、**OpenAI 互換 HTTP** を提供する。これが
  「サイドカーへ HTTP 委譲」する本 proxy にそのまま載る統合点。

### Lemonade Server の要点（本 proxy への接続）
- 既定ポート **8000**。エンドポイントは **`/api/v1` プレフィックス**:
  - chat: `POST /api/v1/chat/completions`
  - health: `GET /api/v1/health`
  - （他に `/api/v1/completions`, `/api/v1/responses`）
- llama.cpp は `/v1/chat/completions` と `/health` なので、proxy 側で **パスを設定可能化**して吸収
  （`internal/inference/client.go` の `SetPaths`、`config.Inference.ChatPath/HealthPath`）。
  ※ config だけで `endpoint=".../api"` とするやり方は health が `/api/health` になり破綻するので不可。
- モデルはリクエストの `model` フィールドで選択（事前に pull/登録が必要）。`lemonade-server list` で
  ローカルの正式 id を確認、`lemonade-server pull <id>` で取得、`lemonade-server serve --port 8000` で起動。
- **注意**: `/api/v1/health` はモデル未ロードでも 200 を返し得る。よって start.ps1 の NPU 受理判定は
  health に加えて **1トークンの chat 補完が通るか**まで確認する（`Test-NpuReady`）。

### モデル形式・配布
- NPU は ONNX(OGA) + 量子化（LLM は INT4/INT8、~2bit 重みのものも）。GGUF はそのまま不可。
- **LFM2 は AMD NPU 公式対応**。AMD が ONNX プリビルドを配布:
  - `amd/LFM2-1.2B-ONNX_rai_1.7.1`（分類用途の既定。instruct 系・低レイテンシ）
  - `amd/LFM2.5-1.2B-Thinking-ONNX_rai_1.7.1`（思考トークンを出す→分類には不向き）
  - `amd/LFM2-2.6B-ONNX_rai_1.7.1`
- **日本語専用 fine-tune の NPU ビルドは無い**（本 proxy の既定は stock `LFM2.5-1.2B-Instruct`）。
  JP fine-tune を NPU 化するには Quark 量子化 + VitisAI コンパイル + 精度再検証が必要（将来課題）。

### 出力制約（重要）
- OGA は llama.cpp の **GBNF JSON schema 制約を強制しない**。サーバ側 `response_format` に依存すると
  全件 fail-close しかねない。NPU では **`reason_decision_prompt`（schema 無し）** を使い、prompt +
  寛容パース（JSON→bare ALLOW/BLOCK→不明はエラーで fail closed）で判定する。

### 前提パッケージ（Windows 11）— LFM2 を NPU で動かす場合（訂正後）
1. AMD NPU ドライバ（XDNA2）`32.0.203.280`(2025/5/16)+。開発機の実機は `32.0.20101.3760`（別 branch）。
   quicktest が不一致を出したら AMD 指定版へ更新。
2. **Miniforge (conda)** — winget `CondaForge.Miniforge3` で可（`C:\ProgramData\miniforge3` に導入実績）。Ryzen AI の前提。
3. **AMD Ryzen AI Software 1.7.1**（`ryzen-ai-lt-1.7.1.exe`、winget 非対応・AMD アカウントポータル経由）→ conda env
   `ryzen-ai-1.7.1`、`onnxruntime-genai-ryzenai` + Ryzen AI EP。**これが LFM2-NPU の実行系**。
4. **git-lfs** + `amd/LFM2-1.2B-ONNX_rai_1.7.1`（ONNX 1.77GB）。`Run-LFM2.py -m <repo>` で実行。
5. （任意）VS2022 C++ + AMD Quark — カスタム op / 自前 OGA 変換時のみ。今回不要。
- **Lemonade Server は LFM2-NPU には不要**（OGA 専用。将来 Llama/Qwen 等を NPU で使う場合のみ）。

### HW / 既知の注意
- 対象: Ryzen AI 300/400 系（Strix Point / Krackan Point / Strix Halo, XDNA2）。開発機 Ryzen AI MAX+ 395、
  デプロイ先 Ryzen 5 350 はいずれも対応。
- Strix Halo の Linux で IOMMU を無効化すると NPU が無効になる事例あり（Windows では非該当だが留意）。
- NPU は decode より **prefill(TTFT) と電力効率**で有利。warm の純トークン生成は iGPU と同等〜やや下のことも
  あるため、デフォルト切替前にベンチで確認する（records 参照）。

## Refs
- https://ryzenai.docs.amd.com/en/latest/llm/overview.html
- https://ryzenai.docs.amd.com/en/latest/llm/server_interface.html
- https://huggingface.co/collections/amd/ryzen-ai-171-npu-lfm2-models
- https://lemonade-server.ai/docs/server/ (model list / serve)
- https://github.com/lemonade-sdk/lemonade
- docs/records/20260606/2233-npu-backend.md
- docs/knowledges/20260606/1931-amd-igpu-vulkan-rocm.md
