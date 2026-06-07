# 履歴セグメントの分類器不能を fail-closed として正しく扱う + ブロック通知の日英併記 (20260607 12:59) #cb21547

## Motivation

新規セッションの第1ターンで `hi` だけを送ったのに、`⚠️ ローカルDLP… 理由: 過去の履歴に機密情報が
残っています。/clear で…` というブロックが返る事象を調査した（セッション `6728881b`、
12:32:50 JST）。

一次証拠（`C:\ProgramData\PromptGate\logs\proxy.log` と監査DB `state\dlp.db`）から、真因は
**機密ではなく LFM サイドカー（llama-server:8791）のコールドスタート**だと判明:

- 12:29:42 サービス起動直後はサイドカー未 ready（health 失敗）。
- 12:32:57 / 12:32:58 `classify_error … context deadline exceeded`（8s タイムアウト）。
- `dlp.fail_closed: true` のため判定不能 → BLOCK（`classifier_unavailable` と `sanitize_failed` の2件、
  いずれも `upstream_called=0`）。

問題は、**履歴側で分類器が応答しなかった場合**の扱い。`detector.Evaluate` の履歴ループは
`r.Decision == Block` だけを見て `classifier_unavailable`（＝判定の不在）を機密の履歴と同一視し、
`HistoryNG` に積んでいた。その結果:

1. サニタイズが構造復元に失敗すると、誤解を招く「過去の履歴に機密情報が残っています / clear」を表示
   （真因はサイドカー起動中なので `/clear` しても直らない）。
2. サニタイズに成功した場合、**未判定の良性な履歴ユニットを無言で削除**してしまう恐れ。

ライブ側には `classifier_unavailable` 専用の「⏳ 起動中」メッセージがあるのに、履歴側には無かった。

## Goal

- 履歴セグメントが `classifier_unavailable` の場合は、機密検知ではなく **リクエスト全体を
  fail-closed（warming メッセージ）** として扱う。良性履歴の無言削除と誤メッセージを防ぐ。
- ついでに、ユーザー向けブロック通知を **日英併記**（英語も簡潔に）にする。

## Records

- `internal/dlp/types.go`: Source 定数を追加（`SourceRule` / `SourceLFM` /
  `SourceClassifierUnavailable`）。マジック文字列を排除し、`classifier_unavailable` の意味
  （「機密の証拠ではなく判定の不在」）をコメントで明文化。
- `internal/dlp/detector.go`: `Classify` を定数に置換。`Evaluate` の履歴ループで、Block かつ
  `Source == SourceClassifierUnavailable` の場合は `Evaluation{Block:true, BlockSource:
  classifier_unavailable}` を返して即 fail-closed（`HistoryNG` には積まない）。
- `internal/proxy/handler.go`: `classifier_unavailable` 比較を `dlp.SourceClassifierUnavailable`
  へ。これで履歴側 warming も既存の「⏳ 起動中」分岐に乗り、誤った sanitize 経路に行かない。
- 日英併記: `internal/anthropic/block.go` の `BlockText`（全ブロック共通ラッパー＋既定理由）、
  `handler.go` の warming / unparseable / sanitize_failed（通常＋bypass の2か所）メッセージ。
- テスト追加:
  - `dlp`: `TestEvaluateHistoryClassifierUnavailableFailsClosed` — 良性ライブ + 履歴側タイムアウト
    → `Block` かつ `BlockSource=classifier_unavailable`、`HistoryNG` 空。
  - `proxy`: `TestHistoryClassifierWarmingFailsClosedWithDistinctMessage` — 外部送信ゼロ、
    「起動」を含み「過去の履歴」を**含まない**ことを確認（修正前は sanitize_failed で「過去の履歴」が出る）。
  - `inference`: `TestParseReasonDecisionIsFaithfulToDecisionField` / `...AmbiguousIsError` —
    reason_decision パーサが `decision` にのみ忠実（良性理由でも `decision:BLOCK` なら遮断、`ALLOW`
    なら許可、両義/欠落はエラー→fail-closed）であることを固定。「モデル誤検知 vs パース問題」の
    切り分け中に追加（実モデルの生出力でも良性入力に `decision:"BLOCK"` を確認＝パーサではなくモデル起因）。

## Results

- `go vet` / `go build` / `go test ./... -timeout 10m` 全て成功。
- `gofmt -l .` は作業ツリーの CRLF により全 .go を列挙するが、編集ファイルは CRLF を除けば gofmt 準拠
  （`gofmt <f> | diff` で確認済み）。コミット時に LF 正規化される既存事象。
- **デプロイは未実施**: 稼働サービスは `C:\ProgramData\PromptGate\proxy.exe`（旧ビルド）。修正反映には
  `go build -o proxy.exe ./cmd/proxy` 後に install/proxyctl で再配置・サービス再起動が必要。

## スコープ外（意図的）

- `EvaluateHistoryOnly`（bypass 経路）は今回未変更。bypass は `#dlp-allow` をユーザーが明示入力した
  ときのみで今回の事象とは別経路。戻り値が `[]Segment` で「全体ブロック」を表現できず、変更には
  シグネチャ変更を要するため別タスクとする。

## Refs
- 調査セッション: `6728881b-4bef-4ed8-9814-273c3c5e49f2`
- proxy.log: `C:\ProgramData\PromptGate\logs\proxy.log`（12:32:57 / 12:33:00 の BLOCK 2件）
- 監査DB: `C:\ProgramData\PromptGate\state\dlp.db`
