# User Guide

Qodex is a local terminal coding assistant. It runs against a locally hosted Qwen Coder model through `llama.cpp`, `vLLM`, or `SGLang`.

## What You Need

- A working Go-built `qodex` binary.
- A terminal with true color support.
- `ripgrep` and `git` installed for the best experience.

Qodex manages backend installation and model downloads automatically. Run `qodex` or `qodex setup` for an interactive wizard that handles everything.

## System Requirements

### Minimum

Run a 7B Q4 model on CPU:

- **OS**: Linux (x86_64/arm64), macOS 12+, Windows 10/11 (WSL2 recommended)
- **CPU**: 4-core x86_64 with AVX2, or ARM64 (Apple Silicon / Raspberry Pi 5-class)
- **RAM**: 8 GB
- **Disk**: 10 GB free
- **Terminal**: 256-color or true-color capable

### Optimum

Larger models (14B–32B) or GPU-accelerated inference:

- **OS**: Linux, macOS 13+, Windows 11 (WSL2)
- **CPU**: 8+ core modern x86_64 / ARM64
- **RAM**: 32 GB+ unified or system memory
- **GPU**: NVIDIA RTX 3060 12 GB+ (CUDA) or Apple M1 Pro/Max/Ultra 16 GB+ unified memory
- **Disk**: 30 GB free SSD/NVMe
- **Terminal**: true-color terminal (WezTerm, Kitty, Alacritty, iTerm2, Windows Terminal)

### Per-Platform Notes

#### Linux

- **Distro**: Ubuntu 22.04+, Fedora 38+, Arch, or NixOS recommended.
- **llama.cpp**: prebuilt binaries download automatically during `qodex setup`.
- **vLLM / SGLang**: requires Python 3.8+ and `pip`. CUDA 12.x toolkit recommended for NVIDIA GPUs.
- **Terminal**: ensure `TERM=xterm-256color` or better.

#### macOS

- **Version**: 13 Ventura or later. Apple Silicon (M1/M2/M3/M4) preferred; Intel supported but slower.
- **llama.cpp**: arm64 or universal binary downloaded during setup.
- **vLLM**: experimental on macOS; expect CPU-only or MPS with limited performance.
- **SGLang**: best suited to Linux; macOS use is limited.

#### Windows

- **OS**: Windows 10 22H2+ or Windows 11.
- **WSL2**: recommended for best compatibility with llama.cpp binaries and Python-based backends.
- **Native**: `qodex` runs natively, but GPU acceleration requires a CUDA-capable setup.
- **Terminal**: Windows Terminal with a Nerd Font for the best TUI experience.

### Memory Sizing By Model

| Model Size       | Min RAM (Q4) | Optimum RAM        |
|------------------|--------------|--------------------|
| 1.5B – 3B       | 4 GB         | 8 GB               |
| 7B               | 6 GB         | 16 GB              |
| 14B              | 10 GB        | 24 GB              |
| 32B              | 18 GB        | 32 GB+             |
| 72B+ (Q4)       | 40 GB        | 64 GB+ / 48 GB VRAM |

Use `qodex models list` and `qodex models download` to pick a model matching your hardware.

## Recommended Model

For serious coding work, start with a Qwen Coder instruct model at 7B or larger if your machine can run it.

Suggested practical tiers:

```text
Low memory: Qwen2.5-Coder 1.5B or 3B, quantized
Recommended floor: Qwen2.5-Coder 7B, quantized
Better quality: Qwen2.5-Coder 14B, quantized
```

Smaller models are useful for testing the app, but they will make more mistakes during multi-step coding tasks.

## Setup

Run `qodex` without arguments for the first time, or run `qodex setup` explicitly, to start the interactive setup wizard:

1. **Choose Backend** — Select `llama.cpp` (default), `vLLM`, or `SGLang`
2. **Install Backend** — Qodex downloads and installs the backend binaries automatically
3. **Choose Model** — Select from available Qwen Coder models
4. **Start Server** — Qodex starts the model server in the background
5. **Create Config** — Writes `.qodex/config.toml` and a starter project skill

