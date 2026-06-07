# PromptGate Evaluation

This folder contains the reproducible evaluation pipeline for PromptGate.

Tagline: PromptGate — A local LFM guard that blocks sensitive prompts before they reach cloud LLMs.

The evaluation is a fixed-set model comparison, not a training curve. W&B is therefore used mainly for tables, not step-based graphs.

## Files

- `prepare_eval_from_hf.py`: creates a fixed evaluation JSONL from the Hugging Face validation split
- `run_ollama_baselines.py`: runs local Ollama models and writes raw predictions
- `evaluate_and_log_wandb.py`: evaluates predictions and optionally logs W&B tables
- `validation_10_eval.jsonl`: seed-fixed random 10-case evaluation set
- `validation_10_ollama_outputs.jsonl`: raw outputs from two baseline models
- `validation_10_ollama.metrics.json`: detailed metrics JSON
- `Evaluation_Result.md`: human-readable result summary
- `eval_format.md`: JSONL format reference

## W&B Tables

The main W&B artifact is `model_comparison`.

| Column | Meaning |
| --- | --- |
| `model` | Display name of the evaluated model |
| `case_total` | Number of prompts evaluated |
| `case_exact_correct` | Number of prompts where all 11 categories exactly matched |
| `case_exact_accuracy` | `case_exact_correct / case_total` |
| `expected_entity_total` | Number of target entities in ground truth |
| `correct_entity_total` | Number of correctly extracted entities |
| `entity_correct_rate` | `correct_entity_total / expected_entity_total` |

The `examples` table stores prompts, ground truth, normalized predictions, and schema validity for inspection.

Detailed precision/recall/F1 metrics are kept in the local metrics JSON and W&B artifact, but they are not the primary demo view.

## Fine-Tuned Model Status

Fine-tuned PromptGate model link:

https://huggingface.co/akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract

Previous 350M fine-tuned model for comparison:

https://huggingface.co/akiFQC/LFM2-350M-Conf-Extract-Japanese

These are Transformers/Safetensors models, not GGUF/Ollama models. The current baseline runner (`run_ollama_baselines.py`) only evaluates local Ollama models, so the fine-tuned models need a separate Transformers inference path or GGUF conversion before they can be added to `model_comparison`.

## Current Evaluation Limitations

The current `validation_10_eval.jsonl` is an initial baseline check.

- It uses a seed-fixed random sample from the validation split (`--sample random --seed 20260607 --limit 10`), so it is reproducible but still small.
- Scoring is strict exact match. Near misses such as longer spans, shorter spans, merged entities, or whitespace differences may be counted as incorrect.
- Schema validity is intentionally strict because PromptGate needs stable structured output for proxy use.
- For the final comparison, prefer a larger random sample or a category-balanced subset that covers PromptGate-specific categories.

## Reproduce Validation-10 Baseline

Prerequisites:

- Ollama is running on `http://localhost:11434`
- The two baseline GGUF models are available in Ollama
- W&B API key is available in `.env`

## Connect W&B

Create an API key at:

https://wandb.ai/authorize

Then create `.env` in the repository root:

```powershell
Copy-Item .env.example .env
```

Edit `.env`:

```text
WANDB_API_KEY=wandb_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
WANDB_PROJECT=promptgate
```

The evaluator also accepts a raw `wandb_...` line for local convenience, but `WANDB_API_KEY=...` is recommended for third-party use.

To verify W&B import and authentication without running model inference:

```powershell
uv run --with wandb python -c "import wandb; print(wandb.__version__)"
uv run --with wandb python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --wandb-mode online --wandb-project promptgate --wandb-run-name promptgate-validation10-wandb-check
```

Expected W&B outputs:

- `model_comparison` table
- `examples` table
- `promptgate-eval-metrics` artifact

Commands:

```powershell
uv run --with datasets python training\evaluation\prepare_eval_from_hf.py --sample random --seed 20260607 --limit 10 --out training\evaluation\validation_10_eval.jsonl
python training\evaluation\run_ollama_baselines.py --input training\evaluation\validation_10_eval.jsonl --out training\evaluation\validation_10_ollama_outputs.jsonl
uv run --with wandb python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --wandb-mode online --wandb-project promptgate --wandb-run-name promptgate-validation10-random-seed20260607
```

To evaluate without W&B:

```powershell
python training\evaluation\evaluate_and_log_wandb.py --input training\evaluation\validation_10_ollama_outputs.jsonl --out training\evaluation\validation_10_ollama.metrics.json --no-wandb
```
