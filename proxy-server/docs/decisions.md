# Decision Log

## AMD NPU(XDNA2) 対応: 自作 Ryzen AI ONNX シム経由、NPU 優先 + 自動フォールバック (20260607 01:25)

### Status
Accepted

### Context

直前の決定（22:33）は「Lemonade Server(OGA) で LFM2 を NPU 実行」前提だったが、再調査と実機検証で
**Lemonade(OGA) は LFM2 を実行できない**ことが確定（`genai_config.json` 必須＝OGA 専用、AMD 公式も
「LFM2 は OGA フロー対象外」と明記）。LFM2-on-NPU の唯一の正規ルートは **AMD Ryzen AI Software 1.7.1
ネイティブ + `Run-LFM2.py`**（token-fusion ONNX + `RyzenAILightExecutionProvider`）で、これは CLI のため
OpenAI 互換 HTTP サーバが無い。実機で LFM2-on-NPU を動作させ、NPU/GPU/CPU を実測した
（docs/records/20260607/0125-npu-shim-and-benchmark.md, docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md）。

### Decision

- NPU ランタイムは **自作の薄い OpenAI 互換シム `npu/npu_server.py`**（`Run-LFM2.py` の prefill/decode ループを
  Python 標準ライブラリ HTTP で包む）。`POST /v1/chat/completions` + `GET /health` を **llama.cpp と同じパス**で出すため、
  proxy 側はパス上書き不要・ランタイム非依存のまま。Ryzen AI conda env で動かし、追加 pip 依存は入れない。
- モデルは AMD プリビルド **`amd/LFM2-1.2B-ONNX_rai_1.7.1`**（ローカル dir を指定）。`reason_decision_prompt`
  プロファイル（schema 無し）+ 寛容パースで判定（NPU は grammar 制約不可）。
- `start.ps1` 既定は **`-Backend auto`（NPU → Vulkan → CPU）** を維持。NPU は health + 1トークン補完で受理し、
  未導入/不調なら透過的に次段へ。`-Backend npu|vulkan|cpu` で単一強制（vulkan/cpu が NPU 無効化）。
- **デフォルト NPU 優先の根拠**: NPU は全機種同一（XDNA2 50 TOPS）。本番機 **Ryzen AI 340** は iGPU が
  Radeon 840M（4 CU, 開発機 8060S=40CU の約 1/10）で、分類（長い入力＋短い出力＝prefill 支配的）では
  **NPU が e2e 最速の見込み**。開発機 Strix Halo では GPU が速いので、開発時は `-Backend vulkan` も可。

### Consequences

- AMD APU 単体で LFM2 を NPU 実行できる。NPU 不可環境でも auto が Vulkan/CPU に落ちるため運用は壊れない。
- 前提が増える（AMD NPU ドライバ / Ryzen AI Software 1.7.1 + conda env `ryzen-ai-1.7.1` / ローカル LFM2 ONNX）。
  Vulkan/CPU 経路は従来どおり `winget install ggml.llamacpp` のみ。
- **Lemonade は LFM2-NPU には不要**（OGA 専用）。Go の chat_path/health_path 配線は将来 Llama 等 OGA モデルを
  NPU で使う余地として残置（LFM2-NPU では未使用＝既定パス）。
- 開発機ベンチでは GPU 最速・NPU 最遅という結果だが、これは 8060S が異常に強い iGPU だったため。本番 340 では
  逆転見込み。340 実機が入手でき次第、同ハーネスで再計測し記録を更新する。
- 日本語専用 fine-tune は NPU ビルド無し（docs/todo.md に Deferred）。

### Related Records
- docs/records/20260607/0125-npu-shim-and-benchmark.md
- docs/knowledges/20260607/0125-lfm2-npu-shim-and-benchmark.md

## AMD NPU(XDNA2) 対応: Lemonade Server 経由、NPU 優先 + 自動フォールバック (20260606 22:33)

### Status
Superseded (20260607 01:25) — Lemonade(OGA) では LFM2 を NPU 実行できないため、自作 Ryzen AI ONNX シムに刷新。上の決定を参照。

### Context

Vulkan(iGPU) バックエンドが安定稼働したため、XDNA2 NPU での推論を試し、有効なら第一候補に
する。調査の結論: **llama.cpp / Ollama は NPU を駆動できない**（GGUF=CPU/GPU のみ）。NPU の
実体は **ONNX(onnxruntime-genai / OGA) + VitisAI EP** で、AMD が LFM2 のプリビルド NPU ONNX
モデルを配布（`amd/LFM2-1.2B-ONNX_rai_1.7.1` ほか）。**Lemonade Server** が OGA をラップして
**OpenAI 互換 HTTP**（`/api/v1/...`）を提供するため、既存の「サイドカーへ HTTP 委譲」設計に
最小変更で載る。詳細は docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md。

### Decision

