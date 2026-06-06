# Decision Log

## 透過HTTPSインスペクションを既定の接続方式にする (20260606 21:01)

### Status
Accepted

### Context

従来は `ANTHROPIC_BASE_URL=http://127.0.0.1:8787`（env var → 平文HTTP）でのみ捕捉していた。設定を忘れやすく回避も容易なため、`api.anthropic.com` をネットワーク層（hostsファイル + HTTPSインスペクション）で**既定捕捉**する仕組みを追加する。要件確認は grill-me で実施。

### Decision

1. **両モード併存・既定=透過HTTPS**。env var(HTTP) モードはフォールバックとして残す（`mode: transparent|proxy|both`）。
2. **捕捉対象は `api.anthropic.com` のみ**（プロンプト本文を運ぶ唯一のホスト）。`intercept.hosts` で設定可能。
3. **ループバック回避**：上流転送は hosts を無視する独自リゾルバ（既定 `1.1.1.1:53`）で実IP解決。SNI は `api.anthropic.com` のままで実証明書を検証（`internal/upstreamdial`）。
4. **Windows サービス**（Session 0, LocalSystem）として常駐し、hosts編集 + `:443` TLS終端 + CA署名 + 転送を担う。
5. **GPUサイドカーはユーザセッション**でログオンタスクから起動（Session 0 では iGPU 不可）。proxy は `127.0.0.1:8791` に接続し、未起動時は fail-closed。
6. **非メッセージパスは透過パススルー**（DLPは `/v1/messages`・`count_tokens` のみ）。パススルーはメソッド+パスのみ監査記録（本文不記録）し、黙示バイパスにしない。
7. **サービスは自動起動**。サイドカーはログオンタスク。
8. **ウォームアップ中は fail-closed**。ただしコンテンツブロックと区別し「分類器が起動中。数秒後に再実行」という専用メッセージを返す。
9. **ルートCAは Name Constraints（`anthropic.com`）で制限**。鍵漏洩時も他サイトのなりすましに使えない（`internal/mitm`）。

実装: `internal/{mitm,upstreamdial,hostsfile}` 追加、`internal/config`（mode/intercept/tls/upstream.resolver_dns、データパスを `%ProgramData%` へ移設）、`internal/anthropic`（hostsバイパス transport + `ForwardRaw`）、`internal/proxy`（catch-all パススルー + ウォームアップ専用応答）、`cmd/proxy`（サービスモード + 443/8787 デュアルリスナ + `-init-ca`）、`install.ps1`/`uninstall.ps1`/`proxyctl.ps1`、`start.ps1 -SidecarOnly`。

### Consequences

- env var を設定しなくても Claude Code が既定で proxy を経由する。advisory から「回避しにくい」方向へ一歩前進（ただし管理者権限のあるユーザは依然解除可能で、改ざん耐性は非目標のまま）。
- ルートCAを信頼ストアへ導入するため、Windows Defender/AV が hosts編集やルート追加を検知する可能性がある（要許可、自動抑制しない）。
- 透過モードの結合テスト（実 hosts編集 + 443 + CA導入 + 実API）は、稼働中の Claude セッションを壊し得るため**ユーザ指示時のみ**実施。単体テストは全て hermetic。
- パススルー経路は明示的な DLP カバレッジ外（`docs/todo.md`）。

### Related Records
- docs/records/20260606/2101-transparent-https-interception.md

## AMD APU 推論バックエンド = llama.cpp Vulkan(iGPU)、CPU フォールバック (20260606 19:31)

### Status
Accepted

### Context

最終実行環境を Intel+NVIDIA 開発機から **AMD Ryzen AI シリーズ APU**（RDNA 3.5 内蔵
Radeon iGPU + XDNA2 NPU。実機例: 開発機 Ryzen AI MAX+ 395 / Radeon 8060S、デプロイ先
Ryzen 5 350）へ移行する。特定SKUに依存しない構成とする。NVIDIA 実行環境は不要。リポジトリ
本体（Go）に CUDA/NVIDIA 依存はなく、LFM 推論は外部 `llama-server`（llama.cpp）へ HTTP
委譲する設計のため、移行の実体は「サイドカーをどのバックエンドで起動するか」と、起動
スクリプト・設定・ドキュメントの AMD 化に集約される。調査詳細は
docs/knowledges/20260606/1931-amd-igpu-vulkan-rocm.md。

### Decision

- 推論サイドカーは **llama.cpp の Vulkan ビルドで内蔵 Radeon iGPU にオフロード**
  （`-ngl 99`）を既定とし、**CPU（`-ngl 0`）をフォールバック**とする。
- **ROCm は採用しない**（Windows の AMD iGPU は ROCm 非対応）。
- **NPU(XDNA2) は今回スコープ外**。ONNX + Vitis AI EP + 新規 Go バックエンドが必要で、
  spec の Milestone 6 として将来対応（ドキュメント記載のみ）。
- `start.ps1` を拡張し、サイドカー自動起動→`/health` 待ち→proxy 起動までを 1 コマンド化
  （`-Backend vulkan|cpu`、`-Classifier keyword`/`-NoSidecar`/既存稼働時はサイドカー不起動）。
- Go コード・`go.mod`・設定スキーマは無改修（バックエンド差はサイドカー起動オプションで吸収）。

### Consequences

- AMD APU 単体で GPU 加速付きのローカル LFM 推論が動く。NVIDIA 依存は完全に除去。
- Vulkan の初回呼び出しはシェーダコンパイル分のコストがあるため `classify_timeout_ms: 8000`
  を据え置く（暖機後は短縮可）。
- Vulkan ビルドの `llama-server` と AMD グラフィックスドライバ（Vulkan ランタイム）が前提に
  なる。Vulkan版 `llama-server` は **`winget install ggml.llamacpp`**（公式 winget パッケージが
  Windows Vulkan ビルドを導入）で入る。Vulkan ランタイムは AMD ドライバ同梱。未導入時は
  `-Backend cpu` または `-Classifier keyword` で動作可能。
- NPU を使わないため XDNA2 の 50 TOPS は当面活用しないが、1.2B Q4 では iGPU/CPU で十分。

### Related Records
- docs/records/20260606/1931-amd-vulkan-migration.md
