#!/usr/bin/env python
"""Evaluate PromptGate extraction outputs and optionally log to W&B.

The script is intentionally lightweight:
- no model inference
- no mandatory wandb dependency
- accepts model outputs produced elsewhere

Usage:
  python training/evaluation/evaluate_and_log_wandb.py \
    --input training/evaluation/validation_10_ollama_outputs.jsonl \
    --out training/evaluation/validation_10_ollama.metrics.json \
    --wandb-mode offline
"""

from __future__ import annotations

import argparse
import json
import os
from collections import defaultdict
from pathlib import Path
from typing import Any


ENTITY_KEYS = [
    "address",
    "company_name",
    "email_address",
    "human_name",
    "phone_number",
    "account_identifier",
    "network_identifier",
    "system_config",
    "project_info",
    "financial_info",
    "transaction_id",
]

MODEL_DISPLAY_NAMES = {
    "past_pii": "LiquidAI/LFM2-350M-PII-Extract-JP-GGUF",
    "base_lfm25": "LiquidAI/LFM2.5-1.2B-JP-GGUF",
    "promptgate_ft": "PromptGate fine-tuned LFM2.5-1.2B-JP",
}


def load_local_env(start: Path) -> None:
    """Load simple .env values without adding a dotenv dependency."""
    for directory in [start, *start.parents]:
        env_path = directory / ".env"
        if not env_path.exists():
            continue
        for raw_line in env_path.read_text(encoding="utf-8").splitlines():
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" in line:
                key, value = line.split("=", 1)
                os.environ.setdefault(key.strip(), value.strip().strip('"').strip("'"))
            elif line.startswith("wandb_"):
                os.environ.setdefault("WANDB_API_KEY", line)
        return


def empty_entities() -> dict[str, list[str]]:
    return {key: [] for key in ENTITY_KEYS}


def normalize_entities(value: Any) -> tuple[dict[str, list[str]], bool]:
    """Return normalized entity dict and whether the input was valid JSON-ish."""
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            return empty_entities(), False
    if not isinstance(value, dict):
        return empty_entities(), False

    normalized: dict[str, list[str]] = {}
    valid = True
    for key in ENTITY_KEYS:
        raw = value.get(key, [])
        if raw is None:
            raw = []
        if not isinstance(raw, list):
            valid = False
            raw = [str(raw)]
        normalized[key] = [str(item) for item in raw if str(item) != ""]

    extra_keys = set(value.keys()) - set(ENTITY_KEYS)
    if extra_keys:
        valid = False
    return normalized, valid


def score_sets(predicted: list[str], expected: list[str]) -> tuple[int, int, int]:
    pred_set = set(predicted)
    exp_set = set(expected)
    true_positive = len(pred_set & exp_set)
    false_positive = len(pred_set - exp_set)
    false_negative = len(exp_set - pred_set)
    return true_positive, false_positive, false_negative


def safe_div(num: float, den: float) -> float:
    return num / den if den else 0.0


def f1(precision: float, recall: float) -> float:
    return safe_div(2 * precision * recall, precision + recall)


