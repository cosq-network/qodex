# Roadmap

This roadmap tracks technical activities for Locha as implementation phases. Status values are:

- `completed`: implemented and verified at MVP level.
- `in-progress`: partially implemented, usable in limited form, or needing hardening.
- `planned`: not implemented yet.

## Current Snapshot

Locha has a working Go MVP with Cobra CLI commands, a basic Bubble Tea TUI, OpenAI-compatible `llama.cpp` chat completions, prompt-based JSON tool calling, local tools, local skills, SQLite persistence, and session resume.

## Phase 0: Foundation MVP

Goal: establish a locally usable coding agent that can talk to `llama.cpp`, call tools, persist sessions, and run from the terminal.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Go module and package layout | Core | Uses `cmd/locha` and `internal/...` packages. |
| completed | Cobra command structure | CLI | `init`, `run`, `chat`, `doctor`, `skills`, and `sessions` exist. |
| completed | One-shot prompt command | CLI | `locha run "prompt"` executes the agent loop. |
| completed | Terminal chat command | CLI/TUI | `locha chat` starts the Bubble Tea UI. |
| completed | Project initializer | CLI/Config | `locha init` creates `.locha/config.toml` and a starter project skill. |
| completed | Model diagnostics | CLI/Runtime | `locha doctor` checks config and `/v1/models` connectivity. |
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
| completed | CLI approval prompt | Safety | `locha run` asks before write/shell tools unless `--yes`. |
| completed | Explicit auto-approval flag | Safety | `--yes` enables write/shell execution. |
| completed | Basic Bubble Tea chat screen | TUI | Prompt input, scrollable history, and responses. |
| completed | SQLite store | Persistence | Uses `modernc.org/sqlite`. |
| completed | Session table | Persistence | Stores project root, title, model, backend, and timestamps. |
| completed | Message table | Persistence | Stores user and assistant messages. |
| completed | Tool call/result tables | Persistence | Stores tool requests and results. |
| completed | Project-local DB default | Persistence | `.locha/locha.db`. |
| completed | Project skill discovery | Skills | Loads `.locha/skills/<name>/SKILL.md`. |
| completed | User skill discovery | Skills | Loads `~/.config/locha/skills/<name>/SKILL.md` when available. |
| completed | Project skill override | Skills | Project skills override user skills with the same name. |
| completed | Starter project skill | Skills | `locha init` creates `.locha/skills/project/SKILL.md`. |
| completed | Local build | Packaging | `go build ./cmd/locha` produces `./locha`. |
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
| completed | Config command | CLI/Config | `locha config get/set/list` exists. |
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
| in-progress | Long-running task state | TUI | Shows a “Working...” marker plus live tool and approval events. |
| completed | TUI approvals | TUI/Safety | Chat mode now asks inline for write, shell, and network approvals unless `--yes` is set. |
| planned | Streaming model responses | Runtime/TUI | Render tokens as they arrive. |
| completed | Tool timeline | TUI | Shows tool calls, results, failures, and approval decisions inline. |
| in-progress | Interactive approval panel | TUI/Safety | Allows approve/deny from the TUI; richer command and diff rendering remains planned. |
| planned | Diff viewer | TUI/Tools | Inspect `write_patch` and `write_file` changes before approval. |
| planned | Better error panel | TUI | Show model/tool errors without losing chat context. |
| in-progress | TUI model tests | Testing | Approval key handling and event rendering have coverage; busy state and resume rendering still need tests. |

## Phase 3: Session Intelligence And Context

Goal: make longer coding sessions useful by reconstructing context correctly, compacting history, and tracking task state.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Session listing | Persistence/CLI | `locha sessions list`. |
| completed | Session resume | Persistence/CLI | `locha sessions resume <id>` resumes in the TUI. |
| completed | Session list and resume storage | Persistence | Resume loads prior messages. |
| in-progress | Conversation reconstruction | Agent/Persistence | Resumed sessions load prior user/assistant messages. Tool result reconstruction is not complete. |
| in-progress | Tool history restoration | Persistence/Agent | Tool calls are stored but not reconstructed into model context on resume. |
| planned | Context compaction | Agent | Needed for long sessions and smaller local models. |
| planned | Better planning state | Agent | Track current task, files inspected, and actions taken explicitly. |
| planned | Non-interactive resume | CLI/Agent | Continue a prior session through `locha run --session <id>`. |
| planned | Session export | Persistence/CLI | Export transcript and tool events. |
| planned | Approval persistence | Persistence/Safety | Store approval decisions as first-class DB rows. |
| planned | Migrations package | Persistence | Version and apply schema changes safely. |

