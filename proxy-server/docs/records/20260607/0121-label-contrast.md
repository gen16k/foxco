# 管理UI: ラベルのコントラスト底上げ（薄くて読みにくい） (20260607 01:21) #pending

## Motivation

ユーザーから「Total requests / Blocked などのラベルが薄くかつ小さくて見にくい」「他の同種の
箇所も同じ」とのフィードバック。Grafana 風で二次テキストに暗いグレー（zinc-500/600）を多用して
おり、ダーク背景でコントラストが不足していた。

## Goal

KPI ラベルをはっきり読めるようにし、UI 全体の弱コントラストな二次テキストを一段引き上げる。
階層（一次=値 / 二次=ラベル / 三次=補助）は保つ。

## Records

- `components/panels/StatCard.tsx`: KPI ラベルを `text-xs text-zinc-500` →
  `text-sm font-medium text-zinc-300` に（サイズ＋太さ＋明度を上げる）。hint は `text-zinc-600`→
  `text-zinc-400`（後述の一括引き上げで反映）。
- 全体の二次テキスト階層を1段引き上げ（app + components の .tsx を一括置換）:
  `text-zinc-500`→`text-zinc-400`、`text-zinc-600`→`text-zinc-500`。
  パターンが別リテラルのため1パスで安全（500→400 が先、600→500 が後）。
  結果: zinc-300=10（強ラベル）/ zinc-400=39（二次）/ zinc-500=3（最も淡い補助のみ）。

## Results

- `npm run build` パス。`text-zinc-600` は0、`text-zinc-500` は補助3箇所のみ。
- KPI ラベル（Total requests / Blocked / Block rate / Allowed / Avg latency / p95 / Upstream egress）が
  15px・medium・zinc-300 で明瞭化。パネル副題・詳細ドロワーの項目ラベル・各種キャプションも明るく。
  さらに調整したい場合は色トークンの置換のみで増減可能。

## Refs
- docs/records/20260607/0116-bump-type-scale.md
- web/components/panels/StatCard.tsx
