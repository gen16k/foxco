# 実Claude Code＋本物LFMでの透過インターセプション検証（Windows Sandbox） (20260606 23:39) #pending

## Motivation

`2230` の結合テスト（curl, keyword分類器）で機構は実証済みだが、ユーザ要望で
**実際の Claude Code CLI** と **本物の LFM 分類器** を使って、(1) Claude Code が我々の
CA を信頼し透過インターセプトされるか、(2) 本物の LFM が機密を BLOCK し良性を ALLOW するか、
を確認する。

## Goal

Windows Sandbox 内で実 Claude Code をインストールし、CPU版 llama.cpp + LFM2.5-1.2B で
proxy を動かして、curl（制御リクエスト）と実 Claude Code の両方で ALLOW/BLOCK/sanitize を観測する。

## Records

### ハーネス追加（`test/sandbox/`）
- `run-claude-tests.ps1`：実 Claude Code（keyword分類器）。CA信頼の2経路（システムストアのみ／
  `NODE_EXTRA_CA_CERTS`）を検証。
- `run-claude-lfm-tests.ps1`：実 Claude Code ＋ **本物の LFM**（CPU llama.cpp）。curl と Claude Code で
  BLOCK/ALLOW を検証。
- `run-sandbox.ps1`：`-Runner` で実行スクリプトを選択、永続キャッシュ `C:\cache`（llama.cpp バイナリ＋
  GGUF）を read-write マウント、共有フォルダを `%TEMP%` へ（リポ汚染回避）、孤児VMのフォルダロックに
  強い後始末に変更。
- `run-tests.ps1`：curl に `--ssl-revoke-best-effort` を追加（後述）。

### 検証中に判明した実運用上の要点
1. **Windows Sandbox(GUI) は RDP 対話デスクトップが前面のときのみ起動成立**。非対話/バックグラウンド
   文脈からの `Start-Process WindowsSandbox.exe` は無言で失敗（VM が上がらない）。ユーザの対話ターミナル
   から起動するのが確実。
2. **クリーン終了は VM 内 `shutdown /s`（自動シャットダウン）か×閉じ**。ホストから client を force-kill
   すると VM ワーカー(`vmwp`)が孤児化し共有フォルダのハンドルを掴んだまま残る。`hcsdiag kill <GUID>` で
   個別終了可（要権限）。各ランナーは終了時に自動シャットダウンする。
3. **schannel系クライアント（Windows同梱 curl 既定）は失効確認を強制**。我々のリーフ/CA は CRL/OCSP を
   持たないため `CRYPT_E_NO_REVOCATION_CHECK`。`--ssl-revoke-best-effort`（失効確認のみ緩和、チェーン信頼は維持）で回避。
4. **llama.cpp の `-hf` ダウンローダは素のサンドボックスで TLS 検証に失敗**（CAバンドル無し:
   `HTTPLIB failed: SSL server verification failed`）。GGUF をホスト側で事前取得し `-m <local>` で起動して回避。
5. **llama.cpp Windows ビルドは VC++ ランタイム依存**（winget は依存として導入するが手動 unzip では入らない）。
   無いと llama-server.exe がローダ段階で無言終了。`vc_redist.x64.exe /install /quiet` を先に実施。

## Results

### phase 2: 実 Claude Code ＋ keyword分類器（13/17）
- **実 Claude Code 2.1.167 のトラフィックが丸ごと透過インターセプトされた**（テレメトリ `/api/event_logging`、
  `/api/claude_code/*`、`/v1/messages` すべて proxy 通過）。
- **CA は Windows システムストアだけで信頼**（`NODE_EXTRA_CA_CERTS` 無しの run でも proxy 到達）。
  `NODE_EXTRA_CA_CERTS` 経由でも成立。Claude Code 既定 `CLAUDE_CODE_CERT_STORE="bundled,system"` のとおり。
- BLOCK は未再現：keyword分類器が Claude Code の巨大システムプロンプト/コンテキストの語に誤反応し、
  liveブロックではなく **sanitize（履歴ユニット除去）** 経路になった（＝機密の egress は防がれたが、ハードBLOCKではない）。

### phase 3: 実 Claude Code ＋ 本物の LFM（18/19）
- **curl 機密（liveターン）→ 本物のLFMがハードBLOCK**：`result=BLOCK reason="contains real password" source=lfm`、
  HTTP 200、外部送信ゼロ、`LOCAL_DLP_NOTE` 付きブロック通知。**LFM が自分の判断で機密を遮断**。
- **curl 良性 → LFM ALLOW → 実Anthropic 401**。
- **実 Claude Code 良性 → LFM ALLOW**（keyword の誤検知問題は解消。実LFMはシステムプロンプトを過剰ブロックしない）→ 401。
- **実 Claude Code 機密 → sanitize（機密ユニット除去、egressゼロ）**。Claude Code は除去後リクエストの 401 を受信。
- 唯一の非PASS（`claude.lfm_secret_client_block`）は「ハードBLOCK時の通知到達」を見る項目で、実Claude Codeでは
  sanitize経路だったため通知ではなく401になっただけ。**機密保護は成立**。
- 知見: **機密が単独のliveターン→ハードBLOCK／Claude Codeの複雑リクエストに埋め込まれ→sanitizeで除去**。いずれも egress を防ぐ。

### 総括
透過インターセプト・CA信頼（システムストア）・本物LFMによる BLOCK/ALLOW・sanitize による機密保護を、
**実 Claude Code** で端から端まで確認。`install.ps1` の CA 導入だけで実 Claude Code が捕捉される
（`NODE_EXTRA_CA_CERTS` は不要だが併用可）ことが確定した。

## Refs
- docs/records/20260606/2230-sandbox-integration-test.md（phase 1: curl 32/32）
- docs/todo.md（Node信頼ストアの件は本検証で解消）
- test/sandbox/run-claude-tests.ps1 / run-claude-lfm-tests.ps1 / run-sandbox.ps1
