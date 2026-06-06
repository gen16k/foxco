# Evaluation Result

Status: validation-10 baseline evaluation completed

Last run: 2026-06-07 JST

W&B run:

https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/pst7vmz7

## Summary

We evaluated two baseline models on the same fixed 10-case validation set from:

https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft

Compared models:

- `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
- `LiquidAI/LFM2.5-1.2B-JP-GGUF`
- `PromptGate fine-tuned LFM2.5-1.2B-JP` (not evaluated yet)

## Evaluation Limitations

This result is an initial baseline check, not the final benchmark.

- The 10 cases were taken from the beginning of the validation split (`offset=0`, `limit=10`), so they are fixed and reproducible but not necessarily representative.
- The scoring is strict exact match. For example, `NHK` vs `NHKラジオ`, or splitting one address into two entities, is counted as incorrect.
- The base model often returns strings instead of arrays. The evaluator normalizes these values for entity counting, but marks the schema as invalid.
- The next evaluation should use a seed-fixed random sample or a category-balanced subset, especially including PromptGate-specific categories such as `account_identifier`, `system_config`, `financial_info`, and `transaction_id`.

## Main Result

This is the main table to show in W&B and slides.

| Model | Cases | Exact Case Correct | Expected Entities | Correct Entities | Entity Correct Rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 10 | 1 | 20 | 4 | 0.20 |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 10 | 0 | 20 | 1 | 0.05 |
| `PromptGate fine-tuned LFM2.5-1.2B-JP` | TBD | TBD | TBD | TBD | TBD |

Definitions:

- `Exact Case Correct`: all 11 categories exactly matched and schema was valid.
- `Correct Entities`: exact string match in the correct category.
- `Entity Correct Rate`: `Correct Entities / Expected Entities`.

## Detailed Metrics

Detailed metrics are kept for reproducibility, but they are not the primary presentation view.

| Model | Micro Precision | Micro Recall | Micro F1 | Macro F1 | Schema Valid Rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 0.2500 | 0.2000 | 0.2222 | 0.1742 | 1.0000 |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 0.0323 | 0.0500 | 0.0392 | 0.0364 | 0.0000 |

## Interpretation

`LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` found 4 of 20 target entities and fully solved 1 of 10 prompts. It can detect some traditional PII, but misses many PromptGate-specific business-sensitive categories such as account identifiers, system configuration, financial information, and transaction IDs.

`LiquidAI/LFM2.5-1.2B-JP-GGUF` found 1 of 20 target entities and fully solved 0 of 10 prompts. It also struggled with strict schema compliance and sometimes generated values that were not in the input.

This supports the fine-tuning story: the base model is capable, but PromptGate needs task-specific tuning to become a reliable pre-send guard.

## Reproduce

```powershell
uv run --with datasets python training\evaluation\prepare_eval_from_hf.py --limit 10 --out training\evaluation\validation_10_eval.jsonl
python training\evaluation\run_ollama_baselines.py --input training\evaluation\validation_10_eval.jsonl --out training\evaluation\validation_10_ollama_outputs.jsonl
uv run --with wandb python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --wandb-mode online --wandb-project promptgate --wandb-run-name promptgate-validation10-official-model-names-20260607
```

## Next Step

Run the fine-tuned PromptGate model on the same `validation_10_eval.jsonl` and add it as the third row in W&B `model_comparison`.
