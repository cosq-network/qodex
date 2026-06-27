# Qodex

Qodex is a local-first coding agent CLI written in Go. It uses a terminal UI with streaming token rendering, an OpenAI-compatible local model endpoint, and a single locally hosted Qwen Coder model by default.

The intended runtime is `llama.cpp`, but Qodex now manages backend installation and model downloads itself during `qodex setup`. Other OpenAI-compatible backends such as vLLM and SGLang can be selected as advanced runtime options without changing the agent core. Backend capability detection is performed at startup to enable streaming when supported.

## Current Status

The repository includes a fully featured coding agent with:

- Cobra CLI commands: `init`, `config`, `chat`, `run`, `review`, `doctor`, `setup`, `reset`, `serve`, `models`, `skills list`, `skills show`, `sessions list`, `sessions resume`, `sessions export`, `version`, and `completion`.
- Bubble Tea terminal chat UI with streaming token rendering, inline diff preview, spinner, error panel, and multi-line input with `@` file autocomplete.
- OpenAI-compatible `/v1/chat/completions` client with SSE streaming and capability detection.
- Prompt-based JSON tool calling with validation repair loop and optional native OpenAI `tools`/`tool_calls` support.
- Built-in tools: `list_files`, `read_file`, `search_text`, `write_file`, `write_patch`, `run_command`, `git_status`, `git_diff`, `run_script` (pre-approved skill scripts), `run_tests`, `run_formatter`, `review_changes`, `project_index`, `lsp_diagnostics`, `lsp_definition`, and `lsp_find_references`.
- Skill system: `skill.toml` metadata (triggers, allowed_tools, context_budget, scripts), model-assisted skill routing (`agent.skill_routing`), keyword/heuristic selection, section-aware context slicing, and pre-approved script policy with provenance tracking.
- SQLite session/tool event storage with WAL mode, migrations, and approval persistence.
- Session resume with full tool history reconstruction (TUI and non-interactive via `--session`).
- Approval gates for write, shell, and network tools with inline diff preview and `--yes` auto-approval.
- Planning state tracking (current task, inspected files, actions taken) in system prompt.
- Context compaction (keeps last 8 messages when approaching token limit).
- LCS-based unified diff generation for write tool previews.

## Goals

- Run as a serious terminal coding assistant, similar in interaction style to OpenCode.
- Use Go for the CLI, terminal UI, agent loop, tool execution, and persistence.
- Use Bubble Tea, Bubbles, and Lipgloss for the terminal interface.
- Use Cobra for commands and configuration entry points.
- Use SQLite for sessions, messages, tool events, approvals, and local indexes.
- Talk to a local OpenAI-compatible endpoint.
- Prefer `llama.cpp` for local single-model Qwen inference.
- Support structured tool calling with validation, approval gates, and auditable execution.
- Support skills loaded from local project and user directories.

## Non-Goals

- No cloud model dependency by default.
- No Ollama-first workflow.
- No multi-model orchestration requirement for the initial implementation.
- No hidden file writes or shell execution without explicit policy handling.

## Documentation

- [Developer Guide](docs/developer-guide.md)
- [Tool Calling And Skills](docs/tool-calling-and-skills.md)
- [User Guide](docs/user-guide.md)
- [Security Model](docs/security-model.md)
- [Roadmap](docs/roadmap.md)
- [llama.cpp Setup Guide](docs/llama-cpp-setup.md)
- [Release Management](docs/release-management.md)
- [Example Configs](examples/)

## Recommended Initial Stack

```text
Language: Go
CLI: Cobra
TUI: Bubble Tea + Bubbles + Lipgloss
Storage: SQLite
Model protocol: OpenAI-compatible Chat Completions
Primary runtime: llama.cpp server
Advanced runtimes: vLLM, SGLang
Default model family: Qwen Coder instruct
```

## System Requirements

### Minimum

These specs let you run Qodex with a 7B parameter model in Q4 quantization on CPU:

```text
OS:        Linux (x86_64/arm64), macOS 12+, Windows 10/11 (WSL2 recommended)
CPU:       4-core x86_64 with AVX2, or ARM64 (Apple Silicon / Raspberry Pi 5-class)
RAM:       8 GB
Disk:      10 GB free (for model + backend + OS overhead)
Display:   Terminal with 256-color or true-color support
```

### Optimum

These specs enable larger models (14B–32B) or faster inference with GPU acceleration:

```text
OS:        Linux, macOS 13+, Windows 11 (WSL2)
CPU:       8+ core modern x86_64 / ARM64
RAM:       32 GB+ unified or system memory
GPU:       NVIDIA RTX 3060 12 GB+ (CUDA) or Apple M1 Pro/Max/Ultra 16 GB+ unified memory
Disk:      30 GB free SSD/NVMe
Display:   True-color terminal (WezTerm, Kitty, Alacritty, iTerm2, Windows Terminal)
```

