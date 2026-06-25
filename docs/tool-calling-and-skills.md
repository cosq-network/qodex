# Tool Calling And Skills

This document defines Locha's local tool-calling and skills design.

## Design Principle

The model is never trusted to perform actions directly. It may request a tool call. Locha validates the request, applies policy, executes the tool, stores the result, and gives the result back to the model.

This makes the agent loop deterministic around filesystem and shell effects even when model output is imperfect.

## Tool Call Format

Prefer native OpenAI-compatible tool calls later when the backend supports them reliably. The current MVP uses a strict prompted JSON block because it works across `llama.cpp` builds and Qwen Coder variants.

Canonical internal representation:

```json
{
  "id": "call_01",
  "name": "read_file",
  "arguments": {
    "path": "README.md"
  }
}
```

The internal representation should be the same no matter how the model produced the request.

## Tool Definition Format

Each tool should have:

```json
{
  "name": "read_file",
  "description": "Read a UTF-8 text file from the current project.",
  "input_schema": {
    "type": "object",
    "properties": {
      "path": {
        "type": "string"
      }
    },
    "required": ["path"],
    "additionalProperties": false
  },
  "effect": "read",
  "approval": "auto"
}
```

Recommended effect values:

```text
read
write
shell
network
destructive
```

The effect controls default approval behavior.

## Tool Result Format

Tool results should be structured:

```json
{
  "tool_call_id": "call_01",
  "ok": true,
  "summary": "Read README.md, 42 lines.",
  "content": "file content or summarized output",
  "metadata": {
    "path": "README.md",
    "bytes": 1800,
    "truncated": false
  }
}
```

For large outputs, truncate `content` and persist the raw output in SQLite or a project-local cache. The model should receive enough detail to continue, not unbounded logs.

## Built-In Tools

### `list_files`

Lists files under the project root.

Arguments:

```json
{
  "path": ".",
  "max_results": 200
}
```

Default approval: auto.

### `read_file`

Reads a text file under the project root.

Arguments:

```json
{
  "path": "internal/agent/loop.go",
  "start_line": 1,
  "end_line": 200
}
```

Default approval: auto.

### `search_text`

Searches project files using ripgrep-compatible behavior.

Arguments:

```json
{
  "query": "func Run",
  "path": ".",
  "case_sensitive": false
}
```

Default approval: auto.

### `write_file`

Writes a complete file under the project root.

Arguments:

```json
{
  "path": "internal/example.go",
  "content": "package internal\n"
}
```

Default approval: ask.

### `write_patch`

Applies a unified diff under the project root.

Arguments:

```json
{
  "patch": "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n"
}
```

Default approval: ask.

The MVP applies patches with `git apply --whitespace=nowarn -` and rejects absolute paths or parent-directory escapes.

### `run_command`

Runs a command in the project root.

Arguments:

```json
{
  "argv": ["go", "test", "./..."],
  "timeout_seconds": 120
}
```

Default approval: ask.

Prefer `argv` for direct execution without shell interpretation.

Legacy shell execution is still supported:

```json
{
  "command": "go test ./...",
  "timeout_seconds": 120
}
```

Shell commands are marked in tool metadata and obvious destructive patterns are rejected.

### `git_status`

Returns concise Git status.

Default approval: auto.

### `git_diff`

Returns Git diff, optionally scoped to files.

Default approval: auto.

## Skills

Skills are local instruction bundles. They let users and projects teach Locha repeatable workflows without changing the binary.

## Skill Locations

Recommended search order:

```text
.locha/skills/
~/.config/locha/skills/
```

Project skills should override user skills with the same name.

## Skill Directory Format

```text
.locha/skills/go-testing/
  skill.toml
  SKILL.md
  examples/
  scripts/
```

Only `SKILL.md` is required for a minimal skill. `skill.toml` is recommended for routing metadata.

## `skill.toml`

Example:

```toml
name = "go-testing"
description = "Run and debug Go tests using project conventions."
version = "0.1.0"

triggers = [
  "test",
  "go test",
  "failing test",
  "coverage"
]

allowed_tools = [
  "read_file",
  "search_text",
  "run_command"
]

context_budget_tokens = 4000
```

## `SKILL.md`

Example:

```md
# Go Testing

Use this skill when the user asks to run, fix, or explain Go tests.

Before changing code:

1. Inspect relevant test files.
2. Run the narrowest test command first.
3. Broaden to `go test ./...` only after the focused test passes.

Prefer table-driven tests when adding coverage.
```

## Skill Loading

A skill can be loaded in three ways:

```text
Explicit: user types /skill go-testing
Heuristic: prompt matches skill triggers
Model-assisted: model selects from compact skill summaries
```

The initial implementation should support explicit and heuristic loading. Model-assisted routing can come later.

## Skill Safety

Skills are instructions, not authority. They should not bypass tool validation or approval policy.

If a skill includes scripts, scripts must be treated like commands:

- Show what will run.
- Ask before execution.
- Enforce timeouts.
- Capture output.
- Persist the event.

## Context Budgeting

Skills can be large. The context builder should include:

- Skill name.
- Skill description.
- Relevant sections from `SKILL.md`.
- Full content only when the skill is small enough.

The agent should never blindly load every skill.

## Prompt Contract

The system prompt should tell the model:

- It is operating inside a local coding agent.
- It may request tools using the provided schema.
- It must not claim to have read or changed files unless tool results prove it.
- It must prefer narrow file reads and searches before broad edits.
- It must explain risky commands before requesting them.
- It must provide a final summary after work is complete.

## Failure Handling

If tool JSON is invalid:

1. Store the invalid response.
2. Ask the model to repair the tool call using the validation error.
3. Retry up to a small limit.
4. Fall back to showing the assistant message to the user if repair fails.

If a tool fails:

1. Return the error to the model.
2. Let the model decide whether to retry, inspect, or report.
3. Stop after repeated failures.
