# Grafana 風 管理ダッシュボード（Next.js）実装 + E2E 検証 (20260606 21:52) #pending

## Motivation

オプトイン生プロンプト保存 + 読み取り専用 admin API（docs/records/20260606/2117-admin-observability-and-ui.md）
の上に、検知件数・検知内容・全プロンプト履歴を確認できる Grafana 風のインタラクティブな管理UIを
構築する。秘密情報を保存するため ID/PW 認証付き。

## Goal

`proxy-server/web/` に Next.js ダッシュボードを実装し、BFF 経由で admin API を消費、ID/PW ログインで保護。
プロキシ→admin API→BFF→UI の全経路をローカルで E2E 検証。

## Records

### スタック
- Next.js 14（App Router/TS）+ Tailwind 3 + Recharts 2 + SWR 2 + iron-session 8 + zod 3。
  Node 24 上での peer-dep 安定性を優先し、Tremor は使わず Recharts 直 + 自前の Grafana 風コンポーネント。

### 構成
- `lib/schemas.ts`(zod=契約の真実) / `lib/proxy-client.ts`(server-side fetch・任意 Bearer・エラー分類) /
  `lib/swr.ts`(useStats/useEvents/useEventDetail/useMeta、相対レンジは preset でキー化し fetch 時に now 再解決) /
  `lib/time-range.ts`(preset/custom・RFC3339 整形) / `lib/use-dashboard-params.ts`(URL params 集約) / `lib/format.ts`。
- BFF: `app/api/admin/{stats,events,events/[id],meta}/route.ts`（Go admin API を server-side で代理。CORS 不要）。
- 認証: `lib/session.ts` + `app/api/auth/{login,logout}` + `middleware.ts`（全ページ + /api/admin/* を保護、
  未認証はページ→/login リダイレクト・API→401）+ `app/login/page.tsx`。
- シェル: `components/shell/`（TopBar / TimeRangePicker 15m〜30d+Custom / RefreshControl Off〜1m + 手動 /
  ProxyStatus(meta) / NavTabs）。`RefreshContext` でポーリング間隔を共有。`(dash)` ルートグループに集約。
- パネル/チャート: KpiRow(7 KPI) / AllowBlockArea(stacked area) / SourceDonut(スライス click→/history?source=) /
  TopReasonsBars(行 click→/history?q=)。
- 履歴: EventFilters(decision/source/q デバウンス/limit) + EventsTable(サーバページング) +
  EventDetailDrawer(?event=id でディープリンク、prompt 全文 + null 時は store_raw_text=false 通知)。
- 空/エラー状態: 空DB→ゼロ表示、プロキシ停止→「127.0.0.1:8787 に接続できません」。
- `web/README.md`、`.env.example`、root `.gitignore` に web 成果物（node_modules/.next/.env.local 等）を追加。

### ビルド
- `npm run typecheck` / `npm run build` 成功（6 ルート、middleware 30.5kB）。Go ツールは web/ を無視（.go 無し）。

## Results（E2E 検証、すべて成功）

- 検証構成: `config/config.demo.yaml`（keyword 分類器 / store_raw_text=true / admin.auth_token=foxco-demo-token /
  upstream=ローカルモック 127.0.0.1:9999 / 隔離DB demo-state/dlp.db）。実 Anthropic API には一切送信せず。
- 既定14リクエスト（ALLOW 8 / BLOCK 6）投入 → 新規DBへマイグレーション後に記録。
- `GET /admin/stats`（Bearer）: total=14, blocked=6, allowed=8, blockRate≈0.43, bySource={rule:3,lfm:3},
  topReasons/avg/p95/series 正常。トークン無し → 401。
- `GET /admin/events?decision=BLOCK`: promptText に投入プロンプトが保存（store_raw_text 動作確認）、
  source rule/lfm、path で /v1/messages と count_tokens を区別。
- UI 経路: `/api/auth/login`(admin/foxco)→200・cookie 発行 → 認証付き `/api/admin/stats` が同一データ(total=14)を返す
  （BFF→Go→token 全経路）。未認証 `/api/admin/stats`→401、未認証 `/`→307 /login、誤資格情報→401。
- バックグラウンド(モック/proxy/dev)停止、ポート 8787/3939/9999 解放を確認。

## Refs
- docs/records/20260606/2117-admin-observability-and-ui.md
- docs/decisions.md（オプトイン生プロンプト保存 + admin API）
- proxy-server/web/README.md
