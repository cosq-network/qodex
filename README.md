# Locha

Locha is a local-first coding agent CLI written in Go. It uses a terminal UI, an OpenAI-compatible local model endpoint, and a single locally hosted Qwen Coder model by default.

The intended runtime is `llama.cpp`, not Ollama. Other OpenAI-compatible backends such as vLLM and SGLang can be supported as advanced runtime options without changing the agent core.

## Current MVP

The repository currently includes a usable MVP with:

- Cobra CLI commands: `init`, `config`, `chat`, `run`, `doctor`, `skills list`, `skills show`, and `sessions list`.
- Bubble Tea terminal chat UI.
- OpenAI-compatible `/v1/chat/completions` client for `llama.cpp`.
- Prompt-based JSON tool calling for broad backend compatibility.
- Built-in tools: `list_files`, `read_file`, `search_text`, `write_file`, `write_patch`, `run_command`, `git_status`, and `git_diff`.
- Project and user skills loaded from `SKILL.md` files.
- SQLite session/tool event storage and TUI session resume.
- Approval gates for write and shell tools, with `--yes` for explicit auto-approval.

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

## Build

```sh
go build ./cmd/locha
```

This creates a local `./locha` binary.

## Runtime Shape

```text
locha
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

Locha should then point at:

```text
http://127.0.0.1:8080/v1
```

The CLI should treat this as an OpenAI-compatible base URL and should not require direct linking to llama.cpp.

## Quick Start

```sh
./locha init
./locha doctor
./locha config list
./locha run "Explain this repository structure"
./locha chat
./locha sessions list
./locha sessions resume <id>
```

For prompts that may write files or run commands:

```sh
./locha --yes run "Run the tests and fix the failing issue"
```

Without `--yes`, the one-shot CLI asks before write and shell tools. In the current MVP, the TUI denies write and shell tools unless started with `--yes`.
