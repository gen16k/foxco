# 管理UI: 全体のフォントサイズを底上げ (20260607 01:16) #f4f3350

## Motivation

ユーザーから「全体的にフォントが小さすぎる」とのフィードバック。Grafana 風で密度重視のため
`text-xs`(12px)/`text-sm`(14px) を多用し、一部に `text-[10px]`/`text-[11px]` の極小ラベルがあり、
通読負荷が高かった。

## Goal

UI 全体のフォントを ~1–2px 底上げし、極小の任意指定サイズも同じスケールで拡大して統一する。
コンポーネントを個別に書き換えず、タイポグラフィを一元管理する。

## Records

- `web/tailwind.config.ts`: `theme.extend.fontSize` で既定スケールを上書き。
  - `2xs` 12px（新規・旧 `text-[10px]`/`text-[11px]` の置換先）
  - `xs` 13px（旧12）, `sm` 15px（旧14）, `base` 17px（旧16）, `lg` 19px（旧18）。各 line-height 付与。
- 任意指定の極小サイズを `text-2xs` に統一（7ファイル: SourceDonut / DecisionBadge /
  LiveDetectionsPanel / EventDetailDrawer / ToastViewport / StatCard / TimeRangePicker）。
  これにより極小ラベルもスケール管理下に入り、10/11px の不統一を解消。

## Results

- `npm run build` パス（Tailwind が新スケールでCSS生成、`text-[1[01]px]` の残存なし）。
- 反映範囲: `text-xs`(25) / `text-sm`(23) / `text-lg`(3) と極小ラベル(15) が一括で拡大。
  `base` も17pxに（body継承テキスト向け）。さらに調整したい場合は config の数値だけで増減可能。

## Refs
- docs/records/20260607/0053-live-detection-alert.md
- web/tailwind.config.ts
