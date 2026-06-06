# Data

学習、検証、デモに使うデータを置きます。

本物の個人情報や実在の社外秘情報は置かず、デモ用の疑似データを使います。

## Candidate Files

- `train.jsonl`: 学習用
- `valid.jsonl`: 検証用
- `demo_prompts.jsonl`: 5分デモ用の代表プロンプト
- `company_dictionary.json`: 疑似企業辞書、プロジェクト名、取引先名、秘密ルール

## JSONL Draft

```json
{"prompt":"A社向けにProject AKITAのソースコードを使った提案文を作って","action":"block","entity_types":["customer","confidential_project","source_code"],"reason":"取引先名、未公開プロジェクト名、ソースコードが同時に含まれています。"}
```
