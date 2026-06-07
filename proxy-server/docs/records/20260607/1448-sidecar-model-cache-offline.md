# 起動時のモデルキャッシュ有効化（--offline 自動判定） (20260607 14:48) #pending

## Motivation

`start.ps1` が `llama-server` を `-hf <ref>` で起動するため、毎回 HuggingFace へ
manifest/etag 確認に行き、再ダウンロード相当の遅延が出ていた。モデルは既に
ローカルにキャッシュ済み（`~/.cache/huggingface/hub`、~697MB/モデル）。

## Goal

キャッシュ済みなら起動時に再ダウンロード/ネットワーク確認をせずキャッシュ直読み
で高速起動する。ただし他マシン/初回など未キャッシュ環境では従来どおりダウンロード
できること（一律 `--offline` は初回が失敗するため不可）。

## Records

- 現状確認: `llama-server --cache-list` で対象3モデルがキャッシュ済みと確認。
  実体は `%LOCALAPPDATA%\llama.cpp` ではなく HF hub レイアウト
  `~/.cache/huggingface/hub/` にあった（llama.cpp 9538 / 5343f4502）。
- `llama-server --offline`（env `LLAMA_ARG_OFFLINE`）= キャッシュ強制・ネットワーク
  遮断、が利用可能。
- `start.ps1` 変更:
  - `-Offline {auto|on|off}` パラメータ追加（既定 `auto`）。
  - `Test-LlamaModelCached`: `--cache-list` 出力に model ref が含まれるかで判定
    （llama.cpp 実際の cache dir に追随）。
  - サイドカー起動時、`auto` かつ `-hf` ref かつキャッシュ済みなら `--offline` 付与。
    未キャッシュ/ローカル `-m` パスでは付与しない。`on`/`off` は明示上書き。
  - ヘッダーの使い方コメントを更新。

## Results

- 構文チェック OK。
- 実機検証: キャッシュ済み `LiquidAI/LFM2.5-1.2B-Instruct-GGUF:Q4_K_M` を `--offline`
  付きで起動 → ネットワーク無しで `model loaded`、`/health` 200 を確認。
- 判定関数: キャッシュ済み2モデル→True、存在しない ref→False（=ダウンロード経路）。
- Go コードは未変更（.ps1 のみ）。

## Refs
- start.ps1
