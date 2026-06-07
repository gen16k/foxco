# PromptGate Submission Checklist

Team: Fox.co

Product: PromptGate

Tagline:

```text
PromptGate - A local LFM guard that blocks sensitive prompts before they reach cloud LLMs.
```

Track:

```text
TBD at submission. Current primary option: Track 1 (LFM Application Track).
```

Public repo link:

```text
TBD
```

## Common Package

### 1. Slide Deck (2-4 slides)

Status: TODO

Deck language:

```text
English slides for Japanese presentation.
```

Required content:

- Japanese problem / use case
- Why an LFM
- Approach
- Results

Suggested structure:

| Slide | Title | Must include |
| --- | --- | --- |
| 1 | PromptGate | product name, tagline, team name, one-line problem |
| 2 | Solution & Target | local proxy / extension, target market, Teikoku Databank 2026 evidence |
| 3 | Technical Approach | architecture diagram, models, local pre-send flow |
| 4 | Results & Demo | W&B `model_comparison`, demo flow, next step |

### 2. Live Demo (5 minutes)

Status: TODO

Demo story:

1. User keeps using Claude / ChatGPT / similar GenAI tools as usual.
2. PromptGate runs locally as a proxy or extension.
3. Sensitive prompt is detected before cloud submission.
4. PromptGate blocks or returns an alternative response when unsafe.
5. Admin/evaluation evidence shows why fine-tuned LFM improves the guard.

### 3. Tagline and Public Repo Link

Status: PARTIAL

- Tagline: ready
- Public repo link: TODO after push

### 4. Demo Assets Folder

Status: TODO

Folder name:

```text
Fox.co_Track1_HackTheLiquidWAY_DemoAssets
```

If submitting Track 2, rename to:

```text
Fox.co_Track2_HackTheLiquidWAY_DemoAssets
```

Required files:

- 60-90 second demo video
- High-resolution screenshots
- Product image / logo
- Team photos
- Captions / bios
- `README.txt` with file descriptions and demo setup steps

Note: encrypt the folder and share the password with `@liquid-yan` on Discord.

### 5. Technical Summary

Status: DRAFT

Can be included in the slide deck or Demo Assets `README.txt`.

Must include:

- Models and framework
- Compute setup
- Device plus latency / efficiency numbers
- Architecture diagram or key technical innovation

Current draft:

- Models:
  - `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`
  - `LiquidAI/LFM2.5-1.2B-JP-GGUF`
  - `akiFQC/LFM2-350M-Conf-Extract-Japanese`
  - `akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract`
- Framework:
  - local proxy / VSCode extension
  - Ollama / GGUF for baseline local evaluation
  - Transformers/Safetensors for fine-tuned models
  - W&B for model comparison tables
- Compute / runtime:
  - local Ollama baseline evaluation on the hackathon development PC
  - proxy/app target: employee PC or internal proxy server
- Current latency numbers on validation-10 random subset:
  - `LiquidAI/LFM2-350M-PII-Extract-JP-GGUF`: average 8.62 sec/case
  - `LiquidAI/LFM2.5-1.2B-JP-GGUF`: average 24.49 sec/case
  - final fine-tuned model latency: TODO after Transformers or GGUF evaluation
- Architecture / key innovation:
  - pre-send local guard that runs before cloud submission
  - users keep existing GenAI workflows
  - unsafe prompts can be blocked or answered with an alternative local response
- Current W&B:
  - https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/k25spzc0

## Judging Criteria Mapping

| Criterion | PromptGate angle |
| --- | --- |
| Fit to Challenge | Japan-specific GenAI adoption faces data leakage risk; PromptGate protects before cloud submission |
| Creativity & Design | Invisible local guard that lets users keep existing GenAI workflows |
| Quality & Completeness | Proxy, VSCode extension, evaluation scripts, W&B comparison, docs |
| Resource Efficiency | Local compact LFM checks reduce extra cloud monitoring LLM/API calls |
| Track-Specific | Application workflow plus fine-tuned model comparison |

## Open Items

- Decide final track number.
- Push public repo and add repo link.
- Add final fine-tuned model evaluation if a Transformers inference path is ready.
- Record 60-90 second demo video.
- Capture high-resolution screenshots.
- Prepare encrypted demo assets folder.
- Prepare 5-minute live demo script.