After setup, use these commands to manage the model server:

```sh
qodex serve start      # Start the model server
qodex serve status     # Check if the server is running
qodex serve stop       # Stop the model server
qodex models list      # List available models
qodex models download <model-name>  # Download a model
```

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

## Built-in Tools

Qodex ships with a broad set of built-in tools covering development workflows across many languages, platforms, and system administration tasks.

| Category | Tools |
|----------|-------|
| **File & Code** | `list_files`, `read_file`, `search_text`, `write_file`, `write_patch`, `run_command`, `run_script`, `run_tests`, `run_formatter`, `review_changes`, `project_index` |
| **Git** | `git_status`, `git_diff`, `git_log` |
| **Build** | `cmake_configure`, `cmake_build`, `clang_format`, `clang_tidy`, `make_build`, `nmake_build`, `msbuild` |
| **Network** | `curl`, `wget` |
| **Java** | `javac_compile`, `java_run` |
| **Search & Text** | `rg_search`, `grep_search`, `find_files`, `sed_edit`, `awk_process`, `base64_encode` |
| **Node.js** | `node_run`, `npm_command`, `npx_command`, `nvm_use` |
| **.NET** | `dotnet_run`, `dotnet_build`, `dotnet_test`, `nuget_restore`, `nuget_install` |
| **Python** | `python_run`, `python3_run`, `pip_install`, `pip3_install` |
| **Conda** | `conda_install`, `conda_create` |
| **Flutter / Dart** | `flutter_run`, `flutter_build`, `flutter_test`, `dart_run`, `dart_analyze`, `dart_format`, `pub_get`, `pub_upgrade`, `pub_add`, `pub_remove` |
| **Archives** | `ar_create`, `ar_extract`, `ar_list`, `tar_create`, `tar_extract`, `tar_list`, `zip_create`, `zip_extract`, `zip_list` |
| **System** | `ps_list`, `tail_file`, `chmod_change`, `chown_change`, `user_add`, `user_del` |
| **Package Managers** | `winget_install`, `choco_install`, `apt_install`, `apt_get_install`, `snap_install`, `dnf_install`, `brew_install` |
| **Docker** | `docker_build`, `docker_run`, `docker_compose_up`, `docker_compose_down` |
| **Virtualization** | `qemu_run` |
| **Android / ADB** | `adb_devices`, `adb_shell`, `adb_push`, `adb_pull` |
| **LSP** | `lsp_diagnostics`, `lsp_definition`, `lsp_find_references` |

Each tool exposes a JSON schema so the model knows which parameters to pass. Tools are classified by effect (`read`, `write`, `shell`, `network`, `destructive`) to determine approval requirements.

## Built-in Skills

In addition to user and project skills, Qodex ships with built-in skills that are always available:

- `project` — Project awareness and repository conventions.
- `go-testing` — Go testing conventions and best practices.
- `git`, `cmake`, `clang`, `make`, `curl`, `wget`, `java`, `rg`, `sed`, `base64` — Language/runtime-specific guidance.
- `node`, `npm`, `npx`, `nvm` — Node.js ecosystem conventions.
- `dotnet`, `nuget`, `msbuild`, `nmake` — .NET ecosystem conventions.
- `system-packages`, `python`, `pip`, `conda` — Python and system package management.
- `flutter`, `dart` — Flutter/Dart development conventions.
- `archives`, `system-admin` — Archive handling and system administration.
- `docker`, `qemu`, `adb` — Container, VM, and Android device management.

Built-in skills are embedded at compile time. They can be overridden by placing a skill with the same name in your project's `.qodex/skills/` or user-level `~/.config/qodex/skills/` directory.

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

## Reset

To remove all Qodex state from the current project:

```sh
qodex reset
```

This deletes the `.qodex/` directory (project config, database with session history, and skills). It prompts for confirmation unless `--force`/`-f` is passed.

