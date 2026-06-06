# Training

This folder contains PromptGate training, evaluation, and model comparison assets.

PromptGate uses `LFM2.5-1.2B-JP-202606` as a local guard model that detects sensitive information before a prompt is sent to a cloud LLM.

## Dataset

- Hugging Face: https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft
- Checked: 2026-06-06 JST
- Splits: train 38,852 rows, validation 2,045 rows
- Format: `messages` with system/user/assistant turns
- Assistant output: JSON string with all 11 sensitive entity keys

## Entity Schema

All keys are required. Values must be arrays. Empty categories must be `[]`.

- `address`
- `company_name`
- `email_address`
- `human_name`
- `phone_number`
- `account_identifier`
- `network_identifier`
- `system_config`
- `project_info`
- `financial_info`
- `transaction_id`

## Compared Models

- `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
- `LiquidAI/LFM2.5-1.2B-JP-GGUF`
- `PromptGate fine-tuned LFM2.5-1.2B-JP` (to be added)

## Current Evaluation

Evaluation assets are in `training/evaluation/`.

Current baseline result is an initial check on a seed-fixed random 10-case validation subset. It is reproducible, but not a final benchmark. See `training/evaluation/Evaluation_Result.md` for limitations.

| Model | Cases | Exact Case Correct | Expected Entities | Correct Entities | Entity Correct Rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 10 | 2 | 59 | 18 | 0.31 |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 10 | 0 | 59 | 16 | 0.27 |

W&B run:

https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/k25spzc0

## Reproduce

Use `uv`, not `pip`.

For W&B logging, copy `.env.example` to `.env` and set `WANDB_API_KEY`.

```powershell
uv run --with datasets python training\evaluation\prepare_eval_from_hf.py --sample random --seed 20260607 --limit 10 --out training\evaluation\validation_10_eval.jsonl
python training\evaluation\run_ollama_baselines.py --input training\evaluation\validation_10_eval.jsonl --out training\evaluation\validation_10_ollama_outputs.jsonl
uv run --with wandb python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --wandb-mode online --wandb-project promptgate --wandb-run-name promptgate-validation10-random-seed20260607
```

## Next Tasks

- Add `PromptGate fine-tuned LFM2.5-1.2B-JP` outputs to the same evaluation JSONL.
- Re-run evaluation so the W&B `model_comparison` table has three rows.
- Select demo cases where the fine-tuned model catches entities missed by the baselines.
