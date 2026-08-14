# Qodex

Qodex is a local-first coding agent CLI written in Go. It uses a terminal UI with streaming token rendering, an OpenAI-compatible local model endpoint, and a single locally hosted Qwen Coder model by default.

The intended runtime is `llama.cpp`, and Qodex manages backend installation and catalog GGUF model downloads during `qodex setup` on Linux and macOS. vLLM and SGLang use Hugging Face model IDs and resolve model storage through their own runtimes. On Windows, WSL2 is the recommended path for managed local backends; native Windows can still target a manually managed OpenAI-compatible endpoint. Backend capability detection is performed at startup to enable streaming when supported.

## Current Status

The repository includes a fully featured coding agent with:

- Cobra CLI commands: `setup`, `init`, `config`, `chat`, `run`, `review`, `doctor`, `skills`, `sessions`, `serve`, `up`, `down`, `status`, `models`, `reset`, `version`, and `completion`.
- Bubble Tea terminal chat UI with streaming token rendering, inline diff preview, spinner, error panel, and multi-line input with `@` file autocomplete.
- OpenAI-compatible `/v1/chat/completions` client with SSE streaming and capability detection.
- Prompt-based JSON tool calling with validation repair loop and optional native OpenAI `tools`/`tool_calls` support.
- 98 built-in tools covering file/symbol search, LSP code intelligence, Git workflows, CMake/clang/make/.NET/Java toolchains, package managers, language runtimes, archives, Docker/QEMU, ADB, and system administration.
- 31 built-in skills shipped with Qodex covering project conventions, git, go-testing, cmake, clang, make, curl, wget, java, rg, sed, base64, node, npm, npx, nvm, dotnet, nuget, msbuild, nmake, system packages, python, pip, conda, flutter, dart, archives, system admin, docker, qemu, and adb.
- Skill system: `skill.toml` metadata (triggers, allowed_tools, context_budget, scripts), model-assisted or heuristic skill routing (`agent.skill_routing`), section-aware context slicing, and pre-approved script policy with provenance tracking.
- SQLite session/tool event storage with WAL mode, migrations, and approval persistence.
- Session resume with full tool history reconstruction (TUI via `sessions resume`, non-interactive via `run --session`), plus JSON export.
- Approval gates for write, shell, and network tools with inline diff preview and `--yes` auto-approval.
- MCP client support for stdio and Streamable HTTP: configured MCP servers contribute namespaced tools while retaining Qodex approval and audit behavior; `qodex mcp doctor` checks installation, environment-backed authentication, protocol health, server capabilities, and tool discovery.
- Interoperable repository instructions from `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, Cursor rules, and GitHub Copilot instructions.
- Planning state tracking (current task, inspected files, actions taken) in the system prompt.
- Context compaction (keeps the last 8 messages when approaching the token limit).
- LCS-based unified diff generation for write tool previews.
- LSP integration (`gopls`, `pyright`, `typescript-language-server`, `rust-analyzer`) for diagnostics, definitions, and references.
- Managed model server lifecycle: install the backend, acquire the backend-appropriate model, and start/stop/status the server from the CLI. Catalog GGUF downloads resume interrupted transfers, report live progress, and validate the GGUF header. Managed startup waits for `/v1/models` readiness, cleans stale runtime state, and llama.cpp starts with the configured context size and CPU thread count.
- Hardened setup flow: setup uses backend-specific model contracts, writes configuration atomically only after required setup succeeds, and pins the managed llama.cpp release. `QODEX_LLAMA_CPP_VERSION` overrides the release; `QODEX_LLAMA_CPP_SHA256` optionally verifies its archive.
- Hardening coverage: stalled MCP calls honor context deadlines, MCP name collisions fail closed, Git path-scoped commits avoid unrelated staged files, Git snapshots stay ignored, and managed Windows PID checks verify process liveness.

## CLI Reference

`qodex` run without arguments prints help if configuration exists, otherwise launches the interactive setup prompt.

Global flags (available on every command):

| Flag | Description |
|------|-------------|
| `--config string` | Config file path (overrides discovery) |
| `-y, --yes` | Auto-approve write and shell tools |
| `--debug string` | Write diagnostics to this file |
| `-h, --help` | Help for any command |

### Commands

| Command | Description |
|---------|-------------|
| `setup` | Interactive first-time setup wizard: choose a backend, install it, select a GGUF or enter a Hugging Face model ID, wait for server readiness, and write `.qodex/config.toml` only after successful setup |
| `init` | Create project-local configuration and a starter skill (`--force` overwrites) |
| `config list` | Print the effective configuration as `key=value` |
| `config get KEY` | Print one effective configuration value |
| `config set KEY VALUE` | Set one project-local configuration value |
| `chat` | Start the terminal chat UI (alt-screen, streaming, inline approvals) |
| `run PROMPT` | Run a one-shot agent prompt; `--session ID` resumes an existing session |
| `review` | Analyze uncommitted git changes and produce a structured code review |
| `doctor` | Check configuration and local model connectivity (backend install, model file, server, live endpoint probe) |
| `mcp list` | List configured MCP servers and authentication modes |
| `mcp doctor [NAME]` | Diagnose MCP installation, authentication, protocol health, tools, and capabilities |
| `skills list` | List discovered skills as `name<TAB>path` |
| `skills show NAME` | Print a skill's content |
| `sessions list` | List recent sessions |
| `sessions resume ID` | Resume a session in the terminal chat UI |
| `sessions export ID` | Export session data as JSON |
| `serve start` | Ensure the managed backend is running (`-p/--port` overrides the port; `--ctx` and `--threads` override llama.cpp context size and CPU threads) |
| `serve stop` | Stop the managed backend |
| `serve status` | Show backend running state, PID, port, model, and install status |
| `up` | One-shot: ensure the managed backend is installed, configured, and running (accepts `--port`, `--ctx`, and `--threads` like `serve start`) |
| `down` | One-shot: stop the managed backend if running |
| `status` | Compact backend status (running state, PID, port, model) |
| `models list` | List known models and downloaded status |
| `models download NAME` | Download a GGUF model into `~/.config/qodex/models/` (resumes partial files and validates the GGUF header) |
| `reset` | Remove Qodex state, config, and cached data (`--force` skips confirmation, `--all` also removes `~/.config/qodex`) |
| `version` | Print version, commit, and build date |
| `completion SHELL` | Generate shell completion scripts (`bash`, `zsh`, `fish`, `powershell`) |

## Configuration

Configuration is TOML, discovered in this order (later wins):

1. `~/.config/qodex/config.toml` — user config
2. `<project>/.qodex/config.toml` — project config
3. An explicit `--config PATH` — loads only that file

Unknown keys are rejected, and a project file inherits anything not set from user config and defaults. Use `qodex config list` to inspect the effective configuration and `qodex config set KEY VALUE` to update the project config.

| Key | Default | Description |
|-----|---------|-------------|
| `model.provider` | `openai-compatible` | Provider identifier (must be `openai-compatible`) |
| `model.base_url` | `http://127.0.0.1:8080/v1` | OpenAI-compatible base URL |
| `model.model` | `qwen2.5-coder` | Model name sent to the endpoint |
| `runtime.backend` | `llama.cpp` | Managed backend: `llama.cpp`, `vllm`, `sglang`, or `external` |
| `runtime.context_tokens` | `32768` | Context window; context compaction triggers at 70% |
| `runtime.temperature` | `0.2` | Sampling temperature (0–2) |
| `runtime.top_p` | `0.95` | Nucleus sampling (0–1) |
| `approval.auto_approve` | `false` | Auto-approve all write/shell/network actions (same as `--yes`) |
| `approval.write_files` | `ask` | Policy for write tools: `ask`, `allow`, or `deny` |
| `approval.run_commands` | `ask` | Policy for shell tools: `ask`, `allow`, or `deny` |
| `approval.network` | `ask` | Policy for network tools: `ask`, `allow`, or `deny` |
| `store.path` | `.qodex/qodex.db` | SQLite database path |
| `agent.max_steps` | `12` | Max agent loop iterations |
| `agent.skill_routing` | `auto` | Skill selection: `auto` (heuristic) or `model` (model-assisted with heuristic fallback) |
| `agent.tool_calls` | `prompt` | Tool-call mechanism: `prompt` (inline JSON) or `native` (OpenAI `tools`/`tool_calls`; streaming disabled) |

