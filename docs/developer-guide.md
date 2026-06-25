# Developer Guide

This document defines the implementation architecture for Qodex.

## Product Shape

Qodex is a local coding agent that runs in a terminal. It should feel like a serious developer tool: fast startup, stable keyboard behavior, clear diffs, explicit approvals, and predictable project-local behavior.

The MVP optimizes for one local model served through `llama.cpp` using an OpenAI-compatible HTTP API. vLLM and SGLang should be optional endpoint-compatible backends.

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

Suggested commands:

```text
qodex
qodex init
qodex chat
qodex run "prompt"
qodex config
qodex config list
qodex config get <key>
qodex config set <key> <value>
qodex models check
qodex doctor
qodex skills list
qodex skills show <name>
qodex sessions list
qodex sessions resume <id>
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

The TUI should be a thin presentation layer over the agent engine. Keep agent state, storage, model calls, and tool execution independent of Bubble Tea.

### Storage

Use SQLite.

Current MVP driver:

- `modernc.org/sqlite` for pure Go builds.

Other driver options:

- `github.com/mattn/go-sqlite3` if CGO is acceptable.

Keep `modernc.org/sqlite` unless performance or compatibility requires changing later.

Suggested tables:

```sql
sessions(id, project_root, title, created_at, updated_at, model, backend)
messages(id, session_id, role, content, created_at)
tool_calls(id, session_id, message_id, name, arguments_json, status, created_at)
tool_results(id, tool_call_id, output, error, created_at)
approvals(id, tool_call_id, decision, policy, created_at)
skills(id, name, source_path, summary, enabled, loaded_at)
```

Treat the database as an event log first. Derived indexes can come later.

## Model Runtime

### Primary Backend: llama.cpp

Qodex should target the `llama.cpp` server OpenAI-compatible API as the primary runtime.

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

Do not make Ollama the recommended runtime. If support is added later, it should be through the same OpenAI-compatible endpoint abstraction and clearly marked as optional.

### Advanced Backends

vLLM and SGLang should be supported as endpoint-compatible alternatives.

The agent should not care whether the backend is `llama.cpp`, vLLM, or SGLang after configuration is loaded. It should only depend on:

- Chat completion request.
- Streaming tokens.
- Structured tool-call response format, either native or prompted.
- Model name.
- Context window settings.

### Direct Hugging Face Transformers

Direct `transformers` integration is not recommended for the main Go application because it would pull Python runtime concerns into the CLI. If needed, expose it through a small local HTTP server that implements the same OpenAI-compatible API.

## Package Layout

Suggested Go package layout:

```text
cmd/qodex/              Cobra entrypoint
internal/app/           app wiring
internal/tui/           Bubble Tea models and views
internal/agent/         agent loop and orchestration
internal/model/         OpenAI-compatible client
internal/tools/         tool registry and built-in tools
internal/skills/        skill discovery and loading
internal/context/       context assembly and compaction
internal/store/         SQLite persistence
internal/config/        config loading and defaults
internal/approval/      approval policies
internal/diff/          patch generation and application helpers
```

Keep `internal/agent` free from TUI imports.

## Agent Loop

The agent loop should be explicit and auditable:

```text
1. Receive user prompt.
2. Load project instructions and enabled skills.
3. Build model context.
4. Call local model endpoint.
5. Parse assistant response.
6. If response contains tool call:
   a. Validate tool name and arguments.
   b. Evaluate approval policy.
   c. Execute tool or ask user.
   d. Persist call and result.
   e. Append tool result to context.
   f. Continue loop.
7. If response is final text:
   a. Persist message.
   b. Render to TUI.
```

Hard limits should prevent infinite loops:

- Max tool calls per user turn.
- Max shell command runtime.
- Max output bytes per tool.
- Max modified files per turn unless approved.

## Built-In Tools

Current MVP tools:

```text
list_files
read_file
search_text
write_file
write_patch
run_command
git_status
git_diff
```

Later tools:

```text
lsp_diagnostics
lsp_definition
lsp_references
test_runner
format_files
browser_preview
```

`write_file` is useful for small complete-file writes. `write_patch` should be preferred for edits to existing files because it preserves surrounding content and is easier to review.

Tool implementations must return structured results with summarized output and raw output references when needed.

## Approval Model

Default policy:

- Reading files inside the project: allow.
- Searching files inside the project: allow.
- Writing files: ask.
- Running commands: ask.
- Network commands: ask and mark clearly.
- Commands outside project root: ask.
- Destructive commands: deny by default or require a high-friction confirmation.

The approval system should be independent of the TUI so `qodex run` can use the same policy.

## Configuration

Recommended locations:

```text
Project config: .qodex/config.toml
Project skills: .qodex/skills/
User config: ~/.config/qodex/config.toml
User skills: ~/.config/qodex/skills/
Database: .qodex/qodex.db by default for the MVP
```

User config is loaded first, then project config overrides it where explicitly set.

## Getting Started With Development

### Prerequisites

- Go 1.26+ (check `go version`)
- A local `llama.cpp` server (recommended) or any OpenAI-compatible endpoint

### Quick Start

```bash
# Build the binary
go build ./cmd/qodex

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

### Testing Without A Real Model

The test suite uses fake HTTP servers and does **not** require a running model. Run all tests with:

```bash
go test ./...
```

Run a specific package's tests:

```bash
go test ./internal/agent/... -v
go test ./internal/tools/... -v
go test ./internal/tui/... -v
```

Run a single test:

```bash
go test ./internal/agent/... -v -run TestAgentReadsFile
```

