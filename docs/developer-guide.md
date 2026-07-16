# Developer Guide

This document is the current architecture reference for Qodex.

## Overview

Qodex is a local-first coding agent written in Go. It provides:

- Cobra-based CLI commands
- a Bubble Tea terminal chat UI
- a multi-step agent loop with tool execution
- SQLite-backed session and tool-event persistence
- local/project skill discovery
- an OpenAI-compatible model client aimed at local backends

The default runtime is `llama.cpp`. `vLLM` and `SGLang` are supported through the same OpenAI-compatible interface.

## Command Surface

The current top-level command set is:

```text
qodex setup
qodex init
qodex config list|get|set
qodex run PROMPT [--session ID]
qodex chat
qodex review
qodex doctor
qodex serve start|stop|status
qodex models list|download
qodex skills list|show
qodex sessions list|resume|export
qodex reset
qodex version
qodex completion
```

## Repository Layout

```text
cmd/qodex/       CLI wiring and command entrypoints
internal/agent/  agent loop, approvals, context compaction, events
internal/config/ layered config loading, defaults, validation
internal/lsp/    JSON-RPC LSP client over stdio
internal/model/  OpenAI-compatible HTTP client and backend manager
internal/skills/ built-in + local skill discovery and routing
internal/store/  SQLite persistence and migrations
internal/tools/  tool registry, executors, diff preview, project index
internal/tui/    Bubble Tea UI
docs/            user, developer, release, and security guides
examples/        example backend configs
```

## Runtime Flow

At runtime, the stack is:

```text
CLI/TUI
  -> config.Load
  -> store.Open
  -> skills.Discover
  -> tools.NewRegistry
  -> model.NewClient
  -> agent.Run
```

The TUI adds:

```text
Bubble Tea model
  -> streamed token rendering
  -> tool timeline
  -> approval prompts
  -> resume history rendering
  -> @file autocomplete
```

## Agent Loop

The agent loop in `internal/agent` is explicit:

1. Load or create a session.
2. Select relevant skills.
3. Build the system prompt from current task, prior state, tools, and skills.
4. Call the model in either:
   - prompt mode: the model emits one JSON tool call in text
   - native mode: the client sends OpenAI-compatible `tools`
5. Validate the requested tool.
6. Apply approval policy based on tool effect.
7. Execute the tool and persist the result.
8. Feed the result back into the conversation.
9. Stop when the model returns final text or `max_steps` is reached.

The agent also:

- tracks current task, files inspected, and actions taken
- emits UI events for tool requests, approvals, failures, and completions
- compacts conversation history when approaching the configured context budget
- stores large tool outputs as artifacts in SQLite

## Configuration Model

Config is layered from:

1. built-in defaults
2. `~/.config/qodex/config.toml`
3. `.qodex/config.toml`
4. an explicit `--config` path, if provided

Key sections are:

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
auto_approve = false
write_files = "ask"
run_commands = "ask"
network = "ask"

[store]
path = ".qodex/qodex.db"

[agent]
max_steps = 12
skill_routing = "auto"
tool_calls = "prompt"
```

## Persistence

SQLite is used as an event log and session store. The current schema includes:

```sql
sessions(id, project_root, title, model, backend, created_at, updated_at)
messages(id, session_id, role, content, created_at)
tool_calls(id, session_id, name, arguments_json, status, created_at)
tool_results(id, tool_call_id, output, error, created_at)
approvals(id, session_id, tool_call_id, tool_name, kind, summary, approved, created_at)
output_artifacts(id, session_id, tool_call_id, tool_name, summary, content, content_type, size, created_at)
schema_version(version, applied_at)
```

## Tools

`internal/tools.NewRegistry` is the single source of truth for built-in tools. Tools are registered with:

- name
- description
- effect
- JSON schema
- executor function

Important tool groups:

- file inspection and edits
- git status/diff/log/review
- test and formatter helpers
- project index and LSP navigation
- language/runtime tools for Go, Node, Python, .NET, Java, Flutter, and Dart
- package and archive helpers
- Docker, QEMU, and ADB helpers

Every tool must be registered in the registry to be reachable by the agent. If you add a tool implementation without registration, it will not appear in `ToolSchemas()` or the prompt tool list.

## Skills

Skills are discovered from:

- built-in embedded skills
- `~/.config/qodex/skills`
- `.qodex/skills`

Project-local skills override user skills with the same name. A skill can include:

- `SKILL.md`
- optional `skill.toml`
- optional pre-approved scripts

`skill.toml` controls:

- triggers
- allowed tools
- context budget
- predefined scripts

The agent can select skills via keyword heuristics or model-assisted routing, depending on `agent.skill_routing`.

## Backend Notes

Backend management lives under `internal/model`.

- Linux and macOS support managed `llama.cpp` installation.
- Native Windows currently does not support automatic `llama.cpp` setup.
- `vLLM` and `SGLang` installation relies on local Python and `pip`.

The model client itself only depends on an OpenAI-compatible API, so external endpoints remain supported.

## Testing

The repo has package-level Go tests across the main subsystems:

- agent loop behavior
- config parsing and validation
- model client behavior
- skill discovery and routing
- store/migrations
- tool registration and tool behavior
- TUI update logic
- LSP client flows

Run the suite with:

```sh
go test ./...
```

## Documentation Maintenance

When behavior changes, update the user guide and any architecture/security docs in the same change. The docs in `docs/` should describe the current implementation, not proposed future plans.

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

## Cross-Platform Support

Qodex targets Linux, macOS, and Windows without CGO. Filesystem, process, signal, and shell differences are isolated in platform-specific files recognized by Go build tags.

### Architecture Pattern

Shared interfaces are declared in the platform-common file, with two implementations behind build tags:

```text
internal/tools/
    shell_unix.go         # //go:build !windows
    shell_windows.go      # //go:build windows
    tools.go              # shared registry, calls ShellCommand()

internal/model/
    stop_unix.go          # SIGKILL escalation
    stop_windows.go       # Process.Kill fallback
    process_unix.go       # SysProcAttr.Setpgid
    process_windows.go    # no-op
    manager.go            # shared server lifecycle
```

This keeps the platform-specific code colocated with the shared logic instead of scattering `runtime.GOOS` switches throughout.

### Platform Boundaries

The following OS differences are handled at the layer shown:

| Boundary | Linux / macOS | Windows |
|----------|---------------|---------|
| Signals | `signal.NotifyContext` with `SIGTERM` | `Interrupt` only |
| Child process groups | `Setpgid` for clean signal forwarding | no-op |
| Stop escalation | `SIGKILL` after `SIGTERM` | `Process.Kill()` |
| Shell execution | `sh -c` | `cmd.exe /C` |
| Path separators | `/` via `path/filepath` | `\` via `path/filepath` |
| LSP file URIs | `file:///home/user/...` | `file:///C:/Users/...` |
| Text line endings | `\n` | `\r\n` normalized to `\n` after file read |
| File permissions | `0o755` / `0o644` | `0o666` for broad native access |
| Tar symlinks | extracted | skipped (no Admin/elevated mode) |
| TTY detection | `stat` + `ModeCharDevice` | `go-isatty` |

### Adding A New Platform Boundary

1. Create `foo_unix.go` and `foo_windows.go` in the same package.
2. Put the shared call site in `foo.go`.
3. Run `go build ./...` on each OS, or use `GOOS=windows go build ./...` for a lightweight compile check.
4. Avoid `runtime.GOOS` inside non-platform-tagged files — it creates un-testable branches.

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