## Phase 4: Skills System

Goal: evolve skills from simple Markdown context into routable, budgeted, policy-aware instruction bundles.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | Skills inspection | CLI/Skills | `locha skills list` and `locha skills show <name>`. |
| completed | Project skill discovery | Skills | Loads `.locha/skills/<name>/SKILL.md`. |
| completed | User skill discovery | Skills | Loads `~/.config/locha/skills/<name>/SKILL.md` when available. |
| completed | Project skill override | Skills | Project skills override user skills with the same name. |
| completed | Starter project skill | Skills | `locha init` creates `.locha/skills/project/SKILL.md`. |
| in-progress | Skill selection | Skills/Agent | Explicit prompt matching and simple name matching exist. |
| planned | `skill.toml` metadata | Skills | Add triggers, allowed tools, and context budgets. |
| planned | Model-assisted skill routing | Skills/Agent | Let the model choose from compact skill summaries. |
| planned | Skill context slicing | Skills/Agent | Include only relevant sections for large skills. |
| planned | Skill script policy | Skills/Safety | Treat scripts as approved shell commands with clear provenance. |

## Phase 5: Coding Tools And Project Awareness

Goal: add higher-signal development tools so the agent can inspect, test, format, and navigate real repositories better.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| planned | Diff preview | Tools/TUI | Show patch/file changes before approval. |
| planned | Tool output artifacts | Tools/Persistence | Persist large raw outputs and send summaries to the model. |
| planned | LSP diagnostics | Tools | Add language-server diagnostics tool. |
| planned | LSP definition/references | Tools | Add navigation tools for supported languages. |
| planned | Test runner tool | Tools | Project-aware test command discovery and execution. |
| planned | Formatter tool | Tools | Run formatter with approval and file scope. |
| planned | Project index | Tools/Context | Maintain a lightweight file and symbol index. |
| planned | Review mode | Agent/Tools | Purpose-built flow for reviewing uncommitted changes. |

## Phase 6: Backend Expansion

Goal: keep `llama.cpp` as the primary runtime while supporting other OpenAI-compatible local backends cleanly.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| completed | llama.cpp-first runtime | Runtime | Primary documented and default backend. |
| planned | vLLM endpoint profile | Runtime | Same OpenAI-compatible abstraction, with documented config. |
| planned | SGLang endpoint profile | Runtime | Same OpenAI-compatible abstraction, with documented config. |
| planned | Native tool-call parsing | Runtime/Agent | Use native OpenAI-compatible tool calls when backend/model supports them reliably. |
| planned | Backend capability detection | Runtime | Detect streaming, model listing, context, and tool-call support where possible. |
| planned | Example configs | Docs/Runtime | llama.cpp, vLLM, and SGLang profiles. |
| planned | llama.cpp model setup guide | Docs/Runtime | Recommended Qwen GGUF variants and server flags. |

## Phase 7: Release Readiness

Goal: turn the local MVP into a distributable developer tool with reliable builds, install paths, and contributor workflow.

| Status | Activity | Area | Notes |
| --- | --- | --- | --- |
| planned | Version command | CLI/Packaging | Add `locha version` with commit/build metadata. |
| planned | Shell completions packaging | CLI/Packaging | Cobra can generate completions; install docs and release packaging are still needed. |
| planned | Release builds | Packaging | Cross-platform binaries for macOS, Linux, and Windows. |
| planned | Homebrew formula | Packaging | Useful for macOS users. |
| planned | Install script | Packaging | Optional convenience installer. |
| planned | Contributor guide | Docs | Coding standards, tests, release workflow. |
| planned | CI workflow | Testing/Packaging | Run tests and release checks on pushes and tags. |
| planned | Signed releases | Packaging | Optional, after release process stabilizes. |

## Near-Term Priority Order

1. TUI diff preview before file writes and patches.
2. Streaming model responses.
3. Better error panel.
4. TUI busy state and resume rendering tests.
5. Context compaction.
6. Backend capability detection for streaming and native tool calls.
7. Session export and tool history reconstruction.