MCP servers can be configured under `[mcp.servers.<name>]` with stdio or Streamable HTTP transport, secret-by-environment authentication, trust, and per-tool permissions. Use `qodex mcp doctor` to verify installation, authentication, protocol health, discovered tools, and server capabilities. See [MCP Integration](docs/mcp.md).

Example configs are in [`examples/`](examples/).

## Built-in Tools

98 tools are registered and categorized by effect, which drives the approval policy:

| Effect | Policy | Count |
|--------|--------|-------|
| `read` | Auto-approved | 21 |
| `write` | Per `approval.write_files` | 11 |
| `shell` | Per `approval.run_commands` | 49 |
| `network` | Per `approval.network` | 15 |
| `destructive` | Always denied | 3 |

### Read (auto-approved)

`list_files`, `read_file`, `search_text`, `rg_search`, `grep_search`, `find_files`, `tail_file`, `ps_list`, `git_status`, `git_diff`, `git_log`, `git_workspace_summary`, `review_changes`, `project_index`, `lsp_diagnostics`, `lsp_definition`, `lsp_find_references`, `ar_list`, `tar_list`, `zip_list`, `adb_devices`

### Write

`write_file`, `write_patch`, `sed_edit`, `git_stage`, `git_commit`, `git_branch`, `git_worktree`, `git_undo`, `git_snapshot`, `git_restore_snapshot`

