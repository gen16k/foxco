# TODO / Deferred Issues

## 透過パススルー経路は DLP 検査外

- Status: Deferred
- Discovered: 20260606 (docs/records/20260606/2101-transparent-https-interception.md)

### Detail

透過モードでは `api.anthropic.com` への全パスが proxy に届くが、DLP 検査するのは
`/v1/messages` と `/v1/messages/count_tokens` のみ。それ以外（例 `GET /v1/models`、
将来の `POST /v1/files` 等）は内容を検査せず上流へ透過転送する。Claude Code を壊さない
ための意図的なカバレッジ外で、監査ログには `decision=PASSTHROUGH`（メソッド+パスのみ、
本文は不記録）として残し、黙示のバイパスにはしていない。

### Why deferred / Blocked by

現状 Claude Code が本文を運ぶのは `/v1/messages` のみ。他経路のDLP化は parse 方式が
パスごとに異なり、誤検査で正規エンドポイントを壊すリスクがある。新たな本文付き egress
経路（ファイルアップロード等）を Claude Code が常用し始めたら検査対象に加える。

### Unblock condition

`/v1/messages` 以外に本文（プロンプト/機密になり得るデータ）を運ぶ経路が実利用される。

## パススルー要求ボディは 32MiB でバッファ上限

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2101-transparent-https-interception.md)

### Detail

catch-all パススルーは要求ボディを `defaultMaxBody`(32MiB) まで読み切ってから転送する。
巨大アップロードがあると切り詰められうる。通常の Claude Code 利用では問題ないが、必要に
なれば `ForwardRaw` を `io.Reader` ストリーミングに変更してボディ無制限で素通しする。
