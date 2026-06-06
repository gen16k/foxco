# start.ps1 で管理UIも同時起動 + ローカル限定 (20260606 22:06) #03b9f5a

## Motivation

ユーザー要望: proxy 起動時に管理 Web UI も一緒に起動したい。アクセスはいったんローカル限定に。

## Goal

`start.ps1` がサイドカー・プロキシに加えて Next.js 管理UIも起動し、UI は 127.0.0.1 のみで待受。

## Records

- `web/package.json`: `dev`/`start` を `-H 127.0.0.1` でループバック限定にバインド（既定ポート 3939）。
- `start.ps1`:
  - `-NoWeb` スイッチ追加（UI を起動しない）。
  - プロキシ起動前に UI を起動: `web/node_modules` 不在なら `npm install`、その後 `npm run dev` を
    `Start-Process`（バックグラウンド）。
  - 選択中の config から `listen_addr` と `admin.auth_token` を正規表現で抽出し、UI 子プロセスへ
    `PROXY_ADMIN_BASE_URL` / `PROXY_ADMIN_TOKEN` を環境変数として引き渡し（トークン自動一致）。
    Next.js は既存の process.env を `.env.local` で上書きしないため、ランチャ指定が優先される。
  - `finally` で UI（npm→node のプロセスツリー）を `taskkill /T /F` で停止。サイドカー停止も従来通り。
  - ヘッダコメント更新。
- README 更新（proxy-server/README.md, web/README.md）: ランチャが UI も起動・ループバック限定・`-NoWeb`。

## Results（検証）

- `start.ps1` 構文 OK（AST パース）。config 解析: example→token=''、demo→token='foxco-demo-token'。
- `.\start.ps1 -Classifier keyword -Config config\config.demo.yaml` を起動 →
  - `/login` 200、`/admin/meta` がトークン経由で `storeRawText:true` 返却（トークン引き渡し成功）。
  - 8787 / 3939 とも `LocalAddress=127.0.0.1` のみで待受。LAN IP(192.168.3.6):3939 は接続拒否＝ローカル限定を確認。
  - 後始末でポート解放確認。

## Refs
- docs/records/20260606/2152-web-admin-dashboard.md
- proxy-server/start.ps1, web/package.json