The agent tests (`internal/agent/agent_test.go`) use a `roundTripFunc` HTTP transport that returns deterministic JSON responses — no real model server is needed. See `TestAgentReadsFile` for the pattern:

```go
client := model.NewClient("http://fake.local/v1", "fake")
client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
    // Return a tool_call on first request, final answer on second
    switch r.URL.Path {
    case "/v1/chat/completions":
        return jsonResponse(chatRequest{...}), nil
    }
    return nil, fmt.Errorf("unexpected: %s", r.URL.Path)
})}
```

### Running With A Real Local Model

1. Start `llama.cpp` server with a Qwen Coder GGUF model:
   ```bash
   llama-server -m qwen2.5-coder-7b-q4_k_m.gguf --host 127.0.0.1 --port 8080
   ```

2. Verify connectivity:
   ```bash
   ./qodex doctor
   ```
   Expected output:
   ```
   Project root: /path/to/project
   Model endpoint: http://127.0.0.1:8080/v1
   Model name: qwen2.5-coder
   Runtime backend: llama.cpp
   Model endpoint: ok
   ```

3. Run the agent:
   ```bash
   ./qodex chat
   ```

### Configuring The Endpoint

Edit `.qodex/config.toml` or set values via the CLI:

```bash
./qodex config set model.base_url http://127.0.0.1:8080/v1
./qodex config set model.model qwen2.5-coder
./qodex config set runtime.temperature 0.2
./qodex config list
```

See the full config reference with `./qodex config --help`.

### Debugging Tips

**Verbose test output:**
```bash
go test ./... -v 2>&1 | head -100
```

**Race detection:**
```bash
go test -race ./...
```

**Viewing SQLite state during a session:**
```bash
sqlite3 .qodex/qodex.db
.tables
select * from sessions;
select * from messages;
select * from tool_calls;
```

**Checking what tools are registered:**
```bash
grep 'r\.add(' internal/tools/tools.go
```

**Recompiling on change (watch mode):**
```bash
go build ./cmd/qodex && ./qodex doctor
```

**Capturing full agent log output:**
The agent emits structured events via the `Observer` interface. Add a logging observer in `buildRuntime` (`cmd/qodex/main.go`) to inspect event flow:

```go
agt.SetObserver(agent.ObserverFunc(func(event agent.Event) {
    log.Printf("event: type=%s tool=%s effect=%s summary=%s", event.Type, event.Tool, event.Effect, event.Summary)
}))
```

### Common Development Workflows

**Adding a new tool:**
1. Write the executor method on `*Registry` in `internal/tools/tools.go` (or a new file).
2. Register it with `r.add(...)` in `NewRegistry`.
3. Add tests in `internal/tools/tools_test.go` using `t.TempDir()` and `json.Marshal` for args.
4. Run `go test ./internal/tools/...` to verify.

**Modifying the agent loop:**
The core loop is in `internal/agent/agent.go`, method `Run()`. Each iteration:
1. Calls `compactContext()` if approaching token limit.
2. Calls `chat()` (streaming or non-streaming).
3. Parses the response via `parseToolCallDetailed()`.
4. If it's a tool call, calls `executeTool()`.
5. If it's a final answer, returns.

**Adding a database migration:**
1. Append a `migration` struct to `var migrations` in `internal/store/migrations.go`.
2. Increment the version number.
3. Write the `CREATE TABLE` / `ALTER TABLE` SQL.
4. Add store methods in `internal/store/store.go`.

### Project Layout Reference

```text
cmd/qodex/              Cobra CLI entrypoint
internal/agent/         Agent loop, tool dispatch, approval
internal/config/        TOML config loading and validation
internal/model/         OpenAI-compatible HTTP client
internal/skills/        Skill discovery, selection, context slicing
internal/store/         SQLite persistence + migrations
internal/tools/         Tool registry and all built-in tools
internal/tui/           Bubble Tea TUI (chat, approvals, streaming)
docs/                   Documentation and roadmap
```

## Testing Strategy

Test the agent as deterministic components:

- Tool schema validation.
- Tool execution behavior with temp project fixtures.
- Skill discovery and precedence.
- Context assembly.
- Approval decisions.
- SQLite migrations.
- Model client request/response parsing using fake HTTP servers.

Avoid tests that require a real model for normal CI. Add optional integration tests for a running local `llama.cpp` endpoint.

## Development Milestones

### Milestone 1: Headless Agent

- Cobra command.
- Config loader.
- OpenAI-compatible streaming client.
- Tool registry.
- `read_file`, `search_text`, `run_command`.
- Basic prompt-to-response loop.

Status: implemented with non-streaming chat completions.

### Milestone 2: File Editing

- `write_patch`.
- Diff preview.
- Approval handling.
- Git status/diff tools.

Status: partially implemented with `write_file`, `write_patch`, approval handling, and Git tools. Diff preview remains.

### Milestone 3: TUI

- Bubble Tea chat screen.
- Streaming response rendering.
- Tool timeline.
- Approval prompt UI.

Status: chat screen, resume rendering, inline tool timeline, and in-TUI approvals are implemented. Streaming and richer diff/error panels remain.

### Milestone 4: Skills

- Skill discovery.
- `SKILL.md` loading.
- Skill selection by command and model-assisted routing.
- Skill context budgets.

Status: basic discovery, explicit prompt matching, and context loading implemented.

### Milestone 5: Persistence

- SQLite event store.
- Session resume.
- Tool result history.
- Context compaction.

Status: SQLite session/message/tool storage, session listing, and TUI resume implemented. Context compaction remains.

### Milestone 6: Advanced Backends

- vLLM endpoint profile.
- SGLang endpoint profile.
- Backend diagnostics through `qodex doctor`.
