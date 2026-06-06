# TODO / Deferred Issues

## matched_snippet に正確な機密スパンを格納する

- Status: Deferred
- Discovered: 20260606 (docs/records/20260606/2117-admin-observability-and-ui.md)

### Detail

`store_raw_text=true` のとき、検知でブロックされた「該当箇所」だけを `matched_snippet` に格納し、
管理UIでハイライト表示したい。現状は `prompt_text`（ライブターン全文）と `reason`（安全な理由）
のみを保存し、`matched_snippet` は常に NULL。

### Why deferred / Blocked by

`dlp.Evaluation`（internal/dlp/detector.go）はブロック理由とソースのみを返し、該当セグメントの
テキストや一致スパンを単独で露出しない。ルール検知は正規表現の一致位置が取れるが、LFM 検知は
理由文のみで位置情報を持たない。両者を一様に扱う API 拡張（例: `Evaluation.BlockSegment string`）が
必要で、今回のデモ範囲では `prompt_text` 全文表示で代替する。

### Unblock condition

`dlp.Detector.Evaluate` / `Classify` が該当セグメント（およびルール一致時はスパン）を返すよう
拡張し、handler から `matched_snippet` に truncate して格納する。
