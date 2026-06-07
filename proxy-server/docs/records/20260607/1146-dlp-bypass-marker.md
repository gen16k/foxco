# 明示的ユーザーバイパスマーカー（誤検知回避）+ admin の BYPASS 表示 (20260607 11:46) #pending

## Motivation

LFM/キーワード分類器は曖昧判定のため**明らかな誤検知**でブロックすることがある
（docs/todo.md「LFM fail-closes on benign input」「Proxy blocks Claude Code's own
injected context」）。現状の唯一の回避策は `ANTHROPIC_BASE_URL` を外す/プロキシを止める
ことで、**監査記録が一切残らない**。本 proxy は advisory・非タンパープルーフ（env を
外せば回避可能）なので、回避を防ぐより**監査可能で範囲限定の回避手段**を用意する方が
安全、という判断。あわせて、バイパス発生時は admin UI が `ALLOW`/`BLOCK` と区別できる
フラグを表示する（ユーザー要望）。

## Goal

- 最新 user ターンのユーザー入力テキストにマーカー（既定 `#dlp-allow`）が含まれる場合、
  そのライブターンのみ DLP ブロックを通過させる。スコープはフルバイパス（確定ルールも
  当該ターンではスキップ）。ユーザー確認済み。
- 履歴サニタイズは維持。検出はユーザー入力テキスト限定（tool_result 除外）。
- 監査に第3判定 `BYPASS` を記録し、admin UI で区別表示する。

## Records

実装当初は別ブランチ（feat/e2e-multiturn-verify、当時 web admin はプレースホルダ）上で
作成したが、その後 main に admin UI（Next.js）+ storage/handler リファクタ（`proxy.New`
への `storeRaw` 追加、`AuditEvent` への `Path`/`PromptText`/`MatchedSnippet`、record 関数の
シグネチャ変更、`PASSTHROUGH` 経路）がマージ済みと判明。最新 main へ再統合した。

Go（すべて proxy-server/ 配下）:

- `internal/config/config.go`: `DLP.Bypass{Enabled,Marker}` 追加。`Default()` で
  `Enabled:true, Marker:"#dlp-allow"`。`Load()` で「enabled かつ marker 空」を disabled
  に正規化（空文字の全一致で DLP 無効化を防止）。
- `config/config.example.yaml`: `dlp.bypass:` ブロックを説明付きで追加。
- `internal/anthropic/messages.go`: `Block.SetText` と `Message.SetStringContent` を追加。
- `internal/dlp/detector.go`: `EvaluateHistoryOnly` 追加（ライブターンを分類せず履歴のみ
  NG 抽出）。
- `internal/proxy/bypass.go`（新規）: `containsBypassMarker`（string content と type:"text"
  ブロックのみ走査、tool_result/tool_use 除外）、`stripBypassMarker`（シェイプ保持、空に
  なる text ブロックは drop、残余が空なら residualEmpty）。
- `internal/proxy/handler.go`: `BypassConfig` 型、`Handler.bypass`、`New` に第9引数追加。
  `process` にバイパス分岐（unparseable は対象外＝fail-closed のまま）。`bypassForward`
  （ストリップ→履歴サニタイズ→転送、count_tokens も実転送、ストリップ後に liveText 再計算）、
  `recordBypass`（`Decision:"BYPASS"`, `Path`, `PromptText`, `upstream_called=true`）、
  `safeBypassDetails`（reason=user_bypass_marker, source=user）。
- `cmd/proxy/main.go`: `cfg.DLP.Bypass` を `proxy.New` に配線。有効時は起動 WARN。
- `internal/storage/sqlite.go`: `AuditEvent.Decision` コメントを
  `ALLOW|BLOCK|BYPASS|PASSTHROUGH` に更新（admin の decision フィルタは passthrough で
  `BYPASS` をそのまま絞り込めるため Go 側変更なし）。

admin UI（proxy-server/web/）:

- `components/common/DecisionBadge.tsx`: 2状態（block か否か）から決定→スタイルの map に
  変更。BYPASS=amber(warn)、PASSTHROUGH=blue(accent)、BLOCK=rose、ALLOW=green、未知は ALLOW。
- `components/history/EventFilters.tsx`: decision フィルタに `BYPASS`/`PASSTHROUGH` の
  選択肢を追加。

テスト:

- `internal/proxy/handler_test.go`: 既存 `New` 呼び出し3箇所に `BypassConfig{}` を追加。
  `newBypassHandler`/`newBypassCaptureHandler` を追加し、バイパス9ケース（転送+ストリップ、
  ルール越えフルバイパス、tool_result 内マーカー非バイパス、履歴サニタイズ維持、count_tokens、
  text ブロック検出、マーカーのみ、無効時ブロック、監査 `BYPASS`）を追加。
- `internal/dlp/detector_test.go`: `EvaluateHistoryOnly` のテスト追加。
- `test/e2e/multiturn_test.go`: `proxy.New` 呼び出しを9引数に更新。**これは `storeRaw`
  追加時に未更新だった潜在的コンパイル破損（`-tags e2e` でのみコンパイル）を併せて修正**。

ドキュメント:

- `docs/spec-proxy.md`: §9.5（バイパス仕様）追加、§15.2 に `BYPASS`（と PASSTHROUGH）を追記。
- `docs/decisions.md`: 設計判断を記録。
- `docs/todo.md`: 関連 TODO に手動回避手段として相互参照、e2e 追加を Deferred 化。

## Results

- `go build ./...` / `go vet ./...` 成功。`go test ./...` 全パス（バイパス9ケース + 
  `EvaluateHistoryOnly` を含む）。`go vet -tags e2e ./test/e2e/` 成功。
- admin UI: `npm ci` 後 `npm run typecheck`（tsc --noEmit）成功。
- `gofmt`: 変更 Go ファイルは（作業コピーの CRLF を除き）clean。`core.autocrlf=true` のため
  `gofmt -l` は全ファイルを列挙するが git 格納は LF。新規 `bypass.go` は CRLF に統一。
- DLP 不変条件（BLOCK は無 egress／全チャネル検査／ルールガードレール）を当該マーカー付き
  ターンに限り意図的に緩和。オプトイン・ユーザー入力由来・config ゲート付き・`BYPASS` 監査
  付き。CLAUDE.md「緩和は明示し確認」に従いユーザー確認済み。

## Refs
- docs/decisions.md「明示的ユーザーバイパスマーカー（誤検知の手動回避）+ admin の BYPASS 表示」
- docs/spec-proxy.md §9.5, §15.2
- docs/todo.md「LFM fail-closes on benign input」「Proxy blocks Claude Code's own injected context」