To also remove the global `~/.config/qodex/` directory (user-level config and skills):

```sh
qodex reset --all
```

## Backend Profiles

Qodex supports multiple OpenAI-compatible local backends. Select the backend during `qodex setup`, or override with `runtime.backend` in `.qodex/config.toml`.

### llama.cpp (Default)

```toml
[runtime]
backend = "llama.cpp"
```

Qodex installs llama.cpp to `~/.config/qodex/bin/` and starts the server on port `8080` by default. Models are stored in `~/.config/qodex/models/`.

### vLLM

```toml
[runtime]
backend = "vllm"
```

vLLM is installed via `pip` and serves an OpenAI-compatible endpoint on port `8000` by default.

### SGLang

```toml
[runtime]
backend = "sglang"
```

SGLang is installed via `pip` and serves an OpenAI-compatible endpoint on port `8000` by default.

All backends are managed through `qodex serve start|stop|status`. See [llama.cpp Setup Guide](llama-cpp-setup.md) for additional model recommendations.

## Native Tool Calls

By default, Qodex instructs the model to emit tool requests as inline JSON inside the chat text (prompt-based tool calling). This works reliably across all backends and models.

When the backend and model support it, Qodex can use the OpenAI-native `tools` parameter and `tool_calls` response format instead. To enable:

```toml
[agent]
tool_calls = "native"
```

In native mode:
- Tool definitions are sent via the API `tools` parameter.
- The model's tool calls arrive as structured `tool_calls` in the response.
- Tool results are returned as `role: "tool"` messages.
- The system prompt does not include JSON formatting instructions.
- Backends with proper function-calling training (e.g. Qwen 2.5+ with vLLM) produce more reliable tool calls.

If the model does not return native tool calls or you see worse behavior, fall back to prompt mode:

```toml
[agent]
tool_calls = "prompt"
```

Not all backends implement `tools` parameter support equally. Test with your specific backend and model combination. Streaming is disabled when native tool calls are in use.

## Shell Commands And Windows

On Windows, Qodex runs shell commands through `cmd.exe /C` instead of `sh -c`. This affects:

- `run_command` tool calls
- Skill scripts executed via `run_script`
- Test and formatter discovery (`go test`, `pytest`, `npm test`, etc.)

Bash-specific syntax (pipes, `&&`, `||`, `$()`, `[[ ]]`) is not available in `cmd.exe` sessions. If you need shell portability, use POSIX-compatible commands or run inside WSL2.

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

## Version Command

```sh
./qodex version
```

Prints the version, git commit hash, and build timestamp. Set via `-ldflags` at build time; a dev build shows `version dev`.

## Shell Completions

```sh
# Bash
source <(./qodex completion bash)
# To load permanently:
echo "source <(./qodex completion bash)" >> ~/.bashrc

# Zsh
source <(./qodex completion zsh)
# To load permanently:
echo "source <(./qodex completion zsh)" >> ~/.zshrc

# Fish
./qodex completion fish > ~/.config/fish/completions/qodex.fish

# PowerShell
./qodex completion powershell | Out-String | Invoke-Expression
```

## Debug Mode

For troubleshooting, pass `--debug <file>` to any command:

```sh
./qodex --debug /tmp/qodex.log run "debug this issue"
./qodex --debug /tmp/qodex.log chat
```

The log file captures:
- Agent loop events (tool calls, model responses, errors).
- Panic recovery information.
- Startup configuration.

Check the log after an issue:

```sh
cat /tmp/qodex.log
```

## Privacy Model

With the default `llama.cpp` setup, prompts, code, and tool results stay on your machine. This assumes:

- The configured model endpoint is local.
- Network-like commands are reviewed through approval prompts.
- Skills and scripts are local and reviewed.

If you configure a remote OpenAI-compatible endpoint, your data may leave your machine.

See [Security Model](security-model.md) for the detailed tool and approval boundaries.