### Per-Platform Notes

#### Linux

- **Distro**: Ubuntu 22.04+, Fedora 38+, Arch, or NixOS recommended.
- **llama.cpp**: prebuilt binaries download automatically during `qodex setup`. No extra packages needed on x86_64 or arm64.
- **vLLM / SGLang**: requires Python 3.8+ and `pip`. CUDA 12.x toolkit recommended for NVIDIA GPUs.
- **Terminal**: ensure `TERM=xterm-256color` or better.

#### macOS

- **Version**: 13 Ventura or later.
- **Architecture**: Apple Silicon (M1/M2/M3/M4) preferred. Intel supported but slower for CPU inference.
- **llama.cpp**: downloaded as a universal or arm64 binary during setup.
- **vLLM**: experimental on macOS; expect CPU-only or MPS with limited performance.
- **SGLang**: best suited to Linux; macOS use is limited.

#### Windows

- **OS**: Windows 10 22H2+ or Windows 11.
- **WSL2**: recommended for best compatibility with llama.cpp prebuilt binaries and Python-based backends.
- **Native**: `qodex` runs natively, but llama.cpp CPU inference works; GPU acceleration requires CUDA-capable setup.
- **Terminal**: Windows Terminal with a Nerd Font for the best TUI experience.

### Memory Sizing By Model

| Model Size       | Min RAM (Q4) | Optimum RAM        |
|------------------|--------------|--------------------|
| 1.5B – 3B       | 4 GB         | 8 GB               |
| 7B               | 6 GB         | 16 GB              |
| 14B              | 10 GB        | 24 GB              |
| 32B              | 18 GB        | 32 GB+             |
| 72B+ (Q4)       | 40 GB        | 64 GB+ / 48 GB VRAM|

Use `qodex models list` and `qodex models download` inside the setup wizard to match your hardware.

### Cross-Platform Compatibility

Qodex supports Linux, macOS, and Windows. The CLI, agent loop, and SQLite storage are pure Go. Platform differences are handled at the OS interaction layer:

| Concern | Linux / macOS | Windows |
|---------|---------------|---------|
| Shell execution | `sh -c` | `cmd.exe /C` |
| Signals | `SIGTERM` / `SIGKILL` | `Interrupt` / `Process.Kill` |
| Path format | `/home/user/...` | `C:\Users\user\...` |
| TTY detection | `isatty` | `isatty` (via `go-isatty`) |
| Symlinks in archives | extracted | skipped (requires elevated privilege) |
| File modes | `0o755` / `0o644` | mapped to `0o666` for broad access |

No CGO is required. `CGO_ENABLED=0` builds are supported and tested.

See the [User Guide](docs/user-guide.md) for per-platform notes on terminal setup, WSL2, and model backends.

## Build

```sh
go build -ldflags="-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/qodex
```

This creates a local `./qodex` binary with version metadata.

To verify the build:

```sh
./qodex version
```

### Cross-platform builds

```sh
make build-all
```

Produces binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64 in `build/`.

## Install

### From source

```sh
make install
```

### Install script (from GitHub Releases)

```sh
curl -fsSL https://github.com/benoybose/qodex/raw/main/scripts/install.sh | sh
```

### Homebrew

```sh
brew install benoybose/qodex/qodex
```

## Runtime Shape

```text
qodex
  -> TUI / CLI command
  -> agent loop
  -> context builder
  -> skill loader
  -> tool registry
  -> local OpenAI-compatible model endpoint
      -> llama.cpp server
      -> Qwen Coder model
  -> validated tool executor
  -> SQLite event/session store
```

## Example Local Model Endpoint

The exact command depends on the installed `llama.cpp` build and model file, but the intended setup is:

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 32768
```

Qodex should then point at:

```text
http://127.0.0.1:8080/v1
```

The CLI should treat this as an OpenAI-compatible base URL and should not require direct linking to llama.cpp.

## Quick Start

```sh
./qodex version
./qodex setup
./qodex doctor
./qodex config list
./qodex run "Explain this repository structure"
./qodex chat
./qodex serve status
./qodex models list
./qodex sessions list
./qodex sessions resume <id>
./qodex sessions export <id>
./qodex reset
./qodex reset --all
./qodex completion bash > /tmp/qodex-completion.sh
```

Run `qodex` without arguments for the first time to trigger the interactive setup wizard, which will:
1. Choose a backend (llama.cpp, vLLM, or SGLang)
2. Install the backend automatically
3. Select and download a model
4. Start the model server
5. Create project configuration

For prompts that may write files or run commands:

```sh
./qodex --yes run "Run the tests and fix the failing issue"
```

Without `--yes`, the one-shot CLI asks before write, shell, and network tools. In chat mode, Qodex shows approval requests inline with a diff preview; press `y` to approve or `n` to deny. Model responses are rendered token-by-token via SSE streaming when the backend supports it.
