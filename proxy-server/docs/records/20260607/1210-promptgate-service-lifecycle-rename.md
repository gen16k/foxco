# PromptGate へのリネームとサービス一元ライフサイクル化 (20260607 12:10) #pending

## Motivation

ログオン時に「サイドカーだけ」が上がり、肝心の proxy サービスは停止のまま、という事象が起きた。
原因は3要素が別々のライフサイクルだったこと：proxy=Windows サービス(自動起動)、サイドカー=
ログオン Scheduled Task、admin UI=手動。ユーザ要望は (1) サービス起動で proxy+サイドカー+
admin UI を一括起動・停止で一括終了、(2) 現行ログオン自動起動の廃止、(3) 製品名を **PromptGate**
へ統一（FoxCo は会社名として存置）、(4) 透過 proxy が不意に起動して稼働中の Claude セッションを
落とさないこと。

## Goal

- サービス `PromptGate` がユーザセッションのサイドカー/admin UI を起動・停止で連動制御。
- ログオン自動起動タスクを廃止し、サービスは **Manual**（boot 自動起動なし）。
- `LocalLfmDlpProxy` → `PromptGate`（モジュール/パス/サービス名/CA/hosts マーカ/Web ブランド）。
- 既存配備データ（CA・監査DB）は `%ProgramData%\LocalLfmDlpProxy`→`%ProgramData%\PromptGate` へ移行。

## Records

### Go
- `go.mod` module `local-lfm-dlp-proxy`→`promptgate`、全 import を一括置換（16 ファイル）。
- 既定値：CA/storage/log パスを `%ProgramData%\PromptGate\…`、`Service.Name=PromptGate`、
  CA CN=`PromptGate CA`/org=`PromptGate`、hosts マーカ `# >>> PromptGate >>>`、service ログ
  既定を `PromptGate\logs\proxy.log`。`config_test.go` 期待値も更新。
- 新 `config.Supervise{Enabled,SidecarTask,WebTask,WebPort,StopTimeoutMS}`（既定 `Enabled=false`：
  コンソール/dev と既存テストに無影響。インストール時のみ true）。
- スーパバイザを追加：`cmd/proxy/supervisor.go`（構造体+`portFromEndpoint`）、
  `supervisor_windows.go`（実装）、`supervisor_other.go`（no-op、`//go:build !windows`）。
  `application.start()` 末尾で `Start()`（schtasks /Run、ベストエフォート＝失敗してもサービスは
  起動して fail-closed）、`stop()` の hosts 復元後に `Stop()`（web→sidecar 順で /End ＋ ポート
  スコープの taskkill /T /F フォールバック）。`service_windows.go` は start()/stop() をそのまま
  呼ぶため start() がエラーを返すとサービスごと落ちる点に注意し、トリガ失敗は非致命にした。

### スクリプト
- `start.ps1`：`-SidecarOnly` を **フォアグラウンド実行**化（`& llama-server`：タスクの
  プロセスツリーに子が入り、/End や supervisor のポート kill でツリーごと終了＝孤児化しない）。
  新 `-WebOnly`（フォアグラウンドで admin UI）。env 導出を `Set-AdminEnvFromConfig` に共通化。
- `install.ps1`：サービス **Manual**＋インストーラでは起動しない。ログオントリガ廃止、
  `PromptGate-Sidecar` / `PromptGate-WebUI` の **RunOnDemand** タスク2本（Interactive 主体、
  `MultipleInstances=IgnoreNew`）。`web/` を InstallRoot へ robocopy（node_modules/.next 除外）。
  旧 service/task 削除＋データ dir 移動＋旧 hosts ブロック除去＋移行 config のパス置換。
  配備 config に `supervise:`（enabled:true）を未存在時のみ追記。
- `uninstall.ps1` / `proxyctl.ps1`：新名称、旧名称も含めて掃除。`proxyctl start/stop` は
  サービス操作のみ（supervisor が3者を制御）。

### Web / Docs
- ブランド：layout title / TopBar / login 見出しを `PromptGate`、cookie
  `promptgate_admin_session`、sound key `promptgate.sound`、`web/package.json(.lock)` name
  `promptgate-admin-ui`、`web/README.md`。🦊 アイコンは存置。FoxCo は会社/publisher として存置。
- `CLAUDE.md` / `README.md` / `docs/spec-proxy.md` の製品名を PromptGate に。ルート README は
  既に component を「PromptGate」と表記していたため据え置き。過去 work records は履歴として不変更。

## Results

- `go build ./...`（windows）と `GOOS=linux go build ./...`（supervisor_other 検証）OK。
  `go vet ./...` OK。`go test ./...` 全 ok。`gofmt` は CRLF チェックアウトで全ファイルが差分扱いに
  なるため、編集ファイルを LF 正規化して個別確認＝clean。git は autocrlf=true で LF 正規化保存。
- 透過 proxy はこのセッション中に**起動していない**（hosts の `api.anthropic.com` は無改変）。
  ライフサイクルの実機検証は別途、`api.anthropic.com` を書き換えない安全構成（mode:proxy/予備
  ポート、または manage_hosts_file:false）で行う想定（計画の Verification 節）。

## Refs
- docs/todo.md「透過モード(:443のみ)だと admin UI が admin API に到達できない」（既知・本変更後も該当）
- docs/decisions.md「サービス一元ライフサイクル: サイドカー/UI はユーザセッションタスク経由 (20260607 12:10)」
