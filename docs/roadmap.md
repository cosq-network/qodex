# Roadmap

This roadmap tracks technical activities for Qodex as implementation phases. Status values are:

- `completed`: implemented and verified at MVP level.
- `in-progress`: partially implemented, usable in limited form, or needing hardening.
- `planned`: not implemented yet.

## Current Snapshot

Qodex has a working Go MVP with Cobra CLI commands, a Bubble Tea TUI with streaming token rendering and diff preview, OpenAI-compatible `llama.cpp` chat completions with SSE streaming support, prompt-based JSON tool calling, local tools, local skills, SQLite persistence, session resume with tool history reconstruction, context compaction, session export, error panel, and backend capability detection.

## Phase 0: Foundation MVP

Goal: establish a locally usable coding agent that can talk to `llama.cpp`, call tools, persist sessions, and run from the terminal.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Go module and package layout | Core | Uses `cmd/qodex` and `internal/...` packages. |
| completed | Cobra command structure | CLI | `init`, `run`, `chat`, `doctor`, `skills`, and `sessions` exist. |
| completed | One-shot prompt command | CLI | `qodex run "prompt"` executes the agent loop. |
| completed | Terminal chat command | CLI/TUI | `qodex chat` starts the Bubble Tea UI. |
| completed | Project initializer | CLI/Config | `qodex init` creates `.qodex/config.toml` and a starter project skill. |
| completed | Model diagnostics | CLI/Runtime | `qodex doctor` checks config and `/v1/models` connectivity. |
| completed | OpenAI-compatible client | Runtime | Uses `/v1/chat/completions` and `/v1/models`. |
| completed | llama.cpp-first configuration | Runtime | Defaults point to `http://127.0.0.1:8080/v1`. |
| completed | Non-streaming chat completions | Runtime | Stable MVP behavior. |
| completed | Basic multi-step agent loop | Agent | Model can request tools and continue after tool results. |
| completed | System prompt contract | Agent | Instructs model to emit one JSON tool call or final text. |
| completed | Tool result feedback | Agent | Tool result is appended back into conversation context. |
| completed | Max step limit | Agent | Prevents infinite tool loops. |
| completed | Tool registry | Tools | Built-in tools are registered with name, description, effect, and executor. |
| completed | `list_files` | Tools | Lists project files with common directory skips. |
| completed | `read_file` | Tools | Reads full or line-ranged files under project root. |
| completed | `search_text` | Tools | Basic in-Go text search under project root. |
| completed | `write_file` | Tools | Writes complete files with approval. |
| completed | `write_patch` | Tools | Applies unified diffs through `git apply` with path validation. |
| completed | `run_command` | Tools | Runs direct argv or shell commands in project root with timeout and approval. |
| completed | `git_status` | Tools | Runs `git status --short`. |
| completed | `git_diff` | Tools | Runs `git diff`. |
| completed | Effect-based tool classification | Safety | Tools use `read`, `write`, and `shell` effects. |
| completed | CLI approval prompt | Safety | `qodex run` asks before write/shell tools unless `--yes`. |
| completed | Explicit auto-approval flag | Safety | `--yes` enables write/shell execution. |
| completed | Basic Bubble Tea chat screen | TUI | Prompt input, scrollable history, and responses. |
| completed | SQLite store | Persistence | Uses `modernc.org/sqlite`. |
| completed | Session table | Persistence | Stores project root, title, model, backend, and timestamps. |
| completed | Message table | Persistence | Stores user and assistant messages. |
| completed | Tool call/result tables | Persistence | Stores tool requests and results. |
| completed | Project-local DB default | Persistence | `.qodex/qodex.db`. |
| completed | Project skill discovery | Skills | Loads `.qodex/skills/<name>/SKILL.md`. |
| completed | User skill discovery | Skills | Loads `~/.config/qodex/skills/<name>/SKILL.md` when available. |
| completed | Project skill override | Skills | Project skills override user skills with the same name. |
| completed | Starter project skill | Skills | `qodex init` creates `.qodex/skills/project/SKILL.md`. |
| completed | Local build | Packaging | `go build ./cmd/qodex` produces `./qodex`. |
| completed | Package tests run | Testing | `go test ./...` passes. |
| completed | README | Docs | Project overview, stack, build, quick start. |
| completed | Developer guide | Docs | Architecture and milestones. |
| completed | Tool calling and skills guide | Docs | Tool protocol and skill design. |
| completed | User guide | Docs | Setup, usage, skills, sessions, troubleshooting. |
| completed | Roadmap | Docs | This document. |

## Phase 1: MVP Hardening

