# JP fine-tune (LFM2.5-1.2B-JP) を NPU で動かすための変換 (20260607 01:34)

## Issue

本番想定モデルは日本語 fine-tune `LFM2.5-1.2B-JP-202606`。NPU バックエンドは現状 AMD プリビルドの
`amd/LFM2-1.2B-ONNX_rai_1.7.1`（stock）しか動かせない。JP fine-tune を NPU で動かすにはどんな変換が要るかを調査した。

## Learnings

### なぜ単純変換できないか — LFM2 は OGA ではなく token-fusion
- AMD の NPU LLM の標準ルートは **OGA フロー**: Quark で量子化 → `model_builder` が `genai_config.json` +
  ONNX を生成 → onnxruntime-genai で実行。Llama/Qwen/Phi 等の fine-tune はこの公開レシピで NPU 化できる
  （`quark.docs.amd.com` の uint4 OGA チュートリアル等）。
- **LFM2 はこの OGA フロー対象外**。ハイブリッド構造（短距離 conv 層 + GQA attention 層）を OGA の
  `model_builder` が表現できないため、AMD は専用の **token-fusion ONNX グラフ**
  (`lfm2-1.2B-token-fusion.onnx` + `.onnx.data` + `.fconst` + seq 長別の事前コンパイル済み DPU 制御パケット
  `dd_metastate_*.ctrlpkt`) を作り、`Run-LFM2.py` / `RyzenAILightExecutionProvider` で直接実行する。
- **AMD はこの token-fusion 変換レシピ/スクリプトを公開していない**（モデルカードは deployment-only で、
  プリビルドの配布のみ。Quark の公開ドキュメントも OGA フロー中心で token-fusion 生成は触れていない）。
  → JP fine-tune の NPU 化は「公開ツールだけでは完結しない」のが現状。

### NPU 化に必要になる処理（一般形）
fine-tune（PyTorch/HF）→ NPU 実行可能までに概念的に必要なのは:
1. **前提**: fine-tune が **LFM2.5-1.2B と同一アーキ・同一トークナイザ/語彙**であること。
   継続学習/SFT で語彙不変なら logits 幅 65536・各テンソル形状が一致 → 後段が楽。語彙追加など形状変化があると
   graph と ctrlpkt の再生成が必須で難度が大幅に上がる。
2. **量子化**: AMD **Quark** で NPU 向け量子化（LLM は INT4/uint4 のグループ量子化が標準。SmoothQuant/GPTQ/AWQ 等）。
3. **token-fusion グラフ生成**: AMD の LFM2 変換フローで conv/attention を融合した ONNX を生成（KV/conv cache の
   入出力を含む）。**ここが非公開部分**。
4. **NPU コンパイル**: Ryzen AI コンパイラで seq 長別の DPU 制御パケット（ctrlpkt）を生成。
5. **DLP 精度の再検証**: 量子化で挙動が動くため `evalbench`（合成ケース、実シークレット禁止）で FP/FN・レイテンシを再確認し、
   必要なら `reason_decision_prompt` の system プロンプトや閾値を再調整。

### 現実的な選択肢
- **選択肢A（低工数・未サポート・要検証）**: 既存プリビルド graph に **FT 重みを再量子化して差し込む**。
  ctrlpkt は**テンソル形状とオーバレイに依存し重み値には非依存**なので、形状/語彙が完全一致なら再利用できる可能性が高い。
  ただし AMD の重み配置（`.onnx.data` / `.fconst` の量子化レイアウト・スケール）が非公開のためリバースエンジニアリングが必要で、
  壊れやすく非サポート。実現可能性の検証から始める価値はある。
- **選択肢B（正攻法・要 AMD）**: AMD の token-fusion 変換ツールで FT から graph+ctrlpkt を再生成。
  AMD サポート/将来公開待ち。Quark + Ryzen AI Software 1.7.1 + おそらく VS2022 C++ ビルド環境が前提。
