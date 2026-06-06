# 管理UI: ライブ検知アラート（トースト＋常設パネル＋オプトイン音） (20260607 00:53) #pending

## Motivation

デモで「いま問題のあるプロンプトが検知された」ことを分かりやすく示したい。既存の管理UIは事後閲覧
専用で、リアルタイムの気づきが無い。フルスクリーン明滅のような“おもちゃっぽさ”は避け、OSS監視
ダッシュボードで標準的な表現＝右上トースト＋常設「直近検知」フィードで新規BLOCKを即時提示する。

ユーザー選択: 表示は「トースト＋常設 Live detections パネル」、通知音は「オプトイン（既定OFF）」。

## Goal

- 新規BLOCK検知時に右上トーストをポップ（検知内容表示・クリックで詳細ドロワー）。全ページで動作。
- Overview に常設「Live detections」フィード（直近ブロック・自動更新・新着ハイライト）。
- 通知音は TopBar の 🔔/🔕 トグル（既定OFF・localStorage 永続・WebAudioビープ）。
- バックエンド変更なし（既存の読み取り専用 `/api/admin/events` を再利用）。

## Records

- `lib/swr.ts`: `useRecentBlocks(limit=20)` 追加。**固定5sの常時ポーリング**（RefreshContextの手動更新
  間隔・時間範囲とは独立＝自動更新OFFでもライブ）。`/api/admin/events?decision=BLOCK&limit=20`。
  ウォッチャとパネルでSWRキー共有＝リクエストは1本に集約。
- `lib/format.ts`: 相対時刻 `fmtRelative(iso)`（just now / Xs / Xm / Xh / Xd）追加。
- `components/notify/NotificationContext.tsx`: `NotificationProvider` ＋ `useNotifications`。
  トースト配列（最大8保持）、`notify(event)` / `dismiss(id)`。
- `components/notify/LiveDetectionWatcher.tsx`（描画なし）: `useRecentBlocks` を購読し、`eventId` 差分で
  新規BLOCKを検出 → `notify`。**初回フェッチはベースライン化**（過去BLOCKでトーストが溢れない）。
  `seen` は毎tick現在ページから再構築＝重複防止＋メモリ有界。音ONなら `playBeep`。
- `components/notify/ToastViewport.tsx`: 右上 `z-40` 積み重ね。最大4表示＋超過分は「＋N 件」に集約。
  各トースト=赤系（`border-block`）、reason/source/該当スパン/相対時刻。`animate-toast-in` 入場、
  ~8sで自動消滅（ホバーで一時停止）、クリックで `/history?event=<id>`（既存ドロワー再利用）、✕/Escで消去。
- `components/dashboard/LiveDetectionsPanel.tsx`: Recent events と**同じ `EventsTable`（compact）**で直近8件の
  BLOCK を表示し、認知負荷を下げる。検知文字列（matched snippet）はここには出さず、行クリックで開く詳細
  ドロワー（と Recent events の Body 表示）に集約。ヘッダに `animate-ping` のライブドット、`PanelState` で
  ローディング/空/エラー処理。`OverviewClient` の KPI 行直下に配置。
- `lib/sound.ts`: WebAudio の2トーン・低音量ビープ（音声ファイル不要）。`primeAudio`（ユーザー操作内で
  AudioContext を resume）と `playBeep`。
- `components/shell/SoundContext.tsx` / `SoundToggle.tsx`: 既定OFF・localStorage（`foxco.sound`）永続。
  TopBar にトグル配置。ウォッチャは同 context を見てビープ可否を判定。
- `app/(dash)/layout.tsx`: `RefreshProvider > SoundProvider > NotificationProvider` で入れ子にし、
  `LiveDetectionWatcher` と `ToastViewport` をマウント（dash配下のみ＝ログイン画面では動かない）。
- `tailwind.config.ts`: keyframes/animation `toast-in` を追加。ライブドットは既定 `animate-ping`。
  `prefers-reduced-motion` を尊重（`motion-reduce:animate-none` / `motion-reduce:hidden` 併用）。
- `lib/mock.ts`: **合成ライブBLOCK** を追加。id は20s毎にロールする `mock_live_<floor(now/20000)>`、
  reason/snippet は `BLOCK_KINDS` を巡回。`mockEvents` が BLOCK/無フィルタ時に先頭付与、`mockEvent` が
  `mock_live_*` を解決。`mockStats` には入れない（KPI数値のちらつき回避＝アラート実演が目的）。

## Results（検証）

- `npm run typecheck` / `npm run build` パス。
- `USE_MOCK=1` ライブ確認: `/api/admin/events?decision=BLOCK` の先頭 id が `mock_live_89038057` →（21s後）
  `mock_live_89038058` に更新（新規一意id・reason/snippet も巡回）。`/api/admin/events/mock_live_<bucket>`
  が 200 で解決。→ ウォッチャが約20秒ごとに新規検知としてトースト発火、パネルが新着行をフラッシュ、
  ON時はビープ。20s窓内の再ポーリング（5s毎）では id 不変＝通知されない（氾濫しない）。
- セキュリティ: 既存の読み取り専用APIの再利用のみ。トースト/パネルの該当スパン表示は既存ドロワーと同等
  （`store_raw_text=true` のときのみ値が存在）。不変条件に変更なし。

## Refs
- docs/records/20260606/2327-matched-snippet-highlight.md
- docs/records/20260606/2218-admin-ui-mock-mode.md
- web/components/notify/LiveDetectionWatcher.tsx
- web/components/dashboard/LiveDetectionsPanel.tsx
