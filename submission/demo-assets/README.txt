PromptGate Demo Assets
Team: Fox.co

Folder name:
Fox.co_Track1_HackTheLiquidWAY_DemoAssets

If submitting Track 2, rename this folder to:
Fox.co_Track2_HackTheLiquidWAY_DemoAssets

Tagline:
PromptGate - A local LFM guard that blocks sensitive prompts before they reach cloud LLMs.

Files:

1. demo-video.mp4
   60-90 second demo video showing PromptGate checking prompts locally before cloud submission.

2. screenshots/
   High-resolution screenshots of the product, demo flow, admin UI, W&B model comparison, and architecture.

3. product/
   Product image, logo, or application visual assets.

4. team/
   Team photos and short captions/bios.

5. setup.md
   Demo setup steps and commands.

Demo setup summary:

1. Start the local proxy / app environment.
2. Route Claude / ChatGPT / similar GenAI tool traffic through PromptGate.
3. Enter a prompt containing sensitive business information.
4. PromptGate detects sensitive content locally before cloud submission.
5. Unsafe prompts are blocked or receive an alternative response.
6. Safe prompts are allowed to reach the cloud LLM.

Technical summary:

- Local pre-send prompt guard.
- Runs as a proxy or extension.
- Uses compact Japanese LFMs for local checks.
- Baseline evaluation is tracked in W&B with model_comparison and examples tables.
- Current baseline latency on validation-10 random subset:
  - LiquidAI/LFM2-350M-PII-Extract-JP-GGUF: average 8.62 sec/case
  - LiquidAI/LFM2.5-1.2B-JP-GGUF: average 24.49 sec/case
- Fine-tuned model:
  - https://huggingface.co/akiFQC/LFM2.5-1.2B-JP-202606-Conf-Extract

W&B:
https://wandb.ai/aki310-sony-semiconductors/promptgate/runs/k25spzc0

Repository:
TBD
