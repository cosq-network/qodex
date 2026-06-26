# llama.cpp Model Setup Guide

This guide covers recommended GGUF variants and `llama-server` flags for running Qwen Coder models as a Qodex backend.

## Recommended Models

The Qwen2.5 Coder family is the default model family for Qodex. Below are per-tier recommendations.

| Tier | Model | Quantization | RAM | Speed |
|------|-------|-------------|-----|-------|
| Entry | Qwen2.5-Coder-1.5B-Instruct | Q4_K_M | ~2 GB | Fast |
| Minimum viable | Qwen2.5-Coder-3B-Instruct | Q4_K_M | ~3 GB | Fast |
| Recommended | Qwen2.5-Coder-7B-Instruct | Q4_K_M | ~6 GB | Good |
| Better | Qwen2.5-Coder-7B-Instruct | Q8_0 | ~8 GB | Good |
| High quality | Qwen2.5-Coder-14B-Instruct | Q4_K_M | ~10 GB | Moderate |
| Maximum | Qwen2.5-Coder-32B-Instruct | Q4_K_M | ~20 GB | Slow on consumer HW |

### Where To Download

Gather models from Hugging Face — for example:

```
https://huggingface.co/Qwen
https://huggingface.co/bartowski?search_models=qwen2.5-coder
```

GGUF-quantized versions from community creators like `bartowski` or `maziyarpanahi` are the most convenient.

## llama-server Flags

The `llama-server` binary is built from the [llama.cpp](https://github.com/ggml-org/llama.cpp) repository.

### Minimal

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080
```

### With Context Length (Recommended)

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 32768
```

The `--ctx-size` flag should match `runtime.context_tokens` in Qodex's config. Qwen2.5-Coder supports up to 32K context by default; use `32768` for the full window.

### Flash Attention (Faster On Apple Silicon)

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 32768 \
  --flash-attn
```

Flash attention reduces memory use and speeds up inference on Apple Silicon (M-series) and modern NVIDIA GPUs.

### GPU Offloading (NVIDIA / CUDA)

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 32768 \
  --n-gpu-layers -1
```

`--n-gpu-layers -1` offloads all layers to GPU. Set a specific number (e.g. `--n-gpu-layers 20`) to partially offload if VRAM is limited.

### Metal (Apple Silicon)

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 32768 \
  --metal
```

### Low Memory

```sh
llama-server \
  --model ./models/qwen2.5-coder-1.5b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 8192
```

Reduce `--ctx-size` to 8192 if memory is tight. In Qodex, set `runtime.context_tokens = 8192` to match.

## Build llama.cpp

```sh
git clone https://github.com/ggml-org/llama.cpp
cd llama.cpp
cmake -B build
cmake --build build --config Release --target llama-server
```

The binary is at `build/bin/llama-server`.

## Verify The Endpoint

Once the server is running, Qodex can check connectivity:

```sh
qodex doctor
```

Or manually:

```sh
curl http://127.0.0.1:8080/v1/models
```

Expected response includes the model name — for example:

```json
{"object":"list","data":[{"id":"qwen2.5-coder-7b-instruct-q4_k_m","object":"model"}]}
```

## Temperature And Top-P Guidance

| Use case | Temperature | Top-P |
|----------|-------------|-------|
| Code generation | 0.1 – 0.2 | 0.95 |
| Debugging / analysis | 0.2 – 0.3 | 0.95 |
| Creative / brainstorming | 0.4 – 0.7 | 0.90 |
| Maximum precision | 0.0 – 0.1 | 1.00 |

Lower temperatures reduce hallucination but may make the model less willing to explore alternative approaches. For coding-agent tasks, 0.2 is a good default.
