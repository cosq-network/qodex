# Developer Guide

This document defines the implementation architecture for Qodex.

## Product Shape

Qodex is a local coding agent that runs in a terminal. It should feel like a serious developer tool: fast startup, stable keyboard behavior, clear diffs, explicit approvals, and predictable project-local behavior.

The MVP optimizes for one local model served through `llama.cpp` using an OpenAI-compatible HTTP API. vLLM and SGLang are supported as optional endpoint-compatible backends.

## Technology Choices

### Language

Use Go.

Reasons:

- Good terminal app performance.
- Simple static binary distribution.
- Strong process, filesystem, and concurrency primitives.
- Mature TUI ecosystem through Charm libraries.
- Easier operational packaging than Python for an end-user CLI.

### CLI

Use `spf13/cobra`.

Full command tree:

```text
qodex
qodex setup              Interactive first-time setup wizard
qodex init               Create project-local config and starter skill
qodex chat               Start the terminal chat UI
qodex run PROMPT         Run a one-shot agent prompt
qodex review             Review uncommitted changes
qodex config list        List effective configuration
qodex config get KEY     Show one configuration value
qodex config set KEY VAL Set a project-local configuration value
qodex doctor             Check configuration and local model connectivity
qodex skills list        List discovered skills
qodex skills show NAME   Show a skill
qodex sessions list      List recent sessions
qodex sessions resume ID Resume a session in the TUI
qodex sessions export ID Export session data as JSON
qodex version            Print version and build metadata
qodex completion SHELL   Generate shell completion scripts
```

### Terminal UI

Use:

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`
- `github.com/charmbracelet/glamour` for Markdown rendering

Primary TUI regions:

```text
┌─────────────────────────────────────────────┐
│ Session header: project, model, backend      │
├─────────────────────────────────────────────┤
│ Conversation and tool timeline               │
│ - assistant messages                         │
│ - tool calls                                 │
│ - command output                             │
│ - file diffs                                 │
├─────────────────────────────────────────────┤
│ Approval / status / diagnostics panel        │
├─────────────────────────────────────────────┤
│ Prompt input                                 │
└─────────────────────────────────────────────┘
```

The TUI is a thin presentation layer over the agent engine. Agent state, storage, model calls, and tool execution are independent of Bubble Tea.

### Storage

Use SQLite via `modernc.org/sqlite` (pure Go, no CGO).

Key tables:

```sql
sessions(id, project_root, title, created_at, updated_at, model, backend)
messages(id, session_id, role, content, created_at)
tool_calls(id, session_id, message_id, name, arguments_json, status, created_at)
tool_results(id, tool_call_id, output, error, created_at)
approvals(id, tool_call_id, decision, policy, created_at)
output_artifacts(id, session_id, tool_call_id, tool_name, summary, content, content_type, created_at)
schema_version(version, applied_at)
```

The database is treated as an event log first. Derived indexes can come later.

## Model Runtime

### Primary Backend: llama.cpp

Default configuration:

```toml
[model]
provider = "openai-compatible"
base_url = "http://127.0.0.1:8080/v1"
model = "qwen2.5-coder"

[runtime]
backend = "llama.cpp"
context_tokens = 32768
temperature = 0.2
top_p = 0.95
```

See [llama.cpp Setup Guide](llama-cpp-setup.md) for recommended model tiers and server flags.

### Advanced Backends

vLLM and SGLang are supported as endpoint-compatible alternatives. See the example configs in `examples/`.

The agent does not care whether the backend is `llama.cpp`, vLLM, or SGLang after configuration is loaded. It depends only on:

- Chat completion request.
- Streaming tokens.
- Structured tool-call response format, either prompt-based or native `tools`/`tool_calls`.
- Model name.
- Context window settings.

### Capability Detection

At startup (TUI mode), the client pings the backend with a streaming probe (`POST /chat/completions` with `stream=true`) to detect whether SSE streaming is supported. The result controls whether the TUI renders tokens incrementally.

## Package Layout

```text
cmd/qodex/              Cobra entrypoint
internal/agent/         Agent loop and orchestration
internal/config/        Config loading, validation, defaults
internal/lsp/           JSON-RPC 2.0 LSP client over stdio
internal/model/         OpenAI-compatible HTTP client + types
internal/skills/        Skill discovery, selection, context slicing
internal/store/         SQLite persistence and migrations
internal/tools/         Tool registry and all built-in tools
internal/tui/           Bubble Tea models and views
```

`internal/agent` is free of TUI imports.

## Agent Loop

The agent loop is explicit and auditable:

```text
1. Receive user prompt.
2. Load project instructions and enabled skills.
3. Build model context (system prompt + conversation history).
4. Select tool-calling mode:
   a. Prompt mode (default): instruct model via system prompt to emit JSON tool calls.
   b. Native mode (opt-in, agent.tool_calls = "native"): send OpenAI tools/tool_calls.
