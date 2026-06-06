# AMD Ryzen APU での LFM 推論バックエンド選定（Vulkan/ROCm/NPU）(20260606 19:31)

## Issue

最終実行環境を Intel+NVIDIA 機から **AMD Ryzen AI シリーズ APU**（RDNA 3.5 内蔵 Radeon
iGPU + XDNA2 NPU。実機例: 開発機 Ryzen AI MAX+ 395 / Radeon 8060S（Strix Halo）、
デプロイ先 Ryzen 5 350（Krackan Point））へ移行するにあたり、llama.cpp サイドカーをどの
バックエンドで動かすべきかを調査した。リポジトリ本体（Go）には CUDA/NVIDIA 依存は
なく、推論は外部 `llama-server` に HTTP 委譲しているため、選定はサイドカーの起動方法に
帰着する。

## Learnings

- **Windows の AMD iGPU は ROCm/HIP 非対応。** AMD の ROCm は Windows ではディスクリート
  Radeon（gfx110X/115X/120X 等）向けで、APU の統合 GPU はサポート外。したがって iGPU で
  GPU 加速するなら実用パスは **Vulkan** ビルドの llama.cpp 一択。
- **Vulkan 特性:** text generation は ROCm と同等〜やや上、prompt processing は ROCm に劣る
  傾向。消費電力は Vulkan の方が大きめ。iGPU はシステム RAM を共有するため、1.2B Q4
  （~0.8GB）では VRAM 制約は実質なし。**初回呼び出しでシェーダコンパイルの一時コスト**が
  あるため、初回タイムアウトは余裕を持たせる（`classify_timeout_ms: 8000` を据え置く理由）。
- **CPU フォールバックは常に有効。** Zen5 コアで Q4 1.2B は warm ~200-300ms（実測、§1.1）。
  `-ngl 0` で CPU 実行。追加ドライバ不要で最も確実。
- **NPU(XDNA2) は llama.cpp 非対応。** 利用には ONNX 変換 + Ryzen AI / Vitis AI Execution
  Provider + 新規 `Classifier` Go バックエンドが必要で、spec の Milestone 6（未着手）。
  今回はスコープ外とし、ドキュメント記載のみ。
- **起動オプション:** `llama-server -hf <repo>:<quant> --host 127.0.0.1 --port 8791 --jinja
  -ngl 99`（Vulkan/iGPU）。`--list-devices` で iGPU を確認、複数 Vulkan デバイス時は
  `GGML_VK_VISIBLE_DEVICES` で固定。
- **Windows での導入は winget が最短。** 公式パッケージ **`ggml.llamacpp`** が
  Windows Vulkan ビルド（`llama-*-bin-win-vulkan-x64.zip`、portable zip）を配布しており、
  `winget install ggml.llamacpp` で `llama-server` が入る（依存 `Microsoft.VCRedist.2015+.x64`
  は自動）。portable のため PATH 反映には新しいターミナルが必要。Vulkan ランタイム
  （`vulkan-1.dll`）は AMD Adrenalin ドライバ同梱で別途不要（無ければ
  `winget install KhronosGroup.VulkanRT`）。winget には Ollama/LM Studio 等の別系統サーバも
  あるが、本プロキシは llama.cpp の `/health` と `/v1/chat/completions` を前提とするため
  `ggml.llamacpp` が素直。

## Refs
- docs/records/20260606/1931-amd-vulkan-migration.md
- docs/decisions.md
- https://github.com/ggml-org/llama.cpp/discussions/15021
- https://rocm.docs.amd.com/projects/radeon-ryzen/en/latest/docs/advanced/advancedrad/windows/llm/llamacpp.html
- https://www.amd.com/en/products/processors/laptop/ryzen/ai-300-series/amd-ryzen-ai-7-350.html
