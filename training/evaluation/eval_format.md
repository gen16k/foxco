# Evaluation JSONL Format

Each line is one evaluation case.

## Input-Only Evaluation Set

`prepare_eval_from_hf.py` creates files like `validation_10_eval.jsonl`.

```json
{
  "id": "validation-00000",
  "prompt": "User prompt text",
  "ground_truth": {
    "address": [],
    "company_name": [],
    "email_address": [],
    "human_name": [],
    "phone_number": [],
    "account_identifier": [],
    "network_identifier": [],
    "system_config": [],
    "project_info": [],
    "financial_info": [],
    "transaction_id": []
  }
}
```

## Prediction Output

`run_ollama_baselines.py` adds model predictions and latency.

```json
{
  "id": "validation-00000",
  "prompt": "User prompt text",
  "ground_truth": {},
  "predictions": {
    "past_pii": "{\"address\": [], ...}",
    "base_lfm25": "{\"address\": [], ...}",
    "promptgate_ft": "{\"address\": [], ...}"
  },
  "raw_outputs": {
    "past_pii": "raw model output"
  },
  "latency_ms": {
    "past_pii": 5000.0
  }
}
```

Prediction values may be JSON objects or JSON strings. The evaluator normalizes them before scoring.

## Exact Case Correct

A case is counted as exact-correct only when:

- the prediction is valid schema output
- all 11 keys exist
- every value is an array
- each category exactly matches the ground truth set

## Entity Correct

Entity correctness is counted by exact string match inside the correct category.