5. Call local model endpoint (streaming or non-streaming).
6. Parse assistant response:
   a. If native mode: extract tool_calls from structured response.
   b. If prompt mode: extract tool_call JSON from text response.
7. If response contains tool call(s):
   a. Validate tool name and arguments.
   b. Evaluate approval policy (read → auto, write/shell → ask).
   c. Execute tool.
   d. Persist call and result.
   e. Append tool result to context.
   f. Continue loop (up to max_steps).
8. If response is final text:
   a. Persist message.
   b. Return to TUI or CLI caller.
```

Hard limits:

- Max tool calls per user turn (`agent.max_steps`, default 12).
- Max shell command runtime (120s default, max 300s).
- Max output bytes per tool (20000 characters, with artifact fallback for larger output).
- Max modified files per turn unless approved.

## Built-In Tools

16 tools registered in `internal/tools/tools.go`:

| Tool | Effect | Description |
|---|---|---|
| `list_files` | read | List files under project root |
| `read_file` | read | Read a UTF-8 text file with optional line range |
| `search_text` | read | Text search in project files |
| `write_file` | write | Write a complete file |
| `write_patch` | write | Apply a unified diff via `git apply` |
| `run_command` | shell | Run a command in project root |
| `run_script` | shell | Run a pre-approved skill script |
| `run_tests` | shell | Discover and run tests (go/pytest/jest) |
| `run_formatter` | shell | Run a code formatter (go/ruff/black/prettier) |
| `git_status` | read | Show git status |
| `git_diff` | read | Show git diff |
| `review_changes` | read | Analyze uncommitted git changes |
| `project_index` | read | Query file/symbol index or get project summary |
| `lsp_diagnostics` | read | Run LSP to get diagnostics for a file |
| `lsp_definition` | read | Go to definition via LSP |
| `lsp_find_references` | read | Find references via LSP |

`write_file` is useful for small complete-file writes. `write_patch` (unified diff) is preferred for edits to existing files because it preserves surrounding content and is easier to review.

Tool implementations return structured `Result` objects with summary, content, and optional metadata. Large outputs (>64KB) are stored as artifacts and replaced with a summary reference in the context.

## Tool Calling Modes

### Prompt Mode (default)

The system prompt instructs the model to emit a JSON object when it wants a tool:

```json
{"tool_call":{"name":"read_file","arguments":{"path":"README.md"}}}
```

The agent parses the response with `parseToolCallDetailed`, which handles markdown fences and embedded JSON. Validation errors are fed back to the model for self-repair.

### Native Mode (`agent.tool_calls = "native"`)

The model receives tool schemas via the OpenAI `tools` parameter and returns structured `tool_calls` in the response. The agent dispatches each `tool_call` and appends results as `role: "tool"` messages. Streaming is disabled in native mode.

## LSP Integration

The `internal/lsp` package implements a JSON-RPC 2.0 client over stdio, supporting:

- `gopls` (Go)
- `pyright-langserver` (Python)
- `typescript-language-server` (TypeScript/JavaScript)
- `rust-analyzer` (Rust)

The client sends `textDocument/didOpen` with file content, then supports `textDocument/documentDiagnostics`, `textDocument/definition`, and `textDocument/references`. The server process is started per-tool-call (initialize → request → shutdown).

## Approval Model

Default policy:

- Reading files inside the project: allow (read effect).
- Searching files inside the project: allow.
- Writing files: ask (write effect).
- Running commands: ask (shell effect).
- Network commands: ask (network effect).
- Commands outside project root: ask.
- Destructive commands: deny by default (rejected before approval).

The approval system is independent of the TUI so `qodex run` can use the same policy (stdin prompt) as `qodex chat` (inline TUI panel).

## Configuration

Locations:

```text
Project config: .qodex/config.toml
Project skills: .qodex/skills/
User config: ~/.config/qodex/config.toml
User skills: ~/.config/qodex/skills/
Database: .qodex/qodex.db
```

User config is loaded first, then project config overrides it where explicitly set.

Full config reference (see `internal/config/config.go`):

```toml
[model]
provider = "openai-compatible"
base_url = "http://127.0.0.1:8080/v1"
model = "qwen2.5-coder"