- **つなぎ（即運用可）**: JP fine-tune を **Vulkan/CPU 上の GGUF** で動かす。HF→GGUF は
  `convert_hf_to_gguf.py` + `llama-quantize`（Q4_K_M）で容易。`start.ps1 -Backend vulkan`（または auto で NPU 不在時に自動降格）。
  **注意**: この間 NPU は stock LFM2、GPU/CPU は JP-FT となり**バックエンド間でモデルが異なる**ため、分類挙動に差が出る。
  NPU を本番 JP 用途で使うなら A/B のどちらかが必要。

### 当面の運用方針への含意
- 既定 NPU 優先は「stock LFM2-1.2B が JP 入力でも DLP 精度が許容範囲」である限り有効。JP 精度が要件なら、
  JP-FT-on-NPU（A or B）が整うまでは **`-Backend vulkan` で JP-FT GGUF を使う**のが安全。
- どちらを既定にするかは Ryzen AI 340 実機での `evalbench`（stock-NPU vs JP-FT-GPU の FP/FN 比較）で判断する。

## NPU 化にかかる時間 と 「元モデルを NPU 対応に」の是非 (20260607 追記)

### ボトルネックは計算時間ではなく「非公開ツール」
LFM2-FT の NPU 化が困難なのは計算量ではなく、**token-fusion 変換レシピが非公開で AMD 依存**だから。
よって「何時間」と固定見積もりできる作業ではなく、ツール入手/サポート次第（数日〜入手不可）。

### 仮に変換ツールがあった場合の計算内訳（外挿・one-time・推論時コストゼロ。学習自体とは別）
- Quark 量子化(INT4): 1.2B で **数分〜1時間**（GPU、アルゴリズム/校正データ次第）。
- token-fusion グラフ生成: 数分。
- **NPU コンパイル（seq 長別 ctrlpkt 生成）: 数十分〜数時間（律速・変動大）**。
- DLP 精度再検証(evalbench): 数分。
- 合計 **one-time 1〜4 時間規模**。※AMD の LFM2 変換実体での実測ではない。

### 「fine-tune の元モデルを NPU 対応にすれば現状でいけるか」— 2 つの読み
- (a) **AMD の LFM2-NPU ファイルを土台に FT** → **不可**。量子化＋コンパイル済みで学習できない。
  学習は FP16 チェックポイントで行い、結果をまた token-fusion 変換する必要があり、同じ非公開の壁に戻る。
  アーキが NPU 対応でも壁は「重みの変換」側のため回避にならない。
- (b) **今の公開ツールで NPU 変換できる標準アーキを土台に JP-DLP を FT** → **現状でも可能**。
  Qwen2.5-1.5B / Llama-3.2-1B / Phi-3.5-mini / Gemma 等は **公開の Quark→OGA(model_builder)→NPU** で
  NPU 化でき、Lemonade でサーブも可。**ただし LFM2 ではなくなる**（LFM2 の効率・既存 JP-LFM2 資産は捨てる）。

### 推奨分岐
- **NPU 上の JP-DLP を早く欲しく、モデル系列が自由** → 標準 NPU 対応アーキ（例 Qwen2.5-1.5B）を DLP 用に FT し、
  公開ルートで NPU 化。これが「現状でなんとかなる」道。
- **LFM2 必須** → 当面 JP-FT を Vulkan/CPU(GGUF) で運用、NPU は stock のまま。token-fusion 変換は AMD と別トラックで。

## Refs
- https://www.amd.com/en/developer/resources/technical-articles/accelerate-llms-locally-on-amd-ryzen-ai-npu-and-igpu.html
- https://www.amd.com/en/developer/resources/technical-articles/2025/ai-inference-acceleration-on-ryzen-ai-with-quark.html
- https://quark.docs.amd.com/latest/supported_accelerators/ryzenai/tutorial_uint4_oga.html
- https://ryzenai.docs.amd.com/en/latest/model_quantization.html
- https://huggingface.co/collections/amd/ryzen-ai-171-npu-lfm2-models
- docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md
- docs/todo.md（JP fine-tune を NPU 化して精度再検証）
