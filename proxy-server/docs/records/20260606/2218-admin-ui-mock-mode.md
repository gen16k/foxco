# 管理UI モックモード（プロキシ無しでコンソール確認） (20260606 22:18) #a13fe3d

## Motivation

ユーザー要望: プロキシを起動せずに（＝ Claude Code セッションに影響を与えずに）管理コンソールの
見た目・動作を確認したい。プロキシは `ANTHROPIC_BASE_URL` が指していない限りセッションに影響しないが、
UI プレビュー専用にバックエンド不要のモックがあると安全・手軽。

## Goal

`USE_MOCK=1` のとき、BFF がリアル風のフェイクデータを返し、プロキシ無し・DB 無しで全パネルが表示できる。
モックと実データが混同されないよう明示表示する。

## Records

- `web/lib/mock.ts`（新規）: `mockEnabled()`（`USE_MOCK==="1"`）、`mockStats/mockEvents/mockEvent/mockMeta`。
  直近24hに広がる決定的な80件（約30%がBLOCK、source は rule/lfm/classifier_unavailable）。プロンプトは
  マスク済みプレースホルダ（実機密なし）。from/to/decision/source/q フィルタ・ページング・hour/day バケットの
  series 集計を Go 実装と同等に再現。meta は backend="mock", listenAddr="mock (no proxy)"。
- BFF 4 ルート（stats/events/events/[id]/meta）: 先頭で `mockEnabled()` なら mock を返す（実プロキシは叩かない）。
- `components/shell/ProxyStatus.tsx`: backend==="mock" のとき amber の「MOCK DATA」バッジを表示。
- `components/common/states.tsx`: プロキシ未接続エラーに「USE_MOCK=1 でモック表示可」のヒント追加。
- `web/.env.example` / `web/README.md`: モックモードの使い方を記載（`$env:USE_MOCK="1"; npm run dev` か
  `.env.local` に `USE_MOCK=1`）。プロキシ単体起動はセッションに影響しない旨も明記。

## Results（検証）

- `npm run typecheck` / `npm run build` 成功。
- `USE_MOCK=1` で `npm run dev` 起動（プロキシ非起動、8787 未待受）→ ログイン200 →
  `/admin/meta` backend=mock / listenAddr="mock (no proxy)"、`/admin/stats?range=24h` total=80 blocked=24
  allowed=56 series=25 sources=rule,lfm,classifier_unavailable、`/admin/events?decision=BLOCK` total=24・
  マスク済みプロンプト返却を確認。プロキシ不要でコンソール全機能をプレビュー可能。

## Refs
- docs/records/20260606/2152-web-admin-dashboard.md
- web/lib/mock.ts
