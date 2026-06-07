# Network Flow タブ（NIRVANA 風 3D パケット可視化）の追加 (20260607 12:53) #pending

## Motivation

管理 UI (`proxy-server/web`) は Overview / Prompt History の 2 タブのみで、いずれも
テーブル・集計チャートの静的表示だった。ユーザーから「NIRVANA のように、
クライアント → PromptGate → Claude を縦に並べたネットワークトポロジー図でパケットの
流れを可視化したい。通常は緑のパケットがクライアントから Claude へ流れ、個人情報等を
検知してブロックした場合は赤のパケットが PromptGate で弾かれるアニメーションにしたい」
という要望。DLP プロキシの「何を通し・何で止めたか」を直感的かつデモ映えする形で見せる。

## Goal

- `/network` ルートに「Network Flow」タブを追加。
- 3D（react-three-fiber / WebGL）で 3 ノード（上 Claude / 中 PromptGate / 下 Client）を
  縦に配置し、リンクで接続。緑パケット=Claude へ到達、赤パケット=PromptGate で炸裂し
  Claude へ届かない、を発光（bloom）付きでアニメーション。
- データは実イベント（`/api/admin/events` を 3s ポーリング）+ アイドル時のデモ補完。
- ローカル専用・低頻度・上限付き・遅延ロードで負荷を抑える。WebGL 不可時は擬似 3D の
  静的フォールバックへ degrade。

## Records

- 計画は plan ファイル参照。ユーザー確認事項: 描画=react-three-fiber（本格3D）、
  データ=実イベント+デモ補完、タブ名=Network Flow。「重ければ擬似3Dでも可」との追加指示
  も受け、データ/オーケストレーション層をレンダラ非依存にし degrade 可能な構成にした。
- 依存追加（`web/package.json`、exact pin）: `three@0.169.0`,
  `@react-three/fiber@8.18.0`（React 18 系の v8 必須）, `@react-three/drei@9.122.0`,
  `@react-three/postprocessing@2.19.1`, dev `@types/three@0.169.0`。
- 新規 lib（レンダラ非依存）: `lib/topology-packets.ts`（Packet 型 / ref ベース store /
  `NODE_Y` 定数 / `MAX_PACKETS=120` / 各 duration / `PacketCounters`）、
  `lib/topology-demo.ts`（緑80%・赤20%のデモ生成）、`lib/webgl.ts`（`hasWebGL()` SSR セーフ）。
- データ: `lib/swr.ts` に `useRecentEvents(30)`（全 decision・3s 固定ポーリング）を追加。
- 3D シーン `components/topology/`: `Nodes`（発光メッシュ + HTML ラベル。ゲートは
  blockで赤パルス）、`Links`（drei Line + additive conduit）、`Packets`（単一
  `instancedMesh` で全パケット描画、`useFrame` で行列/色更新・赤の炸裂・完了 sweep、
  毎フレーム setState ゼロ）、`Effects`（Bloom mipmapBlur）、`CameraRig`（OrbitControls
  既定 autoRotate、reduced-motion で停止）、`TopologyScene`（Canvas、hidden 時
  `frameloop="never"`）、`StaticFallback`（CSS perspective 擬似3D + CSS アニメ）。
- タブ本体 `components/dashboard/NetworkFlowClient.tsx`: `useRecentEvents` 購読、
  seen-set 差分でのパケット生成（初回はシードのみ、履歴を再生しない＝`LiveDetectionWatcher`
  踏襲）、デモ補完 interval、reduced-motion / `visibilitychange` 監視、`hasWebGL` 判定、
  error boundary で `StaticFallback` へ degrade、`next/dynamic({ssr:false})` で Scene を
  クライアント限定遅延ロード、カウンタ/凡例 DOM 表示、proxy 到達不可時は
  「proxy offline — demo data」バッジを出しつつデモ継続。
- ルート `app/(dash)/network/page.tsx`（server component + Suspense）、
  `components/shell/NavTabs.tsx` にタブ 1 行追加。

## Results

- `npm run typecheck`（tsc --noEmit）: **PASS**（`NODE_Y` の `as const` リテラル型対策で
  `Packets` の `y` を `number` 注釈、それ以外は無修正で通過）。
- `npm run build`（next build）: **PASS**。`transpilePackages` は**不要**だった
  （Next14 + R3F v8 + three0.16x、追加設定なしでコンパイル成功）。`/network` の
  First Load JS は 104kB で `/history`(106kB) とほぼ同等 ＝ three.js は `dynamic(ssr:false)`
  により別チャンクへ遅延ロードされ初期バンドルに含まれない（設計通り）。
- dev サーバ（`USE_MOCK=1`, port 3940）スモーク: ログイン200 → `GET /network` 200
  （"Network Flow" 描画）→ `GET /api/admin/events?limit=30` 200 で 30件・BLOCK/ALLOW 両方
  あり（＝緑・赤両方のパケットが流れる）。
- `npm run lint`: 本リポジトリは ESLint 設定ファイルが**未コミット**（`lint` スクリプトと
  `eslint-config-next` 依存はあるが `.eslintrc*` が無く `next lint` が対話セットアップを促す）。
  本機能で eslint 設定を新規導入するのはスコープ外（全ファイルに影響）のため lint はスキップ。
  → `docs/todo.md` に追跡エントリを追加。
- WebGL 不可時は `StaticFallback`（擬似3D）へ自動 degrade。reduced-motion / タブ非表示時の
  省電力（autoRotate/bloom 抑制、`frameloop="never"`）も実装。

## Refs
- 実プロキシでの目視確認（緑フロー・実トラフィックでの赤ブロック）は未実施。
  `.\start.ps1 -Classifier keyword` で proxy を立て、Claude Code を通し、良性プロンプト=緑、
  `AKIAIOSFODNN7EXAMPLE` 等を含むプロンプト=赤ブロック、を 3s ポーリング内に確認すること。
- docs/decisions.md（Network Flow 用 3D 依存採用の判断）
- docs/todo.md（admin UI の ESLint 未設定）
