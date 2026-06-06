# LFM2-NPU を自作 Ryzen AI ONNX シムで実装 + 3 経路ベンチ記録 (20260607 01:25) #914dc35

## Motivation

NPU 対応の当初実装（worktree `feat/npu-backend`, 20260606 22:33）は「Lemonade Server(OGA) で
LFM2 を NPU 実行」前提だったが、再調査で **Lemonade(OGA) は LFM2 を実行できない**ことが確定
（`genai_config.json` 必須＝OGA 専用、LFM2 は OGA フロー対象外）。LFM2-on-NPU の唯一の正規ルートは
AMD Ryzen AI ネイティブ + `Run-LFM2.py` で、これは CLI のため proxy（HTTP サイドカー前提）には
**薄い OpenAI 互換シムの自作**が要る。実機で LFM2-on-NPU を動かし、NPU/GPU/CPU の速さも実測した。

## Goal

- LFM2 を NPU で動かす **自作 OpenAI 互換シム**を作り、proxy のサイドカーとして使えるようにする。
- `start.ps1` の NPU 起動を Lemonade → シムに刷新（`auto`=NPU→Vulkan→CPU は維持）。
- 3 経路ベンチを記録し、デフォルト方針（NPU 優先）の妥当性を整理（本番 Ryzen AI 340 含む）。

## Records

実装（worktree `feat/npu-backend`、Lemonade 前提の未コミット実装からの刷新）:
- **新規 `npu/npu_server.py`**: `Run-LFM2.py` の prefill/decode ループ（io_binding・KV/conv cache・512B
  アライン KV・greedy argmax・eos 停止）を Python 標準ライブラリ HTTP（`ThreadingHTTPServer`）で
  OpenAI 互換化。`POST /v1/chat/completions` + `GET /health`（llama.cpp と同じパス）。`apply_chat_template`、
  `temperature=0`、`max_tokens` 尊重、`response_format` は無視。生成はロックで直列化。127.0.0.1 のみ bind、
  生コンテンツ非ログ。**追加 pip 依存なし**（Ryzen AI conda env を使う）。`npu/README.md` も追加。
- **`start.ps1`**: NPU spec を Lemonade(`lemonade-server serve`, `/api/v1`, port 8000) から
  **`conda run -n ryzen-ai-1.7.1 python .\npu\npu_server.py --model <ONNX dir> --port 8792`** に差替え。
  `-Conda/-CondaEnv/-NpuServerScript/-NpuModel(ローカル dir)/-NpuPort 8792` を新設。シムは既定パスを出すので
  ChatPath/HealthPath は空。`Test-NpuAvailable` を「モデル dir + conda env の存在」に、`Test-NpuReady`
  （health + 1トークン補完）のパスを `/health` `/v1/chat/completions` に更新。`Get-Spec`/`Start-Sidecar`/
  auto フォールバック連鎖の骨格は流用。既定 `-Backend auto`（NPU→Vulkan→CPU）維持。
- **Go（機能変更なし・doc コメントのみ）**: `client.go`/`config.go`/`main.go`/`profile.go` の "Lemonade" 記述を
  自作シムに置換。`eval_bench_test.go` の NPU 実行例を `:8792`・既定パスに更新。chat_path/health_path 配線は
  将来 OGA(Lemonade で Llama 等)用に残置（LFM2-NPU では未使用）。
- **ドキュメント**: 本 record、knowledge `20260607/0125-lfm2-npu-shim-and-benchmark.md`、decisions（22:33 を
  Superseded + 新規）、spec §8.5/§8.7/§1.1/§25、config.example、CLAUDE/README、todo を Lemonade→シムに刷新。

## Results

- ローカル CI（worktree, Windows）: `go vet ./...` OK / `go build ./...` OK / `go test ./... -timeout 10m` **全 PASS**。
  `gofmt -l .` は全ファイルを列挙するが、これは **checkout の `core.autocrlf=true`（作業ツリーが CRLF）** による
  既存事象で本変更とは無関係（コミット blob は LF で gofmt 準拠。編集した Go 5 ファイルは LF 正規化比較で gofmt-clean を確認）。
- ベンチ（開発機 Strix Halo, LFM2-1.2B, 485-tok, 20 iters, prompt-cache 無効）:
  - GPU(Vulkan 8060S): prefill 5227 / decode 199.8 tok/s / e2e 213ms（開発機では最速）。
  - NPU(XDNA2): prefill 2208 / decode 50.2 tok/s / e2e 698ms。
  - CPU(16thr): prefill 1520 / decode 62.5 tok/s / e2e 703ms。
  - 3 経路とも正しい分類判定。詳細・340 外挿は knowledge note 参照。
- 方針: 本番機 **Ryzen AI 340 では NPU が e2e 最速の見込み**（NPU 同一・iGPU 1/10・prefill 支配的）。
  → デフォルト **NPU 優先（auto: NPU→Vulkan→CPU）** を採用。開発機での高速化は `-Backend vulkan`。
- 実機 e2e（start.ps1 auto で NPU 選択 → 監査 backend=npu → フォールバック確認）: （実行後に追記）。

## Refs
- docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md（シム設計 + ベンチ + 340 外挿）
- docs/knowledges/20260607/0134-lfm2-finetune-npu-conversion.md（JP fine-tune を NPU 化する変換）
- docs/knowledges/20260607/0136-ryzen-ai-npu-install.md（conda+PATH+AMD SDK 導入手順）
- docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md
- docs/records/20260606/2233-npu-backend.md（Lemonade 前提だった先行実装。本 record で刷新）
- docs/decisions.md（AMD NPU 対応, 20260607 更新）
- https://huggingface.co/collections/amd/ryzen-ai-171-npu-lfm2-models
