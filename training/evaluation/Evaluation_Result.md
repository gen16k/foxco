# Evaluation Result

Status: validation-10 random baseline evaluation completed

Last run: 2026-06-07 JST

Tagline: PromptGate — A local LFM guard that blocks sensitive prompts before they reach cloud LLMs.

W&B run:

https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/k25spzc0

## Summary

We evaluated two baseline models on the same seed-fixed random 10-case validation subset from:

https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft

Sampling command:

```powershell
uv run --with datasets python training\evaluation\prepare_eval_from_hf.py --sample random --seed 20260607 --limit 10 --out training\evaluation\validation_10_eval.jsonl
```

Selected validation indices:

```text
71,312,363,479,486,601,775,822,861,1764
```

Compared models:

- `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
- `LiquidAI/LFM2.5-1.2B-JP-GGUF`
- `akiFQC/LFM2-350M-Conf-Extract-Japanese` (previous fine-tuned 350M model, not evaluated yet)
- `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract` (fine-tuned PromptGate model, not evaluated yet)

## Evaluation Limitations

This result is an initial baseline check, not the final benchmark.

- The 10 cases are seed-fixed and reproducible, but the sample is still small.
- The scoring is strict exact match. For example, `NHK` vs `NHKラジオ`, or splitting one address into two entities, is counted as incorrect.
- The base model often returns strings instead of arrays. The evaluator normalizes these values for entity counting, but marks the schema as invalid.
- The final evaluation should use a larger random sample or a category-balanced subset, especially including PromptGate-specific categories such as `account_identifier`, `system_config`, `financial_info`, and `transaction_id`.

## Main Result

This is the main table to show in W&B and slides.

| Model | Cases | Exact Case Correct | Expected Entities | Correct Entities | Entity Correct Rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 10 | 2 | 59 | 18 | 0.31 |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 10 | 0 | 59 | 16 | 0.27 |
| `akiFQC/LFM2-350M-Conf-Extract-Japanese` | TBD | TBD | TBD | TBD | TBD |
| `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract` | TBD | TBD | TBD | TBD | TBD |

Definitions:

- `Exact Case Correct`: all 11 categories exactly matched and schema was valid.
- `Correct Entities`: exact string match in the correct category.
- `Entity Correct Rate`: `Correct Entities / Expected Entities`.

## Detailed Metrics

Detailed metrics are kept for reproducibility, but they are not the primary presentation view.

| Model | Micro Precision | Micro Recall | Micro F1 | Macro F1 | Schema Valid Rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 0.5625 | 0.3051 | 0.3956 | 0.1856 | 0.5000 |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 0.2540 | 0.2712 | 0.2623 | 0.2104 | 0.0000 |

## Interpretation

`LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` found 18 of 59 target entities and fully solved 2 of 10 prompts. It detects some traditional PII, but misses PromptGate-specific business-sensitive categories.

`LiquidAI/LFM2.5-1.2B-JP-GGUF` found 16 of 59 target entities and fully solved 0 of 10 prompts. It found some entities, but strict schema compliance remains poor.

This supports the fine-tuning story: the base model has useful extraction ability, but PromptGate needs task-specific tuning to become a reliable pre-send guard.

## Reproduce

```powershell
uv run --with datasets python training\evaluation\prepare_eval_from_hf.py --sample random --seed 20260607 --limit 10 --out training\evaluation\validation_10_eval.jsonl
python training\evaluation\run_ollama_baselines.py --input training\evaluation\validation_10_eval.jsonl --out training\evaluation\validation_10_ollama_outputs.jsonl
uv run --with wandb python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --wandb-mode online --wandb-project promptgate --wandb-run-name promptgate-validation10-random-seed20260607
```

## Next Step

Run the final fine-tuned LFM2.5 model on the same `validation_10_eval.jsonl` and add it to W&B `model_comparison`.
