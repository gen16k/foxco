# Proxy Server

FoxCoのローカル/社内プロキシサーバ実装を置くフォルダです。

## Goal

ユーザーがChatGPT / Claude / Gemini / OpenAI互換APIへ送る前のプロンプトを受け取り、LFM Guardとルールエンジンで判定してから、Block / Mask / Local Answer / Allowを実行します。

## Planned Components

- `src/`: APIサーバ、判定フロー、マスキング、クラウドLLM転送処理
- `config/`: ポリシー、企業辞書、検知ルール、デモ用設定
- `web/`: 管理画面、デモ画面、検知ログビュー

## MVP API Shape

OpenAI互換APIを最初の入口にします。

- `POST /v1/chat/completions`: プロンプトを検査し、安全な場合のみ上流LLMへ転送
- `POST /guard/analyze`: LFM Guardの判定だけを返すデバッグ/評価用API
- `GET /health`: デモ時の起動確認

## Guard Decision

判定結果はまず以下のJSONを目標にします。

```json
{
  "action": "block",
  "risk_score": 0.92,
  "entities": [
    {
      "type": "confidential_project",
      "text": "Project AKITA",
      "reason": "Unreleased internal project name"
    }
  ],
  "reason": "取引先名と未公開プロジェクト名が同時に含まれています。",
  "safe_prompt": "特定の取引先名や未公開プロジェクト名を伏せて、一般化した提案文を作成してください。"
}
```

## First Tasks

- OpenAI互換の最小プロキシを作る
- ルールベース検知を先に実装する
- LFM Guard呼び出し部分を差し替え可能なインターフェースにする
- デモ用のBlock/Mask/Allow画面を作る
