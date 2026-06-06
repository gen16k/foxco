# 透過HTTPSインターセプションの結合テスト（Windows Sandbox） (20260606 22:30) #pending

## Motivation

`2101-transparent-https-interception` で実装した透過インターセプションは、ユニットテストは
全て hermetic だが**結合テスト（実 hosts編集・`:443`バインド・CA信頼ストア導入・実
`api.anthropic.com` アクセス）は意図的に未実施**だった。これらはホスト上で稼働中の Claude
セッションを壊し得るため。ユーザが **Windows Sandbox**（使い捨て・NAT分離のVM）を用意した
ので、ホストや他セッションに一切影響を与えずに結合テストを実施できるようになった。

## Goal

`install → 起動 → 各プローブ → uninstall` を隔離VMで端から端まで流し、機構が実際に動くことを
確認する。特に **hostsバイパスリゾルバが実 `api.anthropic.com` へ到達する**ことを、無害な
401プローブ（無効キー＋良性プロンプト、機密ゼロ）で実証する。

## Records

### テストハーネス（`test/sandbox/`、新規）
- `run-sandbox.ps1`（ホスト側ランチャ）：proxy.exe をホストでビルド → 共有結果フォルダを
  proxy-server **外**（read-onlyマッピングと衝突しない位置）に用意 → スクリプト位置から
  絶対パスを導出して `.wsb` を生成（マシン固有パスはコミットしない）→ Windows Sandbox 起動。
- `run-tests.ps1`（VM内オーケストレータ、ASCII-only＝PS5.1互換）：read-only マップ
  `C:\repo` を `C:\work` へコピー → `install.ps1 -SkipBuild` → `proxyctl start` → curl.exe
  で段階プローブ → `uninstall.ps1` → `transcript.txt` + `results.json` + 各レスポンスボディを
  共有フォルダへ。日本語の warming/block 文字列は PS5.1 のエンコーディング問題を避けるため
  スクリプト内では判定せず、ボディをホストへ保存してホスト側で確認する方式。
- `install.ps1`：`-SkipBuild` スイッチ追加（`go build` を省きホストビルド済み exe を再利用。
  サンドボックス/CI 向け。VM に Go は不要）。
- 接続モデル：proxy-server を `C:\repo` に **read-only** マップ、共有結果フォルダを `C:\share`
  に **read-write** マップ、`<LogonCommand>` が logon 時に `run-tests.ps1` を自動実行。

### 検証マトリクス（32項目、全て PASS）
クライアントは Windows 同梱 `curl.exe`（schannel → マシン信頼ストア。`-k` は使わず、
ハンドシェイク成功＝導入CA経由でリーフが信頼された証明）。
- install：CA を `LocalMachine\Root` に導入（`CN=Local LFM DLP Proxy CA`）／サービス
  Automatic 登録／サイドカー logon タスク登録。
- start：サービス Running・hosts ブロック投入・`api.anthropic.com`→`127.0.0.1`・`:443` 待受。
- warming（既定 llama 構成・サイドカー無し）：良性リクエスト→HTTP 200・ログ
  `classifier_unavailable`・401ではない＝**外部送信ゼロで「⏳ DLP分類器が起動中です」専用文**。
- keyword へ再構成＋再起動。
- forward：良性→ALLOW→転送→**実 `api.anthropic.com` から本物の401**
  (`authentication_error` / 実 `request_id`)＝**`1.1.1.1` バイパスでループバックせず実APIへ到達**。
- block：`"my password is ..."`→HTTP 200・ログ `BLOCK`（reason `password or credential
  keyword`）・**upstream未呼出**。
- passthrough：`GET /v1/models`→401・ログ `PASSTHROUGH path=/v1/models`（無検査透過＋監査）。
- uninstall：service/task/CA/hosts すべて除去、`api.anthropic.com` が実IP（`160.79.104.10`）へ復帰。

### 運用上の手順知見
- Windows Sandbox（GUI）は **RDPの対話デスクトップが前面のときのみ**起動が成立する。非対話/
  バックグラウンド文脈からの `Start-Process WindowsSandbox.exe` はエラーも出ず VM が上がらない。
  → ハーネス起動はユーザの対話ターミナルから実行するのが確実。
- サンドボックスのクリーン終了はウィンドウを×で閉じる（またはVM内 `shutdown /s` だが後者は
  無害なエラーダイアログが出る）。ホストから client プロセスを force-kill すると VM ワーカー
  `vmwp` が孤児化し共有フォルダのハンドルを掴んだまま残る。当初は自動 `shutdown /s` を入れたが
  ダイアログ回避のため撤去し「終わったらウィンドウを閉じる」方式にした。

## Results

- **32/32 PASS**（`sandbox-share/results.json`、`transcript.txt`、各 `*-body.json` がホスト側に保存）。
- 機構を端から端まで実証：hostsリダイレクト → CA信頼 → `:443` TLS終端（SNI毎の動的リーフ）→
  DLP（warming/ALLOW/BLOCK）→ `1.1.1.1` バイパスで実API到達 → 透過 → 完全アンインストール。
- ホスト無傷：危険な変更は全て使い捨てVM内のみ。ホストの hosts/信頼ストア/`:443` は不変
  （ホストリポは read-only マップ、`C:\work` へコピーして操作）。
- `go build ./...` / `go vet ./...` / `go test ./...` green（Goコードは本セッション未変更）。

### 発見（follow-up を `docs/todo.md` に登録）
1. **動的リーフ/CA に CRL/OCSP 配布点が無い** → schannel系クライアント（Windows `curl` 既定・
   .NET・WinHTTP）は失効確認で `CRYPT_E_NO_REVOCATION_CHECK` ハードフェイル。テストは
   `--ssl-revoke-best-effort` で回避（チェーン信頼・ホスト名検証は維持）。
2. **Claude Code は Node.js** で (a) 既定で Windows 証明書ストアを参照しない＝`NODE_EXTRA_CA_CERTS`
   で `ca.crt` を指す必要、(b) 失効確認は既定で行わない。本テストは schannel/curl で機構を実証した
   が、**実 Claude Code 連携には `NODE_EXTRA_CA_CERTS`（または `SSL_CERT_FILE`）の設定が要る**。

## Refs
- docs/records/20260606/2101-transparent-https-interception.md
- docs/todo.md（CRL/OCSP・Node 信頼ストアの follow-up）
- test/sandbox/（run-sandbox.ps1 / run-tests.ps1）, install.ps1（-SkipBuild）
