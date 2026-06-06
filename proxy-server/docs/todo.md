# TODO / Deferred Issues

## Claude Code の CA 信頼（当初「Windowsストアを見ない」懸念 → 実機で否定）

- Status: Resolved
- Discovered: 20260606 (docs/records/20260606/2230-sandbox-integration-test.md)
- Resolved: 20260606 (docs/records/20260606/2339-real-claude-code-lfm-verify.md)

### Detail

当初は「Claude Code は Node.js で、Node は既定で OS 証明書ストアを見ずバンドル CA を使うため、
`install.ps1` の `LocalMachine\Root` 導入だけでは信頼されず `NODE_EXTRA_CA_CERTS` が要る」と
懸念していた。**Sandbox で実 Claude Code 2.1.167 を使って検証した結果、これは否定された。**
Claude Code は既定 `CLAUDE_CODE_CERT_STORE="bundled,system"` で **Windows システムストアを参照**
するため、`install.ps1` の CA 導入だけで透過インターセプトが成立した（`NODE_EXTRA_CA_CERTS`
無しの run でも `/v1/messages` が proxy に到達）。`NODE_EXTRA_CA_CERTS` を明示しても成立。

### 結論

`install.ps1` の現行 CA 導入で実 Claude Code に十分。追加の環境変数設定は不要（任意で併用可）。
schannel 系の他クライアント（curl 既定等）は失効確認のため `--ssl-revoke-best-effort` 相当が要る
点は別項参照。

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
