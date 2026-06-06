# NPU sidecar — LFM2 on the AMD Ryzen AI NPU (XDNA2)

`npu_server.py` is a thin **OpenAI-compatible HTTP shim** that runs the LFM2
classifier on the AMD Ryzen AI NPU and speaks the same wire contract as the
llama.cpp sidecar (`POST /v1/chat/completions`, `GET /health`). The Go proxy
talks to it unchanged; `start.ps1 -Backend npu` (and the default `auto`) launch it.

## Why a custom shim (and not Lemonade)

- **llama.cpp / Ollama cannot drive the NPU** — GGUF runs on CPU/GPU only.
- AMD ships LFM2 for the NPU **not** as an onnxruntime-genai (OGA) model but as a
  custom **token-fusion ONNX graph** (`lfm2-1.2B-token-fusion.onnx` + `.onnx.data`
  + precompiled DPU control packets), run via `onnxruntime.InferenceSession` +
  `RyzenAILightExecutionProvider`. AMD's reference is the CLI `Run-LFM2.py`.
- **Lemonade Server (OGA) cannot run LFM2** — it requires `genai_config.json`
  (OGA-only); LFM2 is explicitly outside the OGA flow. So the only path to
  LFM2-on-NPU is AMD's native Ryzen AI runtime, which has no HTTP server. This
  shim wraps `Run-LFM2.py`'s exact prefill/decode loop behind the HTTP contract.

## Prerequisites (one-time, Windows 11)

1. **AMD NPU driver (XDNA2)** — AMD's specified WHQL version (`npu_sw_installer.exe`).
2. **Miniforge (conda)** — `winget install CondaForge.Miniforge3`.
3. **AMD Ryzen AI Software 1.7.1** (`ryzen-ai-lt-1.7.1.exe`, from AMD's account
   portal; not on winget) → creates the conda env `ryzen-ai-1.7.1` with the Ryzen
   AI Execution Provider.
4. **LFM2 NPU model**: `git lfs` clone `amd/LFM2-1.2B-ONNX_rai_1.7.1` (~2.5 GB).
   `ryzenai_ep_utils.py` ships inside that directory.

The shim itself adds **no pip dependencies** — it uses only the Python standard
library on top of the env's numpy/transformers/onnxruntime.

## Run

```powershell
conda run -n ryzen-ai-1.7.1 python .\npu\npu_server.py `
    --model C:\Users\<you>\ryzenai-lfm2\LFM2-1.2B-ONNX_rai_1.7.1 `
    --host 127.0.0.1 --port 8792
```

Then probe it:

```powershell
Invoke-WebRequest http://127.0.0.1:8792/health            # -> 200 {"status":"ok"}
```

The first launch compiles NPU overlays and can take minutes (the socket only
opens once the model is loaded). `start.ps1` waits up to `-HealthTimeoutSec`
(default 600s) for `/health`.

## Notes

- **Greedy, temperature 0.** `response_format`/`temperature` in the request are
  accepted but ignored — the NPU graph cannot grammar-constrain output, so the
  proxy uses the `reason_decision_prompt` profile (no schema) plus tolerant,
  fail-closed parsing.
- **Security:** binds `127.0.0.1` only; never logs request/response bodies,
  prompts, or generated text (metadata only). Mirrors the proxy's invariants in
  `../CLAUDE.md`.
- One NPU session; generation is serialized with a lock. `/health` stays
  responsive during a generation.
