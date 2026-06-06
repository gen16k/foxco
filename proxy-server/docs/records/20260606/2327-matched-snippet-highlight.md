# 検知該当箇所の保存とUIハイライト + ログイン文言/モック非マスク化 (20260606 23:27) #a02e877

## Motivation

ユーザーが管理UIのプロンプト本文を見たとき、(1) ログイン画面の「ローカル管理用。認証情報は
.env.local で設定します」の注記がダサい、(2) 本文がマスクされているのが気になる・どこがブロック
されたのか全文を見たい・センシティブ判定箇所をハイライトしてほしい、(3) そもそも proxy では
マスクしない設定では？という指摘。

実際には proxy は `store_raw_text=true` でライブターン全文を**無加工**で保存しており、UIも素の
まま表示する。見えていたマスクは **モックデータ（USE_MOCK=1）専用のプレースホルダ**だった。
ハイライトに必要な `matched_snippet` はバックエンドで常時未設定（todo 送り）だったため、ここを
実装して実データでもハイライトを効かせる。

## Goal

- ログイン画面の余計な注記を削除。
- `matched_snippet` に検知該当箇所を保存（ルール=正確なスパン、LFM=該当セグメント）。`store_raw_text`
  と同じオプトインゲート、`reason` には機密値を入れない。
- 詳細ドロワーで本文中の該当箇所を `<mark>` ハイライト。
- モックの BLOCK 文面を非マスク化（既知の偽サンプル値）し `matchedSnippet` を付与してハイライトを
  デモ可能に。

## Records

- `web/app/login/page.tsx`: 末尾の注記 `<p>` を削除。
- `internal/dlp/rules.go`: `RuleEngine.MatchSpan(text) (name, span, hit)` を追加（`FindStringIndex`
  で一致部分文字列を返す）。`Match` は `MatchSpan` 経由に。span は機密値そのものなのでオプトイン
  保存専用・reason には入れない旨をコメント。
- `internal/dlp/types.go`: `Result.Match`（機密該当テキスト）を追加。
- `internal/dlp/detector.go`: `Evaluation.BlockMatch` を追加。`Classify` でルール=`MatchSpan` の span、
  LFM=NG時に該当セグメント全文を `Match` に設定。`Evaluate` のライブブロックで `BlockMatch` を載せる。
- `internal/proxy/handler.go`: `recordBlock(...)` に `match` 引数を追加し `eval.BlockMatch` を渡す。
  `snippetPtr(match)` を追加（`storeRaw && match!=""` のときのみ truncate して返す＝`promptPtr` と
  同じゲート）。`MatchedSnippet: h.snippetPtr(match)` で格納。sanitize_failed / request_unparseable は
  match 空。
- `web/components/history/EventDetailDrawer.tsx`: `Highlighted` コンポーネントを追加し、本文中の
  `matchedSnippet` の全出現を赤 `<mark>` で強調（完全一致・本文に無ければ素のまま）。別枠の
  「検知された箇所（センシティブ判定）」表示も維持。
- `web/lib/mock.ts`: BLOCK 文面を**全文（非マスク）**化し、AWS ドキュメントの `AKIAIOSFODNN7EXAMPLE`
  など既知の偽サンプル値を埋め込み。各 kind に `snippet`（本文の部分文字列）を追加し `matchedSnippet`
  に反映。先頭コメントを「マスクしない／ハイライト可視化のため」に更新。

### テスト

- `internal/dlp/rules_test.go`: `MatchSpan` が正確な値（周辺テキストを含まない）と空時の挙動を検証。
- `internal/dlp/detector_test.go`: `Evaluate(...).BlockMatch` がルール=正確スパン / LFM=該当セグメント
  になることを検証。
- `internal/proxy/handler_test.go`: ブロック時 `MatchedSnippet` が一致値、`reason` に機密値が漏れない、
  `store_raw_text=false` では `MatchedSnippet` が nil であることを検証。

## Results

- `gofmt -l .` 無出力 / `go vet ./...` / `go build ./...` / `go test ./...` パス。
- `npm run typecheck` / `npm run build` パス。
- セキュリティ: `matched_snippet` は `prompt_text` の部分文字列で新たな露出は増えない。同じ
  `store_raw_text` ゲートで保護、既定 false では従来どおり一切保存しない。`reason`/`details_json` には
  機密値を入れない（テストで担保）。

## Refs
- docs/decisions.md（20260606 21:20 エントリを更新）
- docs/todo.md（matched_snippet エントリを Resolved に）
- docs/records/20260606/2152-web-admin-dashboard.md
- docs/records/20260606/2218-admin-ui-mock-mode.md