def evaluate(rows: list[dict[str, Any]]) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    model_counts: dict[str, dict[str, dict[str, int]]] = defaultdict(
        lambda: defaultdict(lambda: {"tp": 0, "fp": 0, "fn": 0})
    )
    model_valid = defaultdict(lambda: {"valid": 0, "total": 0})
    model_simple = defaultdict(
        lambda: {
            "case_total": 0,
            "case_exact_correct": 0,
            "expected_entity_total": 0,
            "predicted_entity_total": 0,
            "correct_entity_total": 0,
        }
    )
    example_rows: list[dict[str, Any]] = []

    for row in rows:
        truth, truth_valid = normalize_entities(row.get("ground_truth", {}))
        if not truth_valid:
            raise ValueError(f"Invalid ground_truth in row {row.get('id')}")

        predictions = row.get("predictions", {})
        if not isinstance(predictions, dict):
            raise ValueError(f"predictions must be an object in row {row.get('id')}")

        for model_name, raw_prediction in predictions.items():
            pred, pred_valid = normalize_entities(raw_prediction)
            model_valid[model_name]["total"] += 1
            model_valid[model_name]["valid"] += int(pred_valid)
            model_simple[model_name]["case_total"] += 1

            case_exact = pred_valid and all(set(pred[key]) == set(truth[key]) for key in ENTITY_KEYS)
            model_simple[model_name]["case_exact_correct"] += int(case_exact)

            for key in ENTITY_KEYS:
                tp, fp, fn = score_sets(pred[key], truth[key])
                model_counts[model_name][key]["tp"] += tp
                model_counts[model_name][key]["fp"] += fp
                model_counts[model_name][key]["fn"] += fn
                model_simple[model_name]["correct_entity_total"] += tp
                model_simple[model_name]["predicted_entity_total"] += tp + fp
                model_simple[model_name]["expected_entity_total"] += tp + fn

            example_rows.append(
                {
                    "id": row.get("id", ""),
                    "model": model_name,
                    "prompt": row.get("prompt", ""),
                    "json_valid": pred_valid,
                    "ground_truth": json.dumps(truth, ensure_ascii=False),
                    "prediction": json.dumps(pred, ensure_ascii=False),
                }
            )

    metrics: dict[str, Any] = {"models": {}, "num_cases": len(rows)}
    for model_name, per_key in model_counts.items():
        model_metrics: dict[str, Any] = {"per_category": {}}
        macro_f1_values = []
        total_tp = total_fp = total_fn = 0

        for key in ENTITY_KEYS:
            counts = per_key[key]
            precision = safe_div(counts["tp"], counts["tp"] + counts["fp"])
            recall = safe_div(counts["tp"], counts["tp"] + counts["fn"])
            category_f1 = f1(precision, recall)
            macro_f1_values.append(category_f1)
            total_tp += counts["tp"]
            total_fp += counts["fp"]
            total_fn += counts["fn"]
            model_metrics["per_category"][key] = {
                "precision": precision,
                "recall": recall,
                "f1": category_f1,
                **counts,
            }

        micro_precision = safe_div(total_tp, total_tp + total_fp)
        micro_recall = safe_div(total_tp, total_tp + total_fn)
        model_metrics["micro_precision"] = micro_precision
        model_metrics["micro_recall"] = micro_recall
        model_metrics["micro_f1"] = f1(micro_precision, micro_recall)
        model_metrics["macro_f1"] = sum(macro_f1_values) / len(macro_f1_values)
        model_metrics["json_valid_rate"] = safe_div(
            model_valid[model_name]["valid"],
            model_valid[model_name]["total"],
        )
        simple = model_simple[model_name]
        model_metrics["case_total"] = simple["case_total"]
        model_metrics["case_exact_correct"] = simple["case_exact_correct"]
        model_metrics["case_exact_accuracy"] = safe_div(
            simple["case_exact_correct"],
            simple["case_total"],
        )
        model_metrics["expected_entity_total"] = simple["expected_entity_total"]
        model_metrics["predicted_entity_total"] = simple["predicted_entity_total"]
        model_metrics["correct_entity_total"] = simple["correct_entity_total"]
        model_metrics["entity_correct_rate"] = safe_div(
            simple["correct_entity_total"],
            simple["expected_entity_total"],
        )
        metrics["models"][model_name] = model_metrics

    return metrics, example_rows


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    rows = []
    with path.open("r", encoding="utf-8") as handle:
        for line_number, line in enumerate(handle, 1):
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise ValueError(f"Invalid JSONL at {path}:{line_number}") from exc
    return rows


def model_comparison_rows(metrics: dict[str, Any]) -> list[list[str | int | float]]:
    rows: list[list[str | int | float]] = []
    for model_name, model_metrics in metrics["models"].items():
        rows.append(
            [
                MODEL_DISPLAY_NAMES.get(model_name, model_name),
                model_metrics["case_total"],
                model_metrics["case_exact_correct"],
                model_metrics["case_exact_accuracy"],
                model_metrics["expected_entity_total"],
                model_metrics["correct_entity_total"],
                model_metrics["entity_correct_rate"],
            ]
        )
    return rows


def log_wandb(args: argparse.Namespace, metrics: dict[str, Any], examples: list[dict[str, Any]]) -> None:
    if args.no_wandb:
        return
    try:
        import wandb
    except ImportError:
        print("wandb is not installed; skipping W&B logging.")
        return

    os.environ.setdefault("WANDB_MODE", args.wandb_mode)
    run = wandb.init(
        project=args.wandb_project,
        name=args.wandb_run_name,
        job_type="eval",
        config={
            "input": str(args.input),
            "dataset": args.dataset,
            "dataset_revision": args.dataset_revision,
            "entity_keys": ENTITY_KEYS,
        },
    )
    assert run is not None
    comparison_table = wandb.Table(
        columns=[
            "model",
            "case_total",
            "case_exact_correct",
            "case_exact_accuracy",
            "expected_entity_total",
            "correct_entity_total",
            "entity_correct_rate",
        ]
    )
    for row in model_comparison_rows(metrics):
        comparison_table.add_data(*row)
    run.log({"model_comparison": comparison_table})

    table = wandb.Table(columns=["id", "model", "prompt", "json_valid", "ground_truth", "prediction"])
    for example in examples:
        table.add_data(
            example["id"],
            MODEL_DISPLAY_NAMES.get(example["model"], example["model"]),
            example["prompt"],
            example["json_valid"],
            example["ground_truth"],
            example["prediction"],
        )
    run.log({"examples": table})

    artifact = wandb.Artifact("promptgate-eval-metrics", type="evaluation")
    artifact.add_file(str(args.out))
    run.log_artifact(artifact)
    run.finish()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--no-wandb", action="store_true")
    parser.add_argument("--wandb-mode", choices=["online", "offline", "disabled"], default="offline")
    parser.add_argument("--wandb-project", default="promptgate")
    parser.add_argument("--wandb-run-name", default="promptgate-eval-local")
    parser.add_argument(
        "--dataset",
        default="akiFQC/japanese-confidential-information-extraction-sft",
    )
    parser.add_argument("--dataset-revision", default="55feb02890de185853c66368d52b51ce96621c15")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    load_local_env(Path.cwd())
    rows = load_jsonl(args.input)
    metrics, examples = evaluate(rows)

    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(metrics, ensure_ascii=False, indent=2), encoding="utf-8")
    log_wandb(args, metrics, examples)

    print(json.dumps(metrics, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
