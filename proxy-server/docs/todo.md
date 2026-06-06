# TODO / Deferred Issues

## Claude Code (Node.js) は Windows 信頼ストアを見ない → CA が信頼されない

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2230-sandbox-integration-test.md)

### Detail

`install.ps1` は CA を `Cert:\LocalMachine\Root` に導入するが、**Claude Code は Node.js**
で動き、Node は既定で OS（Windows）の証明書ストアを参照せず、バンドルした Mozilla ルートを
使う。したがって透過モードで `api.anthropic.com` を MITM しても、Claude Code 側は導入した
CA を信頼せず TLS 検証に失敗する見込み。`NODE_EXTRA_CA_CERTS=<...\ca.crt>`（または
`SSL_CERT_FILE`）を Claude Code の環境に設定する必要がある。Sandbox 結合テストは schannel 系
の `curl.exe`（マシン信頼ストアを参照）で機構を実証したため、この差異は表面化しなかった。

### Why deferred / Blocked by

機構自体（hostsリダイレクト＋TLS終端＋DLP＋実API到達）は実証済み。実 Claude Code を使った
連携確認は次段。`install.ps1` に `NODE_EXTRA_CA_CERTS` のマシン環境変数設定を追加するか、
ドキュメントで案内するかを決める必要がある（環境変数の対象範囲・既存設定の上書き可否を検討）。

### Unblock condition

実 Claude Code を Sandbox 等で起動し、透過モードでブロック/許可が成立することを確認する段。

## 動的発行リーフ/CA に CRL/OCSP が無く schannel が失効確認で失敗

- Status: Open
- Discovered: 20260606 (docs/records/20260606/2230-sandbox-integration-test.md)

### Detail

`internal/mitm` が発行するリーフ/ルートCAには CRL Distribution Point / OCSP(AIA) が無い。
schannel 系クライアント（Windows 同梱 `curl` 既定・.NET `HttpClient`・WinHTTP）は失効確認を
既定で強制するため、チェーン自体は信頼されても `CRYPT_E_NO_REVOCATION_CHECK (0x80092012)` で
ハンドシェイクがハードフェイルする。Sandbox テストでは `curl --ssl-revoke-best-effort` で回避
した（失効確認のみ緩和、チェーン信頼・ホスト名検証は維持）。主対象の Claude Code(Node/OpenSSL)
は既定で失効確認しないため影響しないが、同一マシンの他 schannel クライアントが
`api.anthropic.com` を叩くと失敗し得る。

### Why deferred / Blocked by

主対象クライアント（Node）には無影響で、テストはフラグで回避済み。恒久対応するなら発行証明書に
失効情報を持たせない MITM CA の慣行に合わせて「クライアント側で soft-fail させる」前提を明文化
するか、必要なら CRL/OCSP を持たせる実装を足す。

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
