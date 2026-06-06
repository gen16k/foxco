# Training

LFM Guardの学習データ、ファインチューニング、評価、モデル成果物を置くフォルダです。

## Goal

LFM2.5-1.2B-JP-202606を、単純なPII抽出だけでなく「日本語の社内文脈に基づく送信前リスク判定」ができるGuardモデルとして調整します。

## Planned Components

- `data/`: 学習/検証/デモ用データ
- `scripts/`: データ生成、整形、学習、推論、評価スクリプト
- `evals/`: 評価セット、評価レポート、比較結果
- `models/`: ローカルで使うモデル設定、LoRA/checkpoint参照、量子化モデルのメモ

## Label Design

まずは以下のアクション分類を狙います。

- `allow`: 送信してよい
- `block`: 送信を止める
- `mask`: マスクして送る
- `local_answer`: クラウドへ送らずローカル回答する

追加で返したい情報:

- `risk_score`: 0.0から1.0
- `entity_types`: PII / credential / confidential_project / customer / source_code / financial / medicalなど
- `reason`: ユーザー向けの短い説明
- `safe_prompt`: マスキングまたは一般化した安全なプロンプト

## Comparison Targets

デモで差分を見せるため、以下を比較対象にします。

- Regex only
- `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
- Base LFM2.5-1.2B-JP-202606
- Fine-tuned FoxCo LFM Guard

## First Tasks

- 疑似社内データと危険プロンプトを作る
- `allow/block/mask/local_answer` の小さな評価セットを作る
- 既存PIIモデルで拾えるケース/拾えないケースを整理する
- p50/p95レイテンシ、Precision/Recall、クラウド送信token削減率を測る準備をする
