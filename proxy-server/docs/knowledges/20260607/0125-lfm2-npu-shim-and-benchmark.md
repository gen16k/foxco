# LFM2 を NPU で動かす自作シムと NPU/GPU/CPU ベンチ (20260607 01:25)

## Issue

LFM2 を AMD NPU(XDNA2) で実行し、proxy のサイドカーとして使えるようにする。さらに NPU を
デフォルトにすべきか、3 経路（NPU/GPU(Vulkan)/CPU）の実測で判断する。先行 note
`20260606/2233-amd-npu-lemonade-oga.md` で「Lemonade では LFM2 を NPU 実行できない」ことは確定済み。
本 note はその先 —「では実際にどう動かし、速さはどうか」を実機で確定した記録。

## Learnings

### LFM2-on-NPU の実行系（実機で確立）
- 実行環境: conda env **`ryzen-ai-1.7.1`** + Ryzen AI Software 1.7.1。EP は
  `RyzenAILightExecutionProvider`（`C:\Program Files\RyzenAI\1.7.1\deployment\onnxruntime_providers_ryzenai.dll`）。
- モデル: `amd/LFM2-1.2B-ONNX_rai_1.7.1`（git-lfs, ~2.5GB）。実体は **token-fusion ONNX グラフ**
  (`lfm2-1.2B-token-fusion.onnx` + `.onnx.data` + `.fconst` + seq 長別の事前コンパイル済み DPU 制御パケット
  `dd_metastate_*.ctrlpkt`)。`genai_config.json` は無い（＝OGA ではない）。
- 推論は AMD の `Run-LFM2.py` / `ryzenai_ep_utils.py` 方式: `onnxruntime.InferenceSession` を直接叩き、
  **io_binding** で入出力をバインド、KV cache（full_attention 層）と conv cache（conv 層）を毎ステップ回す。
  `ryzenai_ep_utils.py` は **モデルディレクトリ同梱**で、import 時に EP デプロイディレクトリへ `chdir` する
  → シム側は `sys.path.insert(0, model_dir)` してから import する必要がある。
- LFM2 のハイブリッド構造: `config.layer_types` が層ごとに `full_attention` / `conv`。KV は
  `[bsz, num_kv_heads, MAXSEQ, head_dim]`、conv は `rai.conv_shape`。KV バッファは **512B アラインメント**が要る
  （`Run-LFM2.py` と同じ）。語彙 logits は 65536 幅。greedy は `logits[:, -1].argmax(-1)`。

### 自作 OpenAI 互換シム（`proxy-server/npu/npu_server.py`）
- `Run-LFM2.py` の prefill/decode ループをそのまま HTTP 化。**Python 標準ライブラリのみ**
  (`http.server.ThreadingHTTPServer`) で実装し、Ryzen AI env に pip 依存を足さない（ピン崩し回避）。
- `POST /v1/chat/completions`（OpenAI 互換）+ `GET /health`。llama.cpp と**同じパス**を出すので proxy 側の
  パス上書きは不要（Lemonade の `/api/v1` 問題が消える）。
- `messages` を `tokenizer.apply_chat_template(...)` で整形（system 非対応テンプレ用に system→user 折込の fallback あり）。
  `temperature=0` greedy、`max_tokens` で打切り、eos で停止。**`response_format` は受理して無視**
  （NPU は grammar 制約不可）→ proxy は `reason_decision_prompt`（schema 無し）+ 寛容パースで判定。
- 単一 NPU セッションのため生成を **ロックで直列化**。セキュリティ: 127.0.0.1 のみ bind、
  **生のプロンプト/出力/ボディを一切ログに出さない**（メタデータのみ）。
- 既知挙動: シム/Run-LFM2 終了時に Ryzen AI EP の shutdown で**非ゼロ終了コード**が出ることがあるが、
  health/補完が通っていれば結果は有効（start.ps1 は health+1トークン補完で受理判定）。

### ベンチ結果（開発機 Strix Halo, LFM2-1.2B, 485-tok 分類プロンプト, 20 iters, prompt-cache 無効）
| Backend | Prefill tok/s | Decode tok/s | decode p50/p95/p99 ms | e2e prompt+24gen ms |
|---|---|---|---|---|
| GPU (Vulkan 8060S) | **5227** | **199.8** | 5.09 / 6.01 / 7.12 | **213** |
| NPU (XDNA2) | 2208 | 50.2 | 18.74 / 20.62 / 50.95 | 698 |
| CPU (16 thr) | 1520 | 62.5 | 16.09 / 18.41 / 20.99 | 703 |
- 3 経路とも正しい分類（`{"decision":"ALLOW",...}`）。開発機では **GPU が圧勝**（iGPU 8060S=40CU が極端に強い）。
- NPU は prefill 2 位だが decode 最遅（50 tok/s, NPU 側処理が律速で帯域は余裕）。電力効率と
  「iGPU/CPU を前景作業に空ける」点が NPU の実利。
- 生データ/ハーネス: `C:\Users\gen16k\ryzenai-lfm2\bench\`（`*_full.json`, `RESULTS.md`, `npu_bench.py`, `llama_bench.py`）。

### 本番機 Ryzen AI 5 340（Krackan Point）への外挿 ※実測ではない・要実機確認
- **NPU は全機種同一**（XDNA2 50 TOPS）→ NPU 値（prefill ~2200 / decode ~50）はほぼ転用可。
- iGPU が激減: **Radeon 840M = 4 CU**（~1.5 TFLOPS, 128-bit メモリ）で 8060S（40 CU, 256-bit）の約 1/10。
  prefill は演算律速 → GPU ~520 tok/s（NPU の 1/4 以下）。decode は帯域律速 → GPU ~90–120 tok/s。
- 分類は「長い入力＋短い出力」で **prefill 支配的** → 推定 e2e: NPU ~720ms < GPU ~1150ms < CPU ~1550ms。
  **本番機では NPU が最速の見込み** → デフォルト NPU 優先は本番ターゲットに対して妥当。
- 開発機では GPU が速いので、開発高速化が要る時は `-Backend vulkan`。340 実機が手に入れば同ハーネスで再計測し更新。

## Refs
- docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md（Lemonade では LFM2-NPU 不可）
- docs/records/20260607/0125-npu-shim-and-benchmark.md
- https://ryzenai.docs.amd.com/en/latest/llm/overview.html
- https://huggingface.co/collections/amd/ryzen-ai-171-npu-lfm2-models
- https://www.notebookcheck.net/AMD-Ryzen-AI-5-340-Processor-Benchmarks-and-Specs.950403.0.html
- https://www.notebookcheck.net/AMD-Radeon-840M-Benchmarks-and-Specs.950405.0.html