Goal: make the current MVP safer, easier to configure, and more reliable without expanding the product surface too much.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Runtime configuration | Config/Runtime | Temperature, top-p, model name, base URL, and max steps are configurable. |
| completed | TOML parsing | Config | Config files are decoded with a strict TOML parser. |
| completed | Proper TOML parser | Config | Uses `github.com/pelletier/go-toml/v2` instead of the former line-based parser. |
| completed | Config validation | Config | Validates backend, model URL, runtime bounds, agent steps, and approval values. |
| completed | Config command | CLI/Config | `qodex config get/set/list` exists. |
| completed | Path safety | Safety/Tools | Project-root escape checks exist for file paths and patch paths, with adversarial tests. |
| completed | Command safety | Safety/Tools | `run_command` supports direct `argv`, rejects high-risk direct commands, and keeps shell mode approval-gated. |
| completed | Structured command execution | Safety/Tools | Direct argv execution exists and is documented as preferred. |
| completed | Destructive command detection | Safety | Obvious high-risk shell and direct argv patterns are rejected. Broader policy continues in later phases. |
| completed | Network command detection | Safety | Likely network commands are classified as `network` for approval and result metadata. |
| completed | Tool-call validation feedback | Agent | Malformed tool-call JSON is fed back to the model for self-repair. |
| completed | Structured output repair loop | Runtime/Agent | The agent retries after malformed tool JSON within the normal max-step loop. |
| completed | Store tests | Testing | Session/message/tool persistence has coverage. |
| completed | Config tests | Testing | Project config loading, TOML parsing, path normalization, validation, and set behavior have coverage. |
| completed | Fake model server tests | Testing | Agent loop is covered with deterministic fake model responses. |
| completed | End-to-end smoke test | Testing | CLI `run` is covered against a local fake OpenAI-compatible endpoint when listeners are available. |
| completed | Security model | Docs | Explicit threat model for local tools and approvals. |

## Phase 2: Usable Interactive TUI

Goal: move from a functional chat screen to an interactive coding-agent interface that can stream, show tool activity, and ask for approvals inline.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Resume history rendering | TUI/Persistence | Prior user/assistant messages are shown in resumed sessions. |
| completed | Long-running task state | TUI | Shows a “Working...” marker plus live tool and approval events. |
| completed | TUI approvals | TUI/Safety | Chat mode now asks inline for write, shell, and network approvals unless `--yes` is set. |
| completed | Streaming model responses | Runtime/TUI | Render tokens as they arrive via SSE. |
| completed | Tool timeline | TUI | Shows tool calls, results, failures, and approval decisions inline. |
| completed | Interactive approval panel | TUI/Safety | Allows approve/deny from the TUI; diff preview now shown before approval. |
| completed | Diff viewer | TUI/Tools | Inspect `write_patch` and `write_file` changes before approval. |
| completed | Better error panel | TUI | Show model/tool errors without losing chat context, with persistent error status bar. |
| completed | TUI model tests | Testing | Approval key handling, busy state, resume rendering, and diff rendering have coverage. |

## Phase 3: Session Intelligence And Context

Goal: make longer coding sessions useful by reconstructing context correctly, compacting history, and tracking task state.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Session listing | Persistence/CLI | `qodex sessions list`. |
| completed | Session resume | Persistence/CLI | `qodex sessions resume <id>` resumes in the TUI. |
| completed | Session list and resume storage | Persistence | Resume loads prior messages. |
| completed | Conversation reconstruction | Agent/Persistence | Resumed sessions load prior user/assistant messages and tool call history. |
| completed | Tool history restoration | Persistence/Agent | Tool calls and results are loaded and summarized into model context on resume. |
| completed | Context compaction | Agent | Automatically compacts conversation history when approaching context limit. |
| completed | Better planning state | Agent | Track current task, files inspected, and actions taken explicitly via PlanState. |
| completed | Non-interactive resume | CLI/Agent | Continue a prior session through `qodex run --session <id>`. |
| completed | Session export | Persistence/CLI | `qodex sessions export <id>` outputs JSON transcript with tool events. |
| completed | Approval persistence | Persistence/Safety | Store approval decisions as first-class DB rows via AddApproval. |
| completed | Migrations package | Persistence | Version and apply schema changes safely via schema_version table. |

## Phase 4: Skills System