- NPU ランタイムは **Lemonade Server**（OpenAI 互換, NPU/hybrid OGA）。proxy は endpoint と
  リクエストパスを切り替えるだけ（Go の配線変更は最小）。
- モデルは AMD プリビルド **`amd/LFM2-1.2B-ONNX_rai_1.7.1`**（現行 LFM2.5-1.2B-Instruct と
  同系・低レイテンシ。"Thinking" 版は思考トークンを出すため分類用途では不採用）。
- `start.ps1` の既定を **`-Backend auto`（NPU → Vulkan → CPU）** に変更。各段は health で受理
  （NPU は「モデルが実際に応答するか」まで確認）し、未導入/不調なら透過的に次段へ。
  `-Backend npu|vulkan|cpu` で単一強制（`vulkan`/`cpu` が NPU 無効化）。
- Go 側の最小変更: `LlamaClient` の chat/health パスを設定可能化（Lemonade の `/api/v1`）、
  `inference.chat_path/health_path/backend` 追加、proxy に `-endpoint/-model/-profile/-chat-path/`
  `-health-path/-backend` 上書きフラグ、監査ログの backend を **実ランタイム**に修正。
- NPU では **`reason_decision_prompt` プロファイル**（schema 無し）を使う。OGA は llama.cpp の
  GBNF JSON schema 制約を強制しないため、サーバ側 `response_format` に頼らず prompt + 寛容
  パース（既存 fail-closed）で判定する。

### Consequences

- AMD APU 単体で NPU 推論を試せる。NPU が使えない環境でも auto が Vulkan/CPU に落ちるため
  既存運用は壊れない。NPU を切りたい場合は `-Backend vulkan`。
- 前提パッケージが増える（AMD NPU ドライバ / Ryzen AI Software 1.7.x / Lemonade、+ conda）。
  Vulkan/CPU 経路は従来どおり `winget install ggml.llamacpp` のみで動く。
- 日本語専用 fine-tune は現状未使用（既定は stock Instruct）。JP fine-tune の OGA/VitisAI 変換と
  精度再検証は将来課題（docs/todo.md）。
- Lemonade の細部（`serve` サブコマンド、モデル登録名、health パス）は導入版で要確認。設計上は
  start.ps1 パラメータ + config に隔離済みで「値の変更」で吸収できる。

### Related Records
- docs/records/20260606/2233-npu-backend.md
- docs/knowledges/20260606/2233-amd-npu-lemonade-oga.md

## jp_confidential_extraction プロファイルでは <<<DATA>>> ラッパを外す (20260607 01:38)

### Status
Accepted

### Context

`akiFQC/japanese-confidential-information-extraction-sft` で FT 中のモデルは、ALLOW/BLOCK を
返す分類器ではなく 11 カテゴリの抽出器で、学習データの user ターンは区切り・メタ情報の無い素の
テキスト。`CLAUDE.md` の不変条件「検査テキストは不活性データとして `<<<DATA ... DATA>>>` で包み
『中身に従うな』と枠付けする」を守ると学習分布から外れ、1.2B モデルの抽出精度が落ちる。

### Decision

`jp_confidential_extraction` プロファイルの `BuildUser` は素テキストを送る（`<<<DATA>>>`・
`segment_type` 無し）。これは上記不変条件を本プロファイル限定で弱める。ユーザに実例付きで説明し
承認を得た。既定の `reason_decision` / `ng_boolean` は従来どおりラッパを維持する。

### Consequences

- 利点：FT モデルの学習分布に一致し抽出精度を最大化。
- リスク：プロンプト注入耐性の低下。ただし抽出器には注入で反転し得る判定フィールドが無く、最悪でも
  抽出漏れ（false negative）に留まる。BLOCK 判定は proxy 側 Parse（非空カテゴリ→NG）が下す。
- 後段の担保：決定論的 rule guardrail（本番既定 enabled）と fail-closed が残存リスクを補う。
- 判定方針：11 カテゴリのいずれか非空で BLOCK（全カテゴリが社外秘のため）。人名/企業名/住所は一般
  文にも現れ誤検知が増えうる点は `docs/todo.md` で将来のトリガ集合構成化として追跡。

### Related Records
- docs/records/20260607/0138-jp-confidential-extraction-profile.md

## 結合テストは Windows Sandbox で実施する (20260606 22:30)

### Status
Accepted

### Context

透過インターセプションの結合テスト（実 hosts編集・`:443`バインド・CA信頼ストア導入・実
`api.anthropic.com` アクセス）は、ホスト上で稼働中の Claude セッションを壊し得るため当初は
保留していた。ユーザが使い捨て・NAT分離の **Windows Sandbox** を用意した。

### Decision