### Shell

`run_command`, `run_script`, `run_tests`, `run_formatter`, `cmake_configure`, `cmake_build`, `clang_format`, `clang_tidy`, `make_build`, `nmake_build`, `javac_compile`, `java_run`, `node_run`, `npm_command`, `nvm_use`, `dotnet_run`, `dotnet_build`, `dotnet_test`, `msbuild`, `python_run`, `python3_run`, `conda_create`, `flutter_run`, `flutter_build`, `flutter_test`, `dart_run`, `dart_analyze`, `dart_format`, `pub_add`, `pub_remove`, `ar_create`, `ar_extract`, `tar_create`, `tar_extract`, `zip_create`, `zip_extract`, `awk_process`, `base64_encode`, `chmod_change`, `docker_build`, `docker_run`, `docker_compose_up`, `docker_compose_down`, `qemu_run`, `adb_shell`, `adb_push`, `adb_pull`, `apt_install`, `apt_get_install`

### Network

`curl`, `wget`, `npx_command`, `nuget_restore`, `nuget_install`, `winget_install`, `choco_install`, `snap_install`, `dnf_install`, `brew_install`, `pip_install`, `pip3_install`, `conda_install`, `pub_get`, `pub_upgrade`

### Destructive (denied by policy)

`chown_change`, `user_add`, `user_del`

Tool results are returned as JSON (`ok`, `summary`, `content`, `metadata`), capped at 20,000 characters; large outputs are stored as artifacts in SQLite and referenced by short IDs. `run_command` defaults to a 120s timeout (max 300s) and kills the process on timeout. `run_script` executes scripts defined in active skill `skill.toml` files with provenance recorded.

## Approval Gates

Every tool carries an effect (`read`, `write`, `shell`, `network`, or `destructive`) that selects the policy:

- **Read** tools run without prompting.
- **Write, shell, and network** tools prompt unless the policy is `allow` (`approval.auto_approve = true` or `--yes`) or `deny`.
- **Destructive** tools (`user_add`, `user_del`, `chown_change`) are always denied by policy.

Approval requests show a summary and, for writes, an inline LCS-based unified diff preview. In chat mode the TUI prompts inline with `y`/`n` and a 30s timeout; `run`/`review` prompt on stdin. Shell commands are inspected at runtime and reclassified as `network` when they look network-related (curl/wget/ssh/scp/rsync, `git clone/pull/fetch`, `go get`, package installers), making network approvals explicit. Safety checks reject absolute/escaping paths, dangerous shell commands (e.g. `rm -rf /`, `curl | sh`), and destructive `argv` patterns. See the [Security Model](docs/security-model.md).

## Skills

31 skills are compiled into the binary and overridable from `<project>/.qodex/skills/` and `~/.config/qodex/skills/`:

`adb`, `archives`, `base64`, `clang`, `cmake`, `conda`, `curl`, `dart`, `docker`, `dotnet`, `flutter`, `git`, `go-testing`, `java`, `make`, `msbuild`, `nmake`, `node`, `npm`, `npx`, `nuget`, `nvm`, `pip`, `project`, `python`, `qemu`, `rg`, `sed`, `system-admin`, `system-packages`, `wget`

Each skill is a directory with a `SKILL.md` plus a `skill.toml` declaring `name`, `description`, `version`, `triggers`, `allowed_tools`, and optional `scripts` (pre-approved shell snippets executed through `run_script` with `skill:<name>/script:<desc>` provenance). Skill context budgets use the `context_budget` key, with `context_budget_tokens` accepted as a legacy alias. The `allowed_tools` field also acts as a tool-pack boundary: specialized tools are exposed to the model only when the relevant skill is active. The `project` skill is always loaded first. Routing selects up to 3 skills heuristically (keyword/trigger matching, plus explicit `/skill <name>` prompts) or up to 2 via the model when `agent.skill_routing = "model"`. Skills are instructions, not authority — they cannot bypass validation or approval policy.

## Agent Loop

