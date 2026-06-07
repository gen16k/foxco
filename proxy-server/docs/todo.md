# TODO / Deferred Issues

## 配備チェックポイントの実学習システムプロンプトを厳密確認する

- Status: Open
- Discovered: 20260607 (docs/records/20260607/1224-switch-conf-extract-model.md)

### Detail

`jp_confidential_extraction` の system プロンプトは akiFQC の `-sft` 系統由来の byte-exact 値を
流用している。だが切替先の `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract` / `LFM2-350M-Conf-Extract-Japanese`
のモデルカードは TRL/SFT スタブで、実学習プロンプト・出力フォーマットを公開していない。小型モデルは
全角/半角・改行差に敏感で、プロンプトがズレると抽出精度（ひいてはブロック判定）が落ちる。

### Why deferred / Blocked by

GGUF 変換後に実機で日本語サンプルを流し、11キー JSON が安定して出るかで間接確認する段階。ズレが
判明した場合は `inference.system_prompt_file` で実プロンプトを byte-exact 固定する（再ビルド不要）。
可能なら akiFQC の学習データセットから正準プロンプトを取得して built-in を更新する。

### Unblock condition

実チェックポイントの正準 system プロンプトを入手し、built-in と一致を確認（または固定ファイルで上書き）。

## GGUF リポジトリ公開後は -hf 直 DL へ切替える

- Status: Resolved
- Discovered: 20260607 (docs/records/20260607/1224-switch-conf-extract-model.md)
- Resolved: 20260607 (docs/records/20260607/1320-use-upstream-gguf.md)

### Detail

当初は対象モデルが safetensors のみのため `scripts/convert-model-gguf.ps1` でローカル変換し、
`start.ps1 -Model <local.gguf>` で読み込む運用だった。上流 `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF`
（Q4_K_M / Q8_0 / F16 / BF16）が公開されたため、既定を `start.ps1 -Model
akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract-GGUF:Q4_K_M`（`-hf` 自動DL）に切替えた。変換スクリプトは
GGUF 未公開のチェックポイント（例: 350M）向けフォールバックとして存置。

## 抽出プロファイルのブロック対象カテゴリを構成可能にする

- Status: Open
- Discovered: 20260607 (docs/records/20260607/1224-switch-conf-extract-model.md)

### Detail

`jp_confidential_extraction` は 11 カテゴリのいずれか非空で BLOCK する faithful な既定。だが
`human_name` / `company_name` / `address` は一般的なエンジニアリング文・公開情報にも現れ、過剰ブロックが
増えうる。現状トリガ集合は `profile.go` の定数スライス `jpExtractionSensitiveCategories` で構成不可。

### Why deferred / Blocked by

実運用の誤検知傾向が未測定。まず faithful 既定で配備し、観測後に `inference.extraction_block_categories`
（既定=全11）でサブセット指定できるようにするのが妥当。

## プロファイル切替時の指紋キャッシュ陳腐化

- Status: Open
- Discovered: 20260607 (docs/records/20260607/1224-switch-conf-extract-model.md)

### Detail

`cache.persist_sqlite=true` でプロファイル（モデル）を跨いで再起動すると、指紋→判定キャッシュが旧
プロファイルの判定を返し得る（プロファイル名をキャッシュキー/名前空間に含めていない）。既定は
`persist_sqlite=false`（プロセス内のみ）のため通常は無害だが、モデル切替運用では留意。本変更では未対応。