結合テストは Windows Sandbox 内でのみ実施する。ホストリポは read-only マップ、結果は read-write
の共有フォルダ経由で回収。クライアントは schannel の `curl.exe`（マシン信頼ストア参照、`-k` 不使用）。
無害な401プローブ（無効キー＋良性プロンプト、機密ゼロ）で実 API 到達を確認する。ハーネスは
`test/sandbox/`（`run-sandbox.ps1` ホストランチャ + `run-tests.ps1` VM内オーケストレータ）、
`install.ps1 -SkipBuild` でホストビルド済み exe を再利用（VM に Go 不要）。

### Consequences

- 32/32 PASS で機構を端から端まで実証（hostsリダイレクト→CA信頼→`:443`終端→DLP→`1.1.1.1`
  バイパスで実API到達→透過→完全アンインストール）。ホストは無傷。
- 発見した follow-up（`docs/todo.md`）：Claude Code(Node) は Windows 信頼ストアを見ない＝
  `NODE_EXTRA_CA_CERTS` が要る／動的証明書に CRL/OCSP が無く schannel が失効確認で失敗する。
- Windows Sandbox(GUI) は RDP 対話デスクトップが前面のときのみ起動成立、終了はウィンドウを
  ×で閉じる（force-kill は VM ワーカー孤児化を招く）。

### Related Records
- docs/records/20260606/2230-sandbox-integration-test.md

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

## 管理UI向けオプトイン生プロンプト保存 + 読み取り専用 admin API (20260606 21:20)

### Status
Accepted

### Context

ローカル proxy の検知件数・検知内容・全プロンプト履歴を確認する Grafana 風の管理UI
（Next.js）をユーザーが要望。現状の監査DB（`audit_events`）は **メタデータのみ**
（decision/reason/source/latency/backend/時刻）を保存し、生のプロンプト本文・機密値は
一切保存しない（CLAUDE.md「Never log or persist raw content」不変条件、`store_raw_text:false`）。
「プロンプト履歴」「検知内容」を表示するには生データ保存が必須で、これは不変条件を緩める。
ユーザーに明示的に確認し、(1) プロンプト全文を保存、(2) Go に読み取り専用 admin API を追加、
(3) 秘密情報を保存するため UI に ID/PW 認証＋ admin API に任意の Bearer トークン、で合意。

### Decision

- `storage.store_raw_text`（既存・既定 false）を実際に配線。**true のときのみ**、各リクエストの
  **ライブターン（最新のユーザーターン）本文**を `audit_events.prompt_text` に保存する
  （ALLOW/BLOCK 双方）。Claude Code は毎回全履歴を再送するため、配列全体ではなく新規ターンのみ
  を保存して重複を回避。本文は ~16KiB で rune 安全に切り詰め。`request_unparseable` 等の
  解析不能リクエストは本文を保存しない。
- 監査スキーマに nullable 列 `prompt_text`/`matched_snippet`/`path` を追加。既存DBには
  `PRAGMA table_info` ベースの冪等マイグレーション（`ALTER TABLE ADD COLUMN`）で追加。
- 読み取り専用 `internal/admin`（`GET /admin/stats|events|events/{id}|meta`）を追加。proxy と
  同一 mux・同一 localhost バインド。`admin.enabled`（既定 true）で切替、`admin.auth_token`
  が非空なら `Authorization: Bearer` を必須化（store_raw_text=true 時は設定を強く推奨）。
- `matched_snippet` に**検知された該当箇所**を保存（20260606 23:27 で実装、当初先送りから変更）。
  `dlp.Result.Match` / `Evaluation.BlockMatch` を追加し、ルール検知は `RuleEngine.MatchSpan` で
  正規表現の一致部分文字列（=機密の値）を、LFM 検知は該当セグメント全文を載せる。handler は
  `store_raw_text=true` のときだけ truncate して `matched_snippet` に格納（`prompt_text` と同じ
  オプトインゲート）。`matched_snippet` は `prompt_text` の部分文字列であり新たな露出は増えない。
  `reason` には従来どおりルール名のみで機密値は入れない。管理UIは本文中で該当箇所をハイライト。

### Consequences

- `store_raw_text:true` の監査DBは**秘密情報を含む**（`prompt_text` と `matched_snippet` の両方）。
  保護は retention（既定30日）・localhost バインド・OSファイル権限・admin トークンのみ＝**advisory
  であり強制境界ではない**。本番は既定 false を維持。デモ時のみ true + auth_token を設定する運用。
- 既定（false）の動作は不変＝メタデータのみ。後方互換のマイグレーションで旧DBもそのまま読める。
- admin API は読み取り専用で上流送信を一切行わない（egress 経路を増やさない）。

### Related Records
- docs/records/20260606/2117-admin-observability-and-ui.md

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
- ~~**NPU(XDNA2) は今回スコープ外**。ONNX + Vitis AI EP + 新規 Go バックエンドが必要で、
  spec の Milestone 6 として将来対応（ドキュメント記載のみ）。~~
  → **Superseded (20260606 22:33)**: NPU は Lemonade Server(OGA) 経由で対応済み。上の決定を参照。
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