- **Planning state** tracks the current task, files inspected, and actions taken; it is injected into the system prompt and consumed by the step budget alongside `max_steps`.
- **Context compaction** estimates token usage before each step and, above 70% of `runtime.context_tokens`, keeps the system prompt plus the last 8 messages with a "compacted" note.
- **Tool calling** runs in one of two modes: `prompt` (default) parses inline JSON `{"tool_call": ...}` emitted by the model with a validation/repair loop; `native` sends an OpenAI `tools`/`tool_calls` schema (streaming disabled).
- **Streaming** renders tokens as they arrive via SSE when capability detection confirms the endpoint supports it.

### TUI slash commands

The interactive TUI provides `/help`, `/skills [FILTER]`, `/plan`, and `/compact` for local help, skill discovery, planning state, and context management. `/mcp [NAME]` runs MCP diagnostics. `/commit [MESSAGE]` and `/undo [COMMIT]` ask the agent to perform focused Git operations through the normal approval and audit workflow. Existing `/skill <name>` prompts remain supported for explicit skill routing.

## Sessions and Storage

Sessions are persisted in a SQLite database (WAL mode, `busy_timeout=5000`, pure Go via `modernc.org/sqlite`). The schema covers `sessions`, `messages`, `tool_calls`, `tool_results`, `approvals`, and `output_artifacts` (large tool outputs). Every tool call, result, and approval decision is stored for auditability. Resume a session with `qodex sessions resume ID` (TUI) or `qodex run "..." --session ID`, and export any session as JSON with `qodex sessions export ID`.

## LSP Integration

`lsp_diagnostics`, `lsp_definition`, and `lsp_find_references` use a JSON-RPC client over stdio to talk to language servers per file type:

| Language | Server |
|----------|--------|
| Go | `gopls` |
| Python | `pyright-langserver --stdio` |
| JavaScript / TypeScript | `typescript-language-server --stdio` |
| Rust | `rust-analyzer` |

If a server is not installed, the tool returns installation instructions.

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

- [User Guide](docs/user-guide.md)
- [Developer Guide](docs/developer-guide.md)
- [Tool Calling And Skills](docs/tool-calling-and-skills.md)
- [MCP Integration](docs/mcp.md)
- [Security Model](docs/security-model.md)
- [llama.cpp Setup Guide](docs/llama-cpp-setup.md)
- [GitHub Release Pipeline Setup](docs/github-release-setup.md)
- [GPG Signing](docs/gpg-signing.md)
- [Release Management](docs/release-management.md)
- [Roadmap](docs/roadmap.md)
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
- **WSL2**: recommended for managed llama.cpp installs and best compatibility with Python-based backends.
- **Native**: `qodex` runs natively, but automatic llama.cpp setup is not available yet. Use a manually managed OpenAI-compatible endpoint if you stay outside WSL2.
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
go build -ldflags="-X main.version=$(git describe --tags --dirty --always --match 'v*' | sed 's/^v//') -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/qodex
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

Produces binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, and windows/arm64 in `build/`.

## Install

### From source

```sh
make install
```

### Install script (from GitHub Releases)

```sh
curl -fsSL https://github.com/benoybose/qodex/raw/main/scripts/install.sh | sh
```

### Windows PowerShell install

```powershell
irm https://github.com/benoybose/qodex/raw/main/scripts/install.ps1 | iex
```

### Homebrew

```sh
brew install benoybose/qodex/qodex
```

## Release Automation

Releases use Release Please plus GoReleaser:

- conventional commits merged to `main` update a release PR and `CHANGELOG.md`
- merging the release PR with `RELEASE_PLEASE_TOKEN` configured creates the next semantic `v*` tag
- tag pushes publish signed GitHub Release artifacts for Linux, macOS, and Windows
- Linux releases also publish `.deb`, `.rpm`, and `.apk` packages

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
./qodex mcp list
./qodex mcp doctor
./qodex config list
./qodex status
./qodex run "Explain this repository structure"
./qodex chat
./qodex review
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
1. Choose a backend (llama.cpp, vLLM, SGLang, or an external endpoint)
2. Install the backend automatically where supported
3. Select a catalog GGUF model for llama.cpp, or enter a Hugging Face model ID for vLLM/SGLang
4. Download a llama.cpp model with `qodex models download <model-name>` (progress is reported live, interrupted downloads resume, and the GGUF header is validated), while Python backends acquire models through their own runtimes
5. Start the model server and wait for its `/v1/models` endpoint
6. Create project configuration only after the managed setup succeeds

For prompts that may write files or run commands:

```sh
./qodex --yes run "Run the tests and fix the failing issue"
```

Without `--yes`, the one-shot CLI asks before write, shell, and network tools. In chat mode, Qodex shows approval requests inline with a diff preview; press `y` to approve or `n` to deny. Model responses are rendered token-by-token via SSE streaming when the backend supports it.
