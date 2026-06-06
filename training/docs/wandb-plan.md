# W&B Plan for PromptGate

Purpose: show that PromptGate improves over baseline models on the same fixed evaluation set.

## Current W&B Usage

For evaluation, W&B should be table-first.

The main table is `model_comparison`:

- model
- case_total
- case_exact_correct
- case_exact_accuracy
- expected_entity_total
- correct_entity_total
- entity_correct_rate

The secondary table is `examples`:

- prompt
- ground truth
- normalized prediction
- schema validity

Step-based graphs are not useful for the current baseline comparison because each evaluation run has a single fixed result.

## Baseline Run

Current validation-10 run:

https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/k25spzc0

Compared models:

- `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
- `LiquidAI/LFM2.5-1.2B-JP-GGUF`

## Final Run

When the fine-tuned model is ready, create a final run with three rows:

- `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
- `LiquidAI/LFM2.5-1.2B-JP-GGUF`
- `PromptGate fine-tuned LFM2.5-1.2B-JP`

## Reproduce

Use `uv`, not `pip`.

Create `.env` from `.env.example` and set your own W&B API key:

```powershell
Copy-Item .env.example .env
```

```text
WANDB_API_KEY=wandb_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
WANDB_PROJECT=promptgate
```

```powershell
uv run --with datasets python training\evaluation\prepare_eval_from_hf.py --sample random --seed 20260607 --limit 10 --out training\evaluation\validation_10_eval.jsonl
python training\evaluation\run_ollama_baselines.py --input training\evaluation\validation_10_eval.jsonl --out training\evaluation\validation_10_ollama_outputs.jsonl
uv run --with wandb python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --wandb-mode online --wandb-project promptgate
```

If model inference has already been run, the last command is enough to upload W&B tables from `validation_10_ollama_outputs.jsonl`.