[runtime]
backend = "llama.cpp"
context_tokens = 32768
temperature = 0.2
top_p = 0.95

[approval]
write_files = "ask"
run_commands = "ask"
network = "ask"
auto_approve = false

[store]
path = ".qodex/qodex.db"

[agent]
max_steps = 12
skill_routing = "auto"
tool_calls = "prompt"
```

## Getting Started With Development

### Prerequisites

- Go 1.26+ (check `go version`)
- A local `llama.cpp` server (recommended) or any OpenAI-compatible endpoint
- For LSP tools: `gopls`, `pyright`, `typescript-language-server`, or `rust-analyzer` as needed

### Quick Start

```bash
# Build the binary with version metadata
go build -ldflags="-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/qodex

# List all available commands
./qodex --help

# Run the doctor to verify config and endpoint connectivity
./qodex doctor

# Start the interactive TUI chat
./qodex chat

# Run a one-shot prompt
./qodex run "List all Go files in this project"

# Review uncommitted changes
./qodex review
```

### Testing

The test suite uses fake HTTP servers and does **not** require a running model.

```bash
# Full suite with race detector
go test -race ./...

# Single package
go test ./internal/agent/... -v

# Single test
go test ./internal/tools/... -v -run TestToolSchemas
```

The agent tests use a `roundTripFunc` HTTP transport that returns deterministic JSON responses.

### Manual Local Test Scenarios

After building locally, run these checks to verify behavior end to end.

**CLI smoke tests**

```bash
./qodex version
./qodex doctor
./qodex config list
./qodex skills list
./qodex sessions list
```

**One-shot agent prompt**

This validates tool dispatch, shell approval, and result rendering without the TUI:

```bash
./qodex run "list Go files then show the first 5 lines of go.mod"
```

**Review changed files**

```bash
./qodex review
```

**TUI session**

```bash
./qodex chat
```

In the TUI, verify:
- Token streaming appears incrementally.
- File diff preview renders.
- Approval prompts show the changed content summary.
- `q` or `Ctrl+C` exits cleanly.

**Local model endpoint tests**

```bash
# Start llama-server in another terminal
llama-server -m ./models/qwen2.5-coder-7b-q4_k_m.gguf \
  --host 127.0.0.1 --port 8080 --ctx-size 32768

# Check connectivity
./qodex doctor

# One-shot against the real model
./qodex run "Explain this repository in three sentences"

# Chat with streaming
./qodex chat
```

If `doctor` shows timing out or unreachable, check host, port, context size, and model path.

**LSP tool checks**

```bash
# Ensure gopls is installed
go install golang.org/x/tools/gopls@latest

# Open a Go file scope and ask for diagnostics
./qodex run "run lsp diagnostics on ./internal/agent/agent.go"
```

**Session and database checks**

```bash
./qodex sessions list
sqlite3 .qodex/qodex.db "select id, title, model, updated_at from sessions order by updated_at desc limit 5;"
```

**Approval behavior**

```bash
# Answered prompts should appear for write/shell tools
./qodex run "write a temp file to /tmp/qodex-test and then run ls -la /tmp"
```

With `--yes`, approvals are skipped:

```bash
./qodex --yes run "run ls -la"
```

```go
client := model.NewClient("http://fake.local/v1", "fake")
client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
    switch r.URL.Path {
    case "/v1/chat/completions":
        return jsonResponse(map[string]interface{}{"choices": ...}), nil
    }
    return nil, fmt.Errorf("unexpected: %s", r.URL.Path)
})}
```

### Running With A Real Local Model

1. Start `llama.cpp` server:
   ```bash
   llama-server -m qwen2.5-coder-7b-q4_k_m.gguf --host 127.0.0.1 --port 8080 --ctx-size 32768
   ```

2. Verify connectivity:
   ```bash
   ./qodex doctor
   ```

3. Run the agent:
   ```bash
   ./qodex chat
   ```

### Debugging Tips

```bash
# Race detection
go test -race ./...

