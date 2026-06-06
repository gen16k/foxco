# 管理UI向け観測レイヤ（opt-in 生プロンプト保存 + admin API + Next.js ダッシュボード） (20260606 21:17) #1be61f4

## Motivation

ローカル proxy の DLP 検知件数・検知内容・全プロンプト履歴を確認する手段が無い。ユーザーが
Grafana 風のインタラクティブな管理UI（Next.js, 認証あり）を要望。現状の監査DBはメタデータのみで
本文・機密値を保存しないため、要望を満たすにはオプトインの生データ保存と読み取りAPIが必要。

## Goal

1. `store_raw_text=true` のときライブターン本文を保存（ALLOW/BLOCK 双方、既定は従来通り保存なし）。
2. 読み取り専用 admin API（`/admin/stats|events|events/{id}|meta`、localhost、任意 Bearer 認証）。
3. Next.js 製 Grafana 風ダッシュボード（BFF 経由、ID/PW 認証）で件数・時系列・履歴を可視化。
4. セキュリティ不変条件の緩和をドキュメントで明示（決定記録 + CLAUDE.md）。

## Records

### Go バックエンド（本記録の対象）
- `internal/storage/sqlite.go`: `audit_events` に nullable 列 `path`/`prompt_text`/`matched_snippet`
  を追加。`AuditEvent` を拡張（`Path string`, `PromptText/*MatchedSnippet *string` で NULL 区別）。
  `Open()` に冪等マイグレーション `migrate()`（`PRAGMA table_info` → 欠落列のみ `ALTER TABLE`）を追加。
- `internal/storage/query.go`（新規）: `Query`(動的 WHERE はバインド引数のみ・`LIKE ESCAPE` で injection 安全),
  `Get`, `Stats`（by_source/top_reasons は details_json を Go で集計、p95 は Go 算出、allow/block の
  時系列を span に応じて hour/day/week バケット化）。JSON タグは camelCase の admin API 契約。
- `internal/proxy/handler.go`: `Handler.storeRaw` + `New(...)` 末尾引数を追加。`process` でライブターン
  本文を 1 度だけ取得し `path`/`liveText` を record ヘルパへ。`promptPtr`(storeRaw かつ非空のみ・
  16KiB rune 安全 truncate)。`request_unparseable` は本文なし。count_tokens は `path` で区別。
- `internal/admin/handler.go`（新規）: `Reader` interface（`*storage.Store` が充足）、`Meta`、
  Go1.22 メソッド付きルート、任意 Bearer トークン（constant-time 比較）、エラーは JSON。
- `internal/config/config.go` + `config/config.example.yaml`: `Admin{Enabled,AuthToken}` 追加、
  `store_raw_text` のオプトイン注記。
- `cmd/proxy/main.go`: `store` を巻き上げ、`proxy.New(..., StoreRawText)`、`admin.enabled && store!=nil`
  で admin handler を同一 mux に登録。`store_raw_text=true` 時に警告ログ。

### テスト（test-first、LFM モデル不要）
- `storage/migrate_test.go`: 旧スキーマDBへの列追加 / 冪等 / 旧行保持 + 新列書込。
- `storage/query_test.go`: 新着順・ページング・decision/source/q フィルタ・日付範囲・injection 安全・
  p95・series 集計・空DB・Get。
- `proxy/handler_test.go`: raw on/off の ALLOW/BLOCK 捕捉、ライブターンのみ、unparseable で NULL、
  大プロンプト truncate（既存テストの `New` 引数も更新）。
- `admin/handler_test.go`: 各ルート・フィルタ・404・405・空DB・トークン認証。

### Web（別記録で詳細化）
- `web/` に Next.js（App Router/TS/Tailwind/Tremor/SWR/iron-session）。BFF（`app/api/admin/*`）で
  Go admin API を server-side 取得（CORS 不要）。ID/PW ログイン + middleware。

## Results

- `go build ./...` / `go vet ./...` 無警告、`go test ./...` 全通過。
- `gofmt -l .` は本チェックアウトの CRLF（autocrlf=true、`i/lf w/crlf`）由来で全ファイルを列挙するが、
  git はコミット時 LF へ正規化（`git diff` は空）。変更/新規ファイルを LF 化して `gofmt -l` した結果は
  全て clean ＝実フォーマット問題なし。
- 既定（store_raw_text=false）の posture は不変＝メタデータのみ。

## Refs
- docs/decisions.md（オプトイン生プロンプト保存 + admin API）
- docs/todo.md（matched_snippet の正確なスパン抽出を先送り）
- 計画: ../../../.claude/plans/ui-proxy-ui-grafana-ui-next-js-prancy-summit.md
