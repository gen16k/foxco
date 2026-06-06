#!/usr/bin/env python
"""Create a fixed PromptGate evaluation JSONL from the Hugging Face dataset."""

from __future__ import annotations

import argparse
import json
import random
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


def normalize_entities(value: Any) -> dict[str, list[str]]:
    if isinstance(value, str):
        value = json.loads(value)
    if not isinstance(value, dict):
        raise ValueError("assistant message must be a JSON object")

    normalized: dict[str, list[str]] = {}
    for key in ENTITY_KEYS:
        raw = value.get(key, [])
        if raw is None:
            raw = []
        if not isinstance(raw, list):
            raw = [str(raw)]
        normalized[key] = [str(item) for item in raw if str(item) != ""]
    return normalized


def message_content(message: Any) -> str:
    if not isinstance(message, dict):
        raise ValueError("message must be an object")
    content = message.get("content", "")
    if not isinstance(content, str):
        raise ValueError("message content must be a string")
    return content


def row_to_eval_case(row: dict[str, Any], index: int) -> dict[str, Any]:
    messages = row.get("messages")
    if not isinstance(messages, list) or len(messages) < 3:
        raise ValueError("row must contain messages with system/user/assistant turns")

    user_message = next((message for message in messages if message.get("role") == "user"), None)
    assistant_message = next((message for message in messages if message.get("role") == "assistant"), None)
    if user_message is None or assistant_message is None:
        raise ValueError("row must contain user and assistant messages")

    return {
        "id": f"validation-{index:05d}",
        "prompt": message_content(user_message),
        "ground_truth": normalize_entities(message_content(assistant_message)),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", default="akiFQC/japanese-confidential-information-extraction-sft")
    parser.add_argument("--split", default="validation")
    parser.add_argument("--limit", type=int, default=10)
    parser.add_argument("--offset", type=int, default=0)
    parser.add_argument("--sample", choices=["random", "first"], default="random")
    parser.add_argument("--seed", type=int, default=20260607)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()

    from datasets import load_dataset

    dataset = load_dataset(args.dataset, split=args.split)
    if args.limit < 1:
        raise ValueError("--limit must be positive")
    if args.limit > len(dataset):
        raise ValueError(f"--limit must be <= dataset size ({len(dataset)})")

    if args.sample == "first":
        indices = list(range(args.offset, args.offset + args.limit))
    else:
        rng = random.Random(args.seed)
        indices = sorted(rng.sample(range(len(dataset)), args.limit))

    selected = dataset.select(indices)

    args.out.parent.mkdir(parents=True, exist_ok=True)
    with args.out.open("w", encoding="utf-8") as handle:
        for index, row in zip(indices, selected):
            case = row_to_eval_case(dict(row), index)
            handle.write(json.dumps(case, ensure_ascii=False) + "\n")

    print(f"Wrote {len(selected)} {args.sample} cases to {args.out}")
    print("indices=" + ",".join(str(index) for index in indices))


if __name__ == "__main__":
    main()
