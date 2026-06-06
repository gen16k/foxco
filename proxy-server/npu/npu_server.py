#!/usr/bin/env python
"""Thin OpenAI-compatible HTTP shim for LFM2 on the AMD Ryzen AI NPU (XDNA2).

Why this exists
---------------
The proxy classifier talks to a local OpenAI-compatible LFM server over HTTP
(``POST /v1/chat/completions`` + ``GET /health``) — the same wire contract as a
llama.cpp ``llama-server``. llama.cpp/Ollama cannot drive the NPU, and AMD ships
LFM2 for the NPU NOT as an onnxruntime-genai (OGA) model but as a custom
token-fusion ONNX graph run through ``onnxruntime.InferenceSession`` +
``RyzenAILightExecutionProvider`` (see AMD's ``Run-LFM2.py``). That reference is a
CLI, not a server. This shim wraps the exact same prefill/decode loop behind the
llama.cpp HTTP contract so the Go proxy can use the NPU with zero wire changes.

Runtime: run inside AMD's ``ryzen-ai-1.7.1`` conda env (which provides numpy,
transformers, onnxruntime and the Ryzen AI EP). It uses ONLY the Python standard
library for HTTP so no extra packages are added to that pinned env.

    conda run -n ryzen-ai-1.7.1 python npu_server.py \
        --model C:\\Users\\<you>\\ryzenai-lfm2\\LFM2-1.2B-ONNX_rai_1.7.1 \
        --host 127.0.0.1 --port 8792

Security invariants (mirrors the proxy's CLAUDE.md):
  * Binds 127.0.0.1 only — never the LAN.
  * NEVER logs raw content: no request/response bodies, prompts, or generated
    text reach logs. Only request metadata (method, path, status) is logged.
  * The inspected text is inert data; this server only generates, it does not act
    on anything inside the prompt.
"""

import argparse
import json
import math
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np

# Bound request bodies and generation length. The proxy sends short, segmented
# classification prompts and a small JSON verdict, so these are generous ceilings,
# not expected sizes.
MAX_BODY = 8 * 1024 * 1024   # 8 MiB
MAX_GEN = 1024               # cap completion tokens regardless of requested max
MAXSEQ = 4096                # KV/conv cache horizon (matches AMD's reference loop)


