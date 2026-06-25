# User Guide

Locha is a local terminal coding assistant. It runs against a locally hosted Qwen Coder model through `llama.cpp`.

## What You Need

- A working Go-built `locha` binary.
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

Locha uses the OpenAI-compatible API exposed by `llama.cpp`.

## Configure Locha

Create starter project configuration and a project skill:

```sh
locha init
```

If you built locally from this repository, use:

```sh
./locha init
```

Project-local config should live at:

```text
.locha/config.toml
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
path = ".locha/locha.db"
```

Inspect effective config:

```sh
locha config list
locha config get model.base_url
```

Update project-local config:

```sh
locha config set runtime.temperature 0.1
locha config set model.base_url http://127.0.0.1:8080/v1
```

## Start A Chat Session

From a project directory:

```sh
locha chat
```

For a one-shot prompt:

```sh
locha run "Find the failing tests and suggest a fix"
```

If you built locally from this repository, use `./locha` instead of `locha`.

For prompts that need file writes or shell commands:

```sh
locha --yes run "Run the Go tests and fix the failing test"
```

Without `--yes`, `locha run` asks before writes and shell commands. In the current MVP, the TUI denies writes and shell commands unless started with `--yes`.

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

Locha can ask to use tools. Read-only tools may run automatically. Riskier actions require approval.

Usually automatic:

```text
list files
read files
search text
git status
git diff
```

Usually requires approval:

```text
write file patches
run shell commands
run tests
install dependencies
use network commands
```

Review command text and file diffs before approving them.

## Skills

Skills are local instruction bundles that teach Locha project or workflow conventions.

Project skills live in:

```text
.locha/skills/
```

User-wide skills live in:

```text
~/.config/locha/skills/
```

Example:

```text
.locha/skills/go-testing/
  skill.toml
  SKILL.md
```

You can ask Locha to use a skill explicitly:

```text
/skill go-testing
Run the failing tests and fix the issue.
```

## Sessions

Locha keeps MVP session history in SQLite at `.locha/locha.db` by default. You can list and resume sessions.

Expected commands:

```sh
locha sessions list
locha sessions resume <id>
```

## Troubleshooting

### Locha Cannot Connect To The Model

Check that `llama.cpp` is running:

```sh
curl http://127.0.0.1:8080/v1/models
```

Then verify `.locha/config.toml` has the same host and port.

### Responses Are Weak Or Confused

Use a larger model if possible. For coding-agent workflows, 7B is a more realistic starting point than 1.5B.

Also try:

```toml
[runtime]
temperature = 0.1
context_tokens = 32768
```

### Tool Calls Fail Often

This can happen with smaller models. Locha should retry malformed tool calls, but model quality still matters. Use a larger Qwen Coder model or a backend with better structured-output behavior.

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