# View SQLite state during a session
sqlite3 .qodex/qodex.db
.tables
select * from sessions;
select * from messages;

# List registered tools
grep 'r\.add(' internal/tools/tools.go
```

The agent emits structured events via the `Observer` interface. Add a logging observer in `buildRuntime` (`cmd/qodex/main.go`) to inspect event flow:

```go
agt.SetObserver(agent.ObserverFunc(func(event agent.Event) {
    log.Printf("event: type=%s tool=%s effect=%s summary=%s", event.Type, event.Tool, event.Effect, event.Summary)
}))
```

### Common Development Workflows

**Adding a new tool:**
1. Write the executor method on `*Registry` in a new file under `internal/tools/`.
2. Register it with `r.add(...)` in `NewRegistry`.
3. Add tests using `t.TempDir()` and `json.Marshal` for args.
4. Run `go test ./internal/tools/...` to verify.

**Modifying the agent loop:**
The core loop is in `internal/agent/agent.go`, method `Run()`. Each iteration:
1. Calls `compactContext()` if approaching token limit.
2. Calls `chat()` (prompt mode) or `chatWithTools()` (native mode).
3. If native mode: dispatches `tool_calls` from structured response.
4. If prompt mode: parses tool call via `parseToolCallDetailed()`.
5. Executes the tool via `executeTool()`.
6. Returns final answer when no tool call is detected.

**Adding a new config key:**
1. Add the field to the appropriate struct in `internal/config/config.go`.
2. Set the default in `Defaults()`.
3. Add validation in `Validate()`.
4. Add to `Values()` and `Get()`.
5. Add a test in `internal/config/config_test.go`.

**Adding a database migration:**
1. Append a `migration` struct to `var migrations` in `internal/store/store.go`.
2. Increment the version number.
3. Write the `CREATE TABLE` / `ALTER TABLE` SQL.
4. Add store methods for the new table.

### Project Layout Reference

```text
cmd/qodex/              Cobra CLI entrypoint
internal/agent/         Agent loop, tool dispatch, approval
internal/config/        TOML config loading and validation
internal/lsp/           JSON-RPC 2.0 LSP client (gopls, pyright, etc.)
internal/model/         OpenAI-compatible HTTP client + types
internal/skills/        Skill discovery, selection, context slicing
internal/store/         SQLite persistence + migrations
internal/tools/         Tool registry and all built-in tools
internal/tui/           Bubble Tea TUI (chat, approvals, streaming)
docs/                   Documentation, roadmap, and guides
contrib/homebrew/       Homebrew formula
scripts/                Install script
.github/workflows/      CI + release workflows
```

## Testing Strategy

Test the agent as deterministic components:

- Tool schema validation and execution behavior with temp project fixtures.
- Skill discovery and precedence.
- Config loading, validation, and TOML parsing.
- SQLite migrations and CRUD operations.
- Model client request/response parsing using fake HTTP servers.
- Agent loop with fake model responses (both prompt and native tool call modes).
- LSP client with gopls integration tests.
- TUI approval key handling and auto-approve.

Avoid tests that require a real model for normal CI. Integration tests for a running local `llama.cpp` endpoint are optional.

## Release Process

See [Release Management](release-management.md) for the full process:

1. GPG key setup and GitHub secrets configuration.
2. Tagging with `git tag -s` (signed tags).
3. CI release via GoReleaser on `v*` tags.
4. Artifact signing (`.sig` files).
5. Homebrew formula SHA-256 update.
6. Install script verification.
7. Rollback procedures.
