# 透過HTTPSインスペクション（hostsリダイレクト + 自己CA + Windowsサービス） (20260606 21:01) #fcbcee8

## Motivation

従来は `ANTHROPIC_BASE_URL`（env var → 平文HTTP `:8787`）でのみ Claude Code を捕捉していた。設定し忘れやすく回避も容易。`api.anthropic.com` をネットワーク層で既定捕捉し、HTTPS を検査できるようにする。要件は grill-me で確定（decisions.md 21:01 の9項目）。

## Goal

`mode: transparent` を既定に、hostsファイルで `api.anthropic.com`→`127.0.0.1` リダイレクト、自己発行のName制約付きルートCAで `:443` TLS終端・検査、上流は hosts を無視するリゾルバで実APIへ転送。proxy は Windows サービス、GPUサイドカーはユーザセッション。env var(HTTP) はフォールバックとして併存。

## Records

### 追加パッケージ
- `internal/mitm`：Name制約付き ECDSA P-256 ルートCAの生成/読込（`EnsureCA`）と SNI 毎のリーフ動的発行・キャッシュ（`GetCertificate`）。
- `internal/upstreamdial`：hostsを無視する `*http.Transport`。最小DNSクエリを外部リゾルバ（既定 `1.1.1.1:53`）へ投げて実IP解決→IPへダイヤル、SNIは原ホストのまま。Go標準リゾルバは hosts を先読みするため自前実装。
- `internal/hostsfile`：マーカー区切りブロックの追加/除去（起動時に整復、停止時に除去、クラッシュ耐性）。パスは注入可能（テストは実hostsを触らない）。

### 既存への変更
- `internal/config`：`mode` / `intercept{hosts,https_listen_addr,manage_hosts_file}` / `tls{ca_cert_path,ca_key_path,name_constraints}` / `upstream.resolver_dns` / `logging.file` / `service.name` を追加。データパスを `%LOCALAPPDATA%`→`%ProgramData%`（サービスは LocalSystem のため）。`%VAR%` 展開を汎用化。
- `internal/anthropic/forwarder.go`：`NewForwarderWithTransport`（hostsバイパス transport 注入）と任意メソッド/パスを保つ `ForwardRaw`（パススルー用）。SSE ストリーミングは既存の `copyFlushing` を流用。
- `internal/proxy/handler.go`：catch-all `/` の透過パススルー（メソッド+パスのみ監査、本文不記録）と、分類器 unavailable 時の専用「起動中」応答（fail-closed 維持）。
- `cmd/proxy`：サービス/コンソール検出（`svc.IsWindowsService`）、`:443`(TLS)+`:8787`(HTTP) のデュアルリスナ、起動順=バインド→hosts追加、停止順=hosts除去→shutdown、`-init-ca`、ファイルログ。`service_windows.go`/`service_other.go` でOS分離。
- PowerShell：`install.ps1`（要管理者・一度きり：build / `%ProgramData%` 配置+ACL / CA生成+`LocalMachine\Root`導入 / 自動起動サービス登録+復旧 / サイドカー用ログオンタスク。**既定では起動しない**＝稼働中セッションを壊さない）、`uninstall.ps1`（全revert）、`proxyctl.ps1`（status/start/stop/restart/logs）、`start.ps1 -SidecarOnly`（ユーザセッションでサイドカー常駐）。

### テスト（全て hermetic）
- mitm：CA再利用・Name制約の有無・`api.anthropic.com` は検証成功 / `evil.com` は制約で失敗・SNIキャッシュ。
- upstreamdial：DNSワイヤ往復・IDミスマッチ拒否・isIntercepted・clampTTL・**モックUDP DNS** 経由の `DialContext` E2E（127.0.0.1 のローカルリスナへ）。
- hostsfile：一時ファイルへの add/idempotent/remove/CRLF保持/欠損ファイル no-op。
- config：既定値・`%VAR%`展開・部分上書きでの既定維持。
- anthropic：`ForwardRaw` のメソッド/パス/クエリ/ヘッダ保持。
- proxy：未知パスのパススルー・分類器unavailableで no-egress + 専用メッセージ。

## Results

- `gofmt -l`（自分の変更分）/ `go vet ./...` / `go build ./...` / `go test ./...` いずれも green（CRLFワークツリー由来の gofmt ノイズはコミット時に LF 正規化されるため無害）。
- **結合テストは未実施（意図的）**：実 hosts編集・443バインド・CA導入・実APIアクセスは、このマシンで稼働中の Claude セッションを壊し得るため、ユーザ指示があってから実施する。`install.ps1` も未実行。

## Refs
- docs/decisions.md（20260606 21:01）
- docs/spec-proxy.md §5（接続方式）/ §1.1
- docs/todo.md（パススルーの DLP カバレッジ外）
