# Qodex

Qodex is a local-first coding agent CLI written in Go. It uses a terminal UI with streaming token rendering, an OpenAI-compatible local model endpoint, and a single locally hosted Qwen Coder model by default.

The intended runtime is `llama.cpp`, not Ollama. Other OpenAI-compatible backends such as vLLM and SGLang can be supported as advanced runtime options without changing the agent core. Backend capability detection is performed at startup to enable streaming when supported.

## Current Status

The repository includes a fully featured coding agent with:

- Cobra CLI commands: `init`, `config`, `chat`, `run`, `doctor`, `skills list`, `skills show`, `sessions list`, `sessions resume`, and `sessions export`.
- Bubble Tea terminal chat UI with streaming token rendering, inline diff preview, spinner, error panel, and multi-line input with `@` file autocomplete.
- OpenAI-compatible `/v1/chat/completions` client with SSE streaming and capability detection.
- Prompt-based JSON tool calling with validation repair loop.
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
go build ./cmd/qodex
```

This creates a local `./qodex` binary.

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
./qodex init
./qodex doctor
./qodex config list
./qodex run "Explain this repository structure"
./qodex chat
./qodex sessions list
./qodex sessions resume <id>
./qodex sessions export <id>
```

For prompts that may write files or run commands:

```sh
./qodex --yes run "Run the tests and fix the failing issue"
```

Without `--yes`, the one-shot CLI asks before write, shell, and network tools. In chat mode, Qodex shows approval requests inline with a diff preview; press `y` to approve or `n` to deny. Model responses are rendered token-by-token via SSE streaming when the backend supports it.
