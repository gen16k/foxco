# AMD NPU(XDNA2) バックエンド追加 — NPU 優先 + 自動フォールバック (20260606 22:33) #pending

> **刷新 (20260607 01:25)**: 本 record は「Lemonade Server(OGA) で LFM2 を NPU 実行」前提だったが、
> Lemonade(OGA) は LFM2 を実行できないと判明。実装は **自作 Ryzen AI ONNX シム**に刷新した。
> Go の配線（chat/health パス可変化・上書きフラグ・`reason_decision_prompt`・監査 backend 実ランタイム化）は
> ランタイム非依存でそのまま流用。差替えは start.ps1 の NPU 起動とドキュメントのみ。
> → 詳細は **docs/records/20260607/0125-npu-shim-and-benchmark.md**。

## Motivation

Vulkan(iGPU) が安定したので、XDNA2 NPU で推論を試し、有効なら第一候補にしたい。NPU を切る
オプションも要る。調査で「llama.cpp/Ollama は NPU 非対応、NPU の実体は ONNX/OGA、Lemonade
Server が OpenAI 互換 HTTP を出す」ことが分かり、既存のサイドカー設計に最小変更で載せられる。

## Goal

- proxy に NPU(Lemonade/OGA) 経路を追加。`start.ps1` を `auto`（NPU→Vulkan→CPU, health gated）に。
- `-Backend npu|vulkan|cpu` で単一強制（vulkan/cpu が NPU 無効化）。
- Go の配線変更は最小（OpenAI 互換のまま、パスとモデルとプロファイルを切替）。
- 監査ログの backend を実ランタイム（npu/vulkan/cpu）に修正。

## Records

実装（worktree: feat/npu-backend）:
- `internal/inference/client.go`: `LlamaClient` に `chatPath/healthPath` と `SetPaths` を追加。
  既定は現状リテラル（`/v1/chat/completions`, `/health`）で llama.cpp と既存テストは無改修。
- `internal/inference/profile.go`: `reason_decision_prompt`（Schema 無し）を追加・登録。OGA は GBNF
  非強制のため、NPU は schema を送らず prompt + 寛容パースで判定。
- `internal/config/config.go`: `Inference` に `chat_path/health_path/backend` を追加。
- `cmd/proxy/main.go`: `-endpoint/-model/-profile/-chat-path/-health-path/-backend` 上書きフラグを追加し、
  start.ps1 をランタイム配線の単一の出所に。監査の backend を **実ランタイム**へ（従来は model 名）。
- `start.ps1`: `-Backend auto/npu/vulkan/cpu`（既定 auto）。`Get-Spec`/`Start-Sidecar`/`Test-NpuAvailable`/
  `Test-NpuReady`（health + 1トークン補完）でフォールバック連鎖を実装。NPU 選択時に endpoint/paths/
  profile/model/backend を proxy に渡す。
- `config/config.example.yaml`: NPU 用のコメント例を追記。
- テスト: `client_test.go` に `TestLlamaClientResponseFormatByProfile`（NPU プロファイルは response_format
  を送らない／既定は送る）と `TestLlamaClientSetPaths` を追加。
- ベンチ: `internal/inference/testdata/eval_cases.jsonl` と `//go:build evalbench` の計測テストを追加（後述）。
- ドキュメント: decisions.md（新規 + 旧 NPU 行を Superseded）、本 record、knowledge note、spec §1.1/§8.5/§25、
  todo.md（JP fine-tune の OGA 変換）、README/CLAUDE の run セクション。

## Results

- `gofmt -l .` / `go vet ./...` / `go build ./...` / `go test ./...`: （ローカルCIの結果をここに記録）
- NPU 実機検証（要 AMD NPU ドライバ + Ryzen AI 1.7.x + Lemonade、モデル pull 済み）:
  - `lemonade-server serve --port 8000` → `.\start.ps1`（auto が NPU 選択）→ ALLOW/BLOCK 各1件、監査 backend=npu。
  - NPU 停止 → auto が Vulkan→CPU へフォールバック、要求は失敗しない。
  - `evalbench` で cpu/vulkan/npu の精度・FP率・warm/ cold レイテンシを比較。
  （実機・パッケージ導入後に追記）

## Refs
- docs/decisions.md（AMD NPU 対応, 20260606 22:33）
- docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md
- https://ryzenai.docs.amd.com/en/latest/llm/overview.html
- https://huggingface.co/collections/amd/ryzen-ai-171-npu-lfm2-models
