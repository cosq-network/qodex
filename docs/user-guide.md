# User Guide

Qodex is a local terminal coding assistant. It runs against a locally hosted Qwen Coder model through `llama.cpp`.

## What You Need

- A working Go-built `qodex` binary.
- A `llama.cpp` server binary.
- A local Qwen Coder GGUF model file.
- A terminal with true color support.
- `ripgrep` and `git` installed for the best experience.

## Recommended Model

For serious coding work, start with a Qwen Coder instruct model at 7B or larger if your machine can run it.

Suggested practical tiers:

```text
Low memory: Qwen2.5-Coder 1.5B or 3B, quantized
Recommended floor: Qwen2.5-Coder 7B, quantized
Better quality: Qwen2.5-Coder 14B, quantized
```

Smaller models are useful for testing the app, but they will make more mistakes during multi-step coding tasks.

## Start llama.cpp

Example:

```sh
llama-server \
  --model ./models/qwen2.5-coder-7b-instruct-q4_k_m.gguf \
  --host 127.0.0.1 \
  --port 8080 \
  --ctx-size 32768
```

The expected API base URL is:

```text
http://127.0.0.1:8080/v1
```

Qodex uses the OpenAI-compatible API exposed by `llama.cpp`.

## Configure Qodex

Create starter project configuration and a project skill:

```sh
qodex init
```

If you built locally from this repository, use:

```sh
./qodex init
```

Project-local config should live at:

```text
.qodex/config.toml
```

Example:

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

[store]
path = ".qodex/qodex.db"
```

Inspect effective config:

```sh
qodex config list
qodex config get model.base_url
```

Update project-local config:

```sh
qodex config set runtime.temperature 0.1
qodex config set model.base_url http://127.0.0.1:8080/v1
```

## LSP Tools (Diagnostics, Definitions, References)

Qodex ships with three language-server-powered tools that provide precise code analysis. They communicate with a running LSP server (e.g. `gopls`) over JSON-RPC 2.0 via stdio.

| Tool | Description |
|------|-------------|
| `lsp_diagnostics` | Get errors, warnings, and hints for a file. Accepts `path`. |
| `lsp_definition` | Go to the definition of a symbol. Accepts `path`, `line`, `character`. |
| `lsp_find_references` | Find all references to a symbol. Accepts `path`, `line`, `character`. |

### Supported LSP Servers

| Language | Server | Install |
|----------|--------|---------|
| Go | `gopls` | `go install golang.org/x/tools/gopls@latest` |
| Python | `pyright-langserver` | `pip install pyright` or `npm install -g pyright` |
| JavaScript / TypeScript | `typescript-language-server` | `npm install -g typescript-language-server` |
| Rust | `rust-analyzer` | `rustup component add rust-analyzer` |

If the LSP server is not installed, Qodex returns a clear error with installation instructions. The tools are marked as `read` effect and do not require approval.

## Start A Chat Session

From a project directory:

```sh
qodex chat
```

For a one-shot prompt:

```sh
qodex run "Find the failing tests and suggest a fix"
```

If you built locally from this repository, use `./qodex` instead of `qodex`.

For prompts that need file writes or shell commands:

```sh
qodex --yes run "Run the Go tests and fix the failing test"
```

Without `--yes`, `qodex run` asks before writes, shell commands, and network commands. In chat mode, Qodex shows approval requests inline; press `y` to approve or `n` to deny.

## Common Prompts

```text
Explain this repository structure.
```

```text
Find where authentication is implemented.
```

```text
Run the Go tests and fix the failing test.
```

```text
Add validation for empty project names and include tests.
```

```text
Review my uncommitted changes.
```

## Tool Approvals

Qodex can ask to use tools. Read-only tools may run automatically. Riskier actions require approval.

Usually automatic:

```text
list files
read files
search text
git status
git diff
project index queries
LSP diagnostics, definitions, and references
```

Usually requires approval:

```text
write file patches
run shell commands
run tests
install dependencies
use network commands
```

Review command text and file change details before approving them. A richer diff viewer is planned.

## Skills

Skills are local instruction bundles that teach Qodex project or workflow conventions.

Project skills live in:

```text
.qodex/skills/
```

User-wide skills live in:

```text
~/.config/qodex/skills/
```

Example:

```text
.qodex/skills/go-testing/
  skill.toml
  SKILL.md
```

You can ask Qodex to use a skill explicitly:

```text
/skill go-testing
Run the failing tests and fix the issue.
```

## Sessions

Qodex keeps MVP session history in SQLite at `.qodex/qodex.db` by default. You can list and resume sessions.

Expected commands:

```sh
qodex sessions list
qodex sessions resume <id>
```

## Troubleshooting

### Qodex Cannot Connect To The Model

Check that `llama.cpp` is running:

```sh
curl http://127.0.0.1:8080/v1/models
```

Then verify `.qodex/config.toml` has the same host and port.

### Responses Are Weak Or Confused

Use a larger model if possible. For coding-agent workflows, 7B is a more realistic starting point than 1.5B.

Also try:

```toml
[runtime]
temperature = 0.1
context_tokens = 32768
```

### Tool Calls Fail Often

This can happen with smaller models. Qodex should retry malformed tool calls, but model quality still matters. Use a larger Qwen Coder model or a backend with better structured-output behavior.

### The Agent Wants To Run A Risky Command

Decline the approval and ask it to explain the command or use a safer approach.

Example:

```text
Do not run that command. Explain why it is needed and propose a narrower command.
```

## Privacy Model

With the default `llama.cpp` setup, prompts, code, and tool results stay on your machine. This assumes:

- The configured model endpoint is local.
- Network-like commands are reviewed through approval prompts.
- Skills and scripts are local and reviewed.

If you configure a remote OpenAI-compatible endpoint, your data may leave your machine.

See [Security Model](security-model.md) for the detailed tool and approval boundaries.
