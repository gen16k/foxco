#!/usr/bin/env python
"""Run baseline PromptGate extraction prompts against local Ollama models."""

from __future__ import annotations

import argparse
import json
import time
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_MODELS = {
    "past_pii": "hf.co/LiquidAI/LFM2-350M-PII-Extract-JP-GGUF:Q4_K_M",
    "base_lfm25": "hf.co/LiquidAI/LFM2.5-1.2B-JP-GGUF:Q4_K_M",
}

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

SYSTEM_PROMPT = """あなたは日本語テキストから社外秘にすべき固有表現を抽出するアシスタントです。
入力テキストに含まれる機微情報を、必ず次の11キーをすべて含むJSON objectだけで出力してください。
該当がないキーは空配列 [] にしてください。説明文、Markdown、コードブロックは禁止です。
値は、入力テキストに実際に含まれている文字列だけをそのまま抽出してください。

keys:
address, company_name, email_address, human_name, phone_number,
account_identifier, network_identifier, system_config, project_info,
financial_info, transaction_id
"""


def load_jsonl(path: Path) -> list[dict[str, Any]]:
    rows = []
    with path.open("r", encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    return rows


def post_json(url: str, payload: dict[str, Any], timeout: int) -> dict[str, Any]:
    data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return json.loads(response.read().decode("utf-8"))


def chat(base_url: str, model: str, prompt: str, timeout: int) -> tuple[str, float]:
    start = time.perf_counter()
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": prompt},
        ],
        "stream": False,
        "format": "json",
        "options": {
            "temperature": 0,
            "num_predict": 512,
        },
    }
    response = post_json(base_url.rstrip("/") + "/api/chat", payload, timeout)
    elapsed_ms = (time.perf_counter() - start) * 1000
    return response.get("message", {}).get("content", ""), elapsed_ms


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--ollama-url", default="http://localhost:11434")
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--models", nargs="*", default=[])
    args = parser.parse_args()

    selected = DEFAULT_MODELS.copy()
    for item in args.models:
        name, value = item.split("=", 1)
        selected[name] = value

    rows = load_jsonl(args.input)
    out_rows = []
    for row in rows:
        predictions: dict[str, Any] = {}
        raw_outputs: dict[str, Any] = {}
        latencies: dict[str, float] = {}
        for name, model in selected.items():
            content, elapsed_ms = chat(args.ollama_url, model, row["prompt"], args.timeout)
            predictions[name] = content
            raw_outputs[name] = content
            latencies[name] = round(elapsed_ms, 2)
            print(f"{row.get('id')} {name}: {elapsed_ms:.0f} ms")

        out_rows.append(
            {
                "id": row.get("id", ""),
                "prompt": row["prompt"],
                "ground_truth": row["ground_truth"],
                "predictions": predictions,
                "raw_outputs": raw_outputs,
                "latency_ms": latencies,
            }
        )

    args.out.parent.mkdir(parents=True, exist_ok=True)
    with args.out.open("w", encoding="utf-8") as handle:
        for row in out_rows:
            handle.write(json.dumps(row, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