Goal: evolve skills from simple Markdown context into routable, budgeted, policy-aware instruction bundles.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Skills inspection | CLI/Skills | `qodex skills list` and `qodex skills show <name>`. |
| completed | Project skill discovery | Skills | Loads `.qodex/skills/<name>/SKILL.md`. |
| completed | User skill discovery | Skills | Loads `~/.config/qodex/skills/<name>/SKILL.md` when available. |
| completed | Project skill override | Skills | Project skills override user skills with the same name. |
| completed | Starter project skill | Skills | `qodex init` creates `.qodex/skills/project/SKILL.md`. |
| completed | Skill selection | Skills/Agent | Keyword-based matching from headings and body content, project skill always included, `/skill <name>` explicit invocation. |
| completed | `skill.toml` metadata | Skills | Add triggers, allowed tools, context budgets, and predefined scripts. |
| completed | Model-assisted skill routing | Skills/Agent | Let the model choose from compact skill summaries via SelectViaModel, with heuristic fallback. Controlled by agent.skill_routing config (auto/model). |
| completed | Skill context slicing | Skills/Agent | Include only relevant sections for large skills via RenderSliced, which splits on ##/### headings, scores each section against the prompt, and includes top-scoring sections within budget. |
| completed | Skill script policy | Skills/Safety | run_script tool executes pre-approved skill scripts without re-prompting; metadata tracks provenance (skill name + script description). |

## Phase 5: Coding Tools And Project Awareness

Goal: add higher-signal development tools so the agent can inspect, test, format, and navigate real repositories better.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Diff preview | Tools/TUI | Show patch/file changes before approval via inline diff rendering. |
| completed | Tool output artifacts | Tools/Persistence | Persist large raw outputs and send summaries to the model via output_artifacts DB table. |
| completed | LSP diagnostics | Tools | Add language-server diagnostics tool via JSON-RPC 2.0 client. |
| completed | LSP definition/references | Tools | Add navigation tools for supported languages. |
| completed | Test runner tool | Tools | Project-aware test command discovery and execution for go, pytest, and jest. |
| completed | Formatter tool | Tools | Run formatter (go fmt, ruff, black, prettier) with approval and file scope. |
| completed | Project index | Tools/Context | Maintain a lightweight file and symbol index with per-language regex extraction. |
| completed | Review mode | Agent/Tools | Purpose-built `qodex review` CLI command + `review_changes` tool for uncommitted change analysis. |

## Phase 6: Backend Expansion

Goal: keep `llama.cpp` as the primary runtime while supporting other OpenAI-compatible local backends cleanly.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | llama.cpp-first runtime | Runtime | Primary documented and default backend. |
| completed | vLLM endpoint profile | Runtime | Same OpenAI-compatible abstraction, with documented config and example. |
| completed | SGLang endpoint profile | Runtime | Same OpenAI-compatible abstraction, with documented config and example. |
| completed | Native tool-call parsing | Runtime/Agent | Use native OpenAI-compatible tool calls when backend/model supports them reliably. |
| completed | Backend capability detection | Runtime | Detect streaming support and model availability at startup. |
| completed | Example configs | Docs/Runtime | llama.cpp, vLLM, and SGLang profiles. |
| completed | llama.cpp model setup guide | Docs/Runtime | Recommended Qwen GGUF variants and server flags. |

## Phase 7: Release Readiness

Goal: turn the local MVP into a distributable developer tool with reliable builds, install paths, and contributor workflow.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Version command | CLI/Packaging | `qodex version` prints version, commit hash, and build timestamp. |
| completed | Shell completions | CLI/Packaging | `qodex completion bash/zsh/fish/powershell` generates shell completions. |
| completed | Release builds | Packaging | Cross-platform GoReleaser config targeting macOS, Linux, and Windows (amd64 + arm64). |
| completed | Homebrew formula | Packaging | Stub formula at `contrib/homebrew/qodex.rb`. |
| completed | Install script | Packaging | `scripts/install.sh` downloads and installs from GitHub Releases. |
| completed | Contributor guide | Docs | `CONTRIBUTING.md` covers standards, testing, and release process. |
| completed | CI workflow | Testing/Packaging | GitHub Actions runs tests with race detector on push/PR and releases on tags. |
| completed | Signed releases | Packaging | GoReleaser signs all artifacts with GPG. See [Release Management](release-management.md). |

## Near-Term Priority Order

1. ~~TUI diff preview before file writes and patches.~~ *(completed)*
2. ~~Streaming model responses.~~ *(completed)*
3. ~~Better error panel.~~ *(completed)*
4. ~~TUI busy state and resume rendering tests.~~ *(completed)*
5. ~~Context compaction.~~ *(completed)*
6. ~~Backend capability detection for streaming and native tool calls.~~ *(completed)*
7. ~~Session export and tool history reconstruction.~~ *(completed)*
