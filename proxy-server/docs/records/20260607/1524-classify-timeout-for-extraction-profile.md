# classify_timeout を抽出プロファイル向けに引き上げ (20260607 15:24) #pending

## Motivation

分類器（llama-server サイドカー）が起動・推論しているのに、毎回「⏳ DLP分類器が起動中です
（warmup中）」ブロックが返る。真因は機密検知でもサイドカー未起動でもなく、**classify 呼び出しの
タイムアウト不足**。デフォルトプロファイルが `jp_confidential_extraction`（最大384トークンの
多キーJSONを生成）に変わったのに、`classify_timeout_ms` が旧二値分類器（数トークン）向けの
5–8秒のまま。iGPU/CPU では 384 トークン decode + コールドキャッシュ/初回シェーダコンパイルで
8秒を超え、毎回 `context deadline exceeded` → fail-closed → warming メッセージになっていた。

## Goal

タイムアウト系パラメータのみを修正し、良性リクエストの誤 fail-closed を解消する。

## Records

- `internal/config/config.go`: 既定 `ClassifyTimeoutMS` 5000 → 30000。コメントを抽出プロファイル
  前提に更新。
- `config/config.example.yaml`: `classify_timeout_ms` 8000 → 30000。コメント更新。
- `config.demo.yaml` は keyword 分類器（即時・モデル不要）のため変更不要。

## Results

- `go build ./...` / `go test ./internal/config/... ./cmd/...` 成功。
- 稼働サービス（`C:\ProgramData\PromptGate\proxy.exe`）への反映には再ビルド＋再配置・再起動が必要。
  既存設定YAMLを使っている場合は `classify_timeout_ms` を 30000 に手動更新するか既定値に委ねる。

## Refs
- docs/records/20260607/1259-history-classifier-unavailable-message.md