class LFM2Engine:
    """Loads the AMD LFM2 token-fusion ONNX model once and runs greedy decode.

    The prefill/decode loop (io_binding, KV cache for full_attention layers, conv
    cache for conv layers, 512-byte aligned KV buffers, argmax sampling) mirrors
    AMD's ``Run-LFM2.py`` / ``ryzenai_ep_utils`` reference exactly; that is the
    only supported way to run LFM2 on the NPU.
    """

    def __init__(self, model_dir):
        model_dir = os.path.abspath(model_dir)
        if not os.path.isdir(model_dir):
            raise FileNotFoundError(model_dir)
        # ryzenai_ep_utils.py ships INSIDE the model directory and, on import,
        # chdir's to the Ryzen AI EP deployment dir. Import it from there.
        sys.path.insert(0, model_dir)
        import ryzenai_ep_utils  # noqa: E402  (import has the chdir side effect)

        onnx = next(f for f in os.listdir(model_dir)
                    if f.lower().endswith("-token-fusion.onnx"))
        self.rai = ryzenai_ep_utils.loader.local(os.path.join(model_dir, onnx))
        self.names = {x.name for x in self.rai.ort_session.get_inputs()}

        eos = set()
        ce = getattr(self.rai.config, "eos_token_id", None)
        if isinstance(ce, (list, tuple)):
            eos.update(int(x) for x in ce)
        elif ce is not None:
            eos.add(int(ce))
        te = getattr(self.rai.tokenizer, "eos_token_id", None)
        if te is not None:
            eos.add(int(te))
        self.eos_ids = eos

        # The NPU session is single; serialize all generation.
        self._lock = threading.Lock()

    def _encode(self, messages):
        """Render chat messages to input ids via the model's chat template.

        Falls back to folding any system turn into the first user turn for
        templates that do not support a system role.
        """
        tok = self.rai.tokenizer
        try:
            return tok.apply_chat_template(
                messages, add_generation_prompt=True, tokenize=True,
                return_dict=True, return_tensors="np")
        except Exception:
            sys_txt = "".join(m.get("content", "") + "\n\n"
                              for m in messages if m.get("role") == "system")
            usr_txt = "".join(m.get("content", "")
                              for m in messages if m.get("role") != "system")
            return tok.apply_chat_template(
                [{"role": "user", "content": sys_txt + usr_txt}],
                add_generation_prompt=True, tokenize=True,
                return_dict=True, return_tensors="np")

    def generate(self, messages, max_new_tokens):
        """Return (text, prompt_tokens, completion_tokens). Greedy, temperature 0."""
        with self._lock:
            return self._generate_locked(messages, max_new_tokens)

    def _generate_locked(self, messages, max_new_tokens):
        rai = self.rai
        enc = self._encode(messages)
        base_ids = enc["input_ids"].astype(np.int64)
        base_attn = enc["attention_mask"].astype(np.int64)
        prompt_len = int(base_ids.shape[-1])
        if prompt_len >= MAXSEQ:
            raise ValueError("prompt exceeds context window")
        max_new = max(1, min(int(max_new_tokens), MAXSEQ - prompt_len))

        input_ids = base_ids.copy()
        attn = np.pad(base_attn.copy(), ((0, 0), (0, MAXSEQ - base_attn.shape[1])),
                      mode="constant", constant_values=0)
        bsz = input_ids.shape[0]
        pos = np.tile(np.arange(0, input_ids.shape[-1]), (bsz, 1)).astype(np.int64)
        kv_shape = [bsz, rai.config.num_key_value_heads, MAXSEQ,
                    rai.config.hidden_size // rai.config.num_attention_heads]
        conv_shape = [bsz, rai.conv_shape[1], rai.conv_shape[2]]

        caches, past = {}, {}
        for i in range(rai.config.num_hidden_layers):
            lt = rai.config.layer_types[i]
            if lt == "full_attention":
                for kv in ("key", "value"):
                    caches[f"{i}.{kv}"] = np.zeros(kv_shape, dtype=rai.dtype_kv_cache)
            elif lt == "conv":
                caches[f"{i}.conv"] = np.zeros(conv_shape, dtype=rai.dtype_conv_cache)
            else:
                raise ValueError(lt)

        ptoks = input_ids.shape[-1]
        past_seq = 0
        out_ids = []
        for step in range(max_new):
            seqlen = input_ids.shape[-1]
            total = past_seq + seqlen
            logits = np.zeros([bsz, 1 if rai.logits_pruned else seqlen, 65536],
                              dtype=rai.dtype_logits)
            io = rai.ort_session.io_binding()
            io.bind_input("input_ids", "cpu", 0, np.int64, input_ids.shape,
                          input_ids.ctypes.data)
            io.bind_input("attention_mask", "cpu", 0, np.int64, attn.shape,
                          attn.ctypes.data)
            if "position_ids" in self.names:
                io.bind_input("position_ids", "cpu", 0, np.int64, pos.shape,
                              pos.ctypes.data)
            io.bind_output("logits", "cpu", 0, rai.as_onnx_type(rai.dtype_logits),
                           logits.shape, logits.ctypes.data)
            for key in caches:
                lid = int(key.split(".")[0])
                if "conv" in key:
                    io.bind_input(f"past_conv.{lid}", "cpu", 0,
                                  rai.as_onnx_type(rai.dtype_conv_cache),
                                  caches[key].shape, caches[key].ctypes.data)
                    io.bind_output(f"present_conv.{lid}", "cpu", 0,
                                   rai.as_onnx_type(rai.dtype_conv_cache),
                                   caches[key].shape, caches[key].ctypes.data)
                else:
                    past[key] = caches[key]
                    kvb = math.prod(kv_shape) * rai.dtype_kv_cache.itemsize
                    buf = np.empty(kvb + 512, dtype=np.uint8)
                    si = -buf.ctypes.data % 512  # 512-byte alignment for the EP
                    caches[key] = buf[si:si + kvb].view(rai.dtype_kv_cache).reshape(kv_shape)
                    io.bind_input(f"past_key_values.{key}", "cpu", 0,
                                  rai.as_onnx_type(rai.dtype_kv_cache),
                                  caches[key].shape, past[key].ctypes.data)
                    io.bind_output(f"present.{key}", "cpu", 0,
                                   rai.as_onnx_type(rai.dtype_kv_cache),
                                   caches[key].shape, caches[key].ctypes.data)
            rai.ort_session.run_with_iobinding(io)

            input_ids = logits[:, -1].argmax(-1, keepdims=True)  # greedy
            attn[0][ptoks + step] = 1
            pos = pos[:, -1:] + 1
            past_seq = total
            tok = int(input_ids[0, 0])
            out_ids.append(tok)
            if tok in self.eos_ids:
                break

        text = rai.tokenizer.decode(out_ids, skip_special_tokens=True)
        return text, prompt_len, len(out_ids)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "lfm2-npu-shim/1.0"

    # --- helpers ---------------------------------------------------------
    def _send_json(self, code, obj):
        data = json.dumps(obj).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        try:
            self.wfile.write(data)
        except (BrokenPipeError, ConnectionResetError):
            pass

    # --- routes ----------------------------------------------------------
    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path.endswith("/health"):
            ready = getattr(self.server, "engine", None) is not None
            self._send_json(200 if ready else 503,
                            {"status": "ok" if ready else "loading"})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self):
        path = self.path.split("?", 1)[0]
        if not path.endswith("/chat/completions"):
            self._send_json(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", 0) or 0)
        except ValueError:
            length = 0
        if length <= 0 or length > MAX_BODY:
            self._send_json(400, {"error": "invalid content length"})
            return
        try:
            body = json.loads(self.rfile.read(length).decode("utf-8"))
        except Exception:
            self._send_json(400, {"error": "invalid json"})
            return

        messages = body.get("messages")
        if not isinstance(messages, list) or not messages:
            self._send_json(400, {"error": "messages required"})
            return
        norm = []
        for m in messages:
            if not isinstance(m, dict):
                continue
            content = m.get("content", "")
            if isinstance(content, list):  # OpenAI content-parts -> flat text
                content = "".join(p.get("text", "") for p in content
                                  if isinstance(p, dict))
            norm.append({"role": m.get("role", "user"),
                         "content": content if isinstance(content, str) else str(content)})
        if not norm:
            self._send_json(400, {"error": "messages required"})
            return

        try:
            max_tokens = int(body.get("max_tokens") or 128)
        except (TypeError, ValueError):
            max_tokens = 128
        max_tokens = max(1, min(max_tokens, MAX_GEN))
        # response_format / temperature are accepted but ignored: the NPU graph
        # cannot grammar-constrain output, and decoding is greedy. The proxy uses
        # a prompt-only profile + tolerant, fail-closed parsing for this reason.

        try:
            text, ptoks, ctoks = self.server.engine.generate(norm, max_tokens)
        except Exception as e:  # never surface raw content in the error
            self._send_json(500, {"error": "generation failed",
                                   "type": type(e).__name__})
            return

        self._send_json(200, {
            "id": "chatcmpl-npu",
            "object": "chat.completion",
            "model": body.get("model") or self.server.model_name,
            "choices": [{
                "index": 0,
                "message": {"role": "assistant", "content": text},
                "finish_reason": "stop",
            }],
            "usage": {"prompt_tokens": ptoks, "completion_tokens": ctoks,
                      "total_tokens": ptoks + ctoks},
        })

    # Metadata only: method/path/status. Never the body, prompt, or output.
    def log_message(self, fmt, *args):
        sys.stderr.write("[npu_server] %s\n" % (fmt % args))


def main():
    ap = argparse.ArgumentParser(description="OpenAI-compatible HTTP shim for LFM2 on the AMD Ryzen AI NPU")
    ap.add_argument("--model", required=True, help="path to the LFM2 *-ONNX_rai_* directory")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8792)
    ap.add_argument("--no-warmup", action="store_true", help="skip the startup warmup generation")
    args = ap.parse_args()

    # Security invariant: localhost only.
    if args.host not in ("127.0.0.1", "localhost", "::1"):
        print("[npu_server] non-localhost host refused; forcing 127.0.0.1", file=sys.stderr)
        args.host = "127.0.0.1"

    print(f"[npu_server] loading LFM2 ONNX from {args.model}", file=sys.stderr, flush=True)
    print("[npu_server] first run compiles NPU overlays; this can take minutes.", file=sys.stderr, flush=True)
    engine = LFM2Engine(args.model)
    print("[npu_server] model loaded.", file=sys.stderr, flush=True)

    if not args.no_warmup:
        try:
            engine.generate([{"role": "user", "content": "ping"}], 4)
            print("[npu_server] warmup complete.", file=sys.stderr, flush=True)
        except Exception as e:
            print(f"[npu_server] warmup failed ({type(e).__name__}); serving anyway.",
                  file=sys.stderr, flush=True)

    httpd = ThreadingHTTPServer((args.host, args.port), Handler)
    httpd.engine = engine
    httpd.model_name = os.path.basename(os.path.abspath(args.model))
    print(f"[npu_server] serving on http://{args.host}:{args.port} "
          f"(POST /v1/chat/completions, GET /health)", file=sys.stderr, flush=True)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        httpd.server_close()


if __name__ == "__main__":
    main()
