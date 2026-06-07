# PromptGate

PromptGate - A local LFM guard that blocks sensitive prompts before they reach cloud LLMs.

## Quick Links

- Slide + demo video: https://youtu.be/6dpdyT167RM?si=nXwf2KLgvu0UNSAx
- Slide PDF: https://drive.google.com/file/d/1hPTru7czIH7GRFMk9cOUfYvm20szlZ7D/view?usp=sharing
- Final fine-tuned model: https://huggingface.co/akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract
- Previous fine-tuned model: https://huggingface.co/akiFQC/LFM2-350M-Conf-Extract-Japanese
- Dataset: https://huggingface.co/datasets/akiFQC/japanese-confidential-information-extraction-sft
- W&B comparison run: https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/k25spzc0
- Submission checklist: [submission/README.md](submission/README.md)

## What It Does

PromptGate checks prompts locally before they are sent to cloud LLM services such as Claude and ChatGPT.

It can run as a local proxy server or through a client-side integration such as a VSCode extension. Users keep using their normal GenAI tools; PromptGate runs behind the workflow, detects sensitive information, and blocks, masks, or returns an alternative response when the prompt is unsafe.

## Why It Matters

Japanese enterprises are adopting GenAI while still facing data leakage risk.

GenAI is still early in Japan: 34.5% of companies use it, 14.2% are considering it, and 41.7% of large enterprises worry about data leakage. ([Teikoku Databank, 2026](https://www.tdb.co.jp/resource/files/assets/d4b8e8ee91d1489c9a2abd23a4bb5219/61afb5417b4e4abf83e66684660cb4a8/20260514_%E7%94%9F%E6%88%90AI%E3%81%AB%E9%96%A2%E3%81%99%E3%82%8B%E4%BC%81%E6%A5%AD%E3%81%AE%E5%8B%95%E5%90%91%E8%AA%BF%E6%9F%BB%EF%BC%882026%E5%B9%B43%E6%9C%88%EF%BC%89.pdf))

PromptGate moves the guardrail before cloud submission. This reduces accidental leakage and avoids sending sensitive text to an additional cloud-side monitoring model just to decide whether the text should have been sent.

## Why LFM

Fast, private, low-cost local checks powered by compact Japanese LFMs.

LFM2.5-1.2B-JP is small enough to target employee PCs or internal proxy servers while still handling Japanese business text. Fine-tuning lets the model focus on confidential Japanese organizational entities, not only generic PII.

## Demo Flow

1. A user enters a normal-looking prompt that contains confidential information.
2. PromptGate intercepts the prompt locally before cloud submission.
3. The local LFM extracts sensitive entities across 11 categories.
4. If confidential entities are found, PromptGate blocks or returns an alternative response.
5. The admin viewer shows the detection result and the reason.

Example demo prompt:

```text
映画コラボキャンペーンの提案メールを、社外パートナー向けに作成してください。
未発表企画名「赤い帽子の大冒険」と社内施策名「土管ワープ大作戦」を含め、共同制作先の株式会社きつね堂、宣伝予算4,800万円を自然に盛り込んでください。
```

Expected sensitive categories:

| Category | Example |
| --- | --- |
| `project_info` | 赤い帽子の大冒険, 土管ワープ大作戦 |
| `company_name` | 株式会社きつね堂 |
| `financial_info` | 4,800万円 |

## Repository Layout

| Path | Purpose |
| --- | --- |
| [proxy-server/](proxy-server/) | Local proxy server, LFM guard, admin web viewer, Claude/Anthropic integration |
| [vscode-extension/](vscode-extension/) | VSCode-side integration and proxy health hook |
| [fine-tuning/](fine-tuning/) | Dataset processing, model training, conversion, and evaluation assets |
| [submission/](submission/) | Submission checklist and demo assets README templates |

## Models

| Model | Role | Notes |
| --- | --- | --- |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | Previous PII extraction baseline | Existing Japanese PII extraction model |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | Base LFM2.5 baseline | Original model before confidential-info fine-tuning |
| `akiFQC/LFM2-350M-Conf-Extract-Japanese` | Previous fine-tuned comparison model | Kept as a comparison target |
| `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract` | Final fine-tuned model | Main PromptGate confidential-information extraction model |

## Extraction Schema

PromptGate uses an 11-category confidential-information schema:

`address`, `company_name`, `email_address`, `human_name`, `phone_number`, `account_identifier`, `network_identifier`, `system_config`, `project_info`, `financial_info`, `transaction_id`.

The model outputs JSON arrays for each category. PromptGate treats any non-empty confidential category as a reason to block, mask, or return an alternative local response.

## Current Evaluation

The current W&B run compares two baseline models on a random 10-case validation subset from the dataset.

| Model | Cases | Exact correct | Expected entities | Correct entities | Entity correct rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 10 | 2 | 59 | 18 | 0.305 |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 10 | 0 | 59 | 16 | 0.271 |

Random validation indices:

```text
71, 312, 363, 479, 486, 601, 775, 822, 861, 1764
```

Evaluation note: these 10 cases are sampled from the validation split, but they are not yet a statistically strong benchmark. The final fine-tuned LFM2.5 model should be added to the same comparison table once the Transformers or GGUF inference path is ready.

## Technical Summary

- Local guard: Go proxy server with Anthropic-compatible request handling.
- Model runtime: llama.cpp / GGUF for local baseline and proxy-side inference; Transformers/Safetensors for fine-tuned model evaluation.
- App surface: admin web viewer plus VSCode-side integration.
- Audit approach: store detection metadata and categories; avoid exposing extracted sensitive values by default.
- Key innovation: pre-send confidential-information detection, before the prompt reaches a cloud LLM.

Current latency numbers on the random 10-case baseline evaluation:

| Model | Average latency |
| --- | ---: |
| `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF` | 8.62 sec/case |
| `LiquidAI/LFM2.5-1.2B-JP-GGUF` | 24.49 sec/case |

Final fine-tuned model latency is still to be measured after the final inference path is connected.

## Running The Components

Proxy server setup and demo commands are documented in [proxy-server/README.md](proxy-server/README.md).

Fine-tuning and model usage details are documented in [fine-tuning/README.md](fine-tuning/README.md) and [fine-tuning/experiments/README.md](fine-tuning/experiments/README.md).

Submission packaging notes are documented in [submission/README.md](submission/README.md).

## Team

Team: Fox.co

| Member | Role in PromptGate | Background |
| --- | --- | --- |
| Fukuchi Akihiko / 福地 成彦 | Fine-tuning and dataset | LLM Research Engineer |
| Kimura Genichiro / 木村 源一朗 | Proxy server and admin application | Platform Infrastructure Engineer |
| Akita Satoru / 秋田 賢 | Product, research, slides, submission | Edge AI Engineer |
