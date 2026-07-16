# Tool Calling And Skills

This document describes how Qodex exposes tools to the model and how skills shape agent behavior.

## Design Goal

The model is not allowed to act directly on the filesystem or shell. It can only request tools. Qodex then:

1. validates the request
2. applies approval policy
3. executes the tool
4. stores the result
5. feeds the result back into the conversation

That separation is the core control boundary for the agent.

## Tool Calling Modes

Qodex supports two modes.

### Prompt Mode

This is the default. The model is instructed to emit exactly one JSON object when it wants a tool:

```json
{"tool_call":{"name":"read_file","arguments":{"path":"README.md"}}}
```

Qodex parses that JSON from the assistant text response and validates it before execution.

### Native Mode

If `agent.tool_calls = "native"`, Qodex sends OpenAI-compatible tool schemas and reads structured `tool_calls` back from the model response.

This mode is useful when the backend/model pair reliably supports native tool calling.

## Tool Definition Model

Each tool in the registry has:

- a stable name
- a human-readable description
- an effect classification
- a JSON schema for arguments
- an executor

The effect classification is one of:

```text
read
write
shell
network
destructive
```

The effect drives default approval handling.

## Tool Results

Tools return structured JSON with the shape:

```json
{
  "ok": true,
  "summary": "Read README.md",
  "content": "file contents or summarized output",
  "metadata": {
    "path": "README.md"
  }
}
```

If output is large, Qodex stores it as an artifact in SQLite and replaces the raw content with a short reference in the model-visible result.

## Built-In Tool Groups

The exact built-in set is defined in `internal/tools.NewRegistry`. Current groups include:

- file inspection and edits
- shell and script execution
- git inspection and review
- test and formatter helpers
- project index and LSP helpers
- language/runtime helpers for Go, Node, Python, Java, .NET, Flutter, and Dart
- package-manager, archive, Docker, QEMU, and ADB helpers

Important higher-level tools currently exposed to the model include:

- `run_tests`
- `run_formatter`
- `review_changes`
- `project_index`
- `lsp_diagnostics`
- `lsp_definition`
- `lsp_find_references`

## Approval Behavior

The simplified default policy is:

- `read`: auto-approve
- `write`: ask unless explicitly allowed
- `shell`: ask unless explicitly allowed
- `network`: ask unless explicitly allowed
- `destructive`: deny by policy

Some tools may be reclassified dynamically. For example, `run_command` can be treated as `network` if its arguments look like network activity.

## Direct Command vs Shell Command

For command execution, prefer direct `argv`:

```json
{
  "argv": ["go", "test", "./..."],
  "timeout_seconds": 120
}
```

Legacy shell-style command execution still exists:

```json
{
  "command": "go test ./...",
  "shell": true,
  "timeout_seconds": 120
}
```

Direct `argv` is safer because it avoids shell parsing.

## Skills

Skills are local instruction bundles that give Qodex project or workflow-specific guidance without changing the binary.

Qodex loads:

- embedded built-in skills
- user skills from `~/.config/qodex/skills`
- project skills from `.qodex/skills`

Project skills override user skills with the same name.

## Skill File Layout

Minimal skill:

```text
.qodex/skills/my-skill/
  SKILL.md
```

Full skill:

```text
.qodex/skills/my-skill/
  SKILL.md
  skill.toml
```

## `skill.toml`

Supported metadata fields are:

- `triggers`
- `allowed_tools`
- `context_budget`
- `scripts`

Example:

```toml
triggers = ["go test", "failing test", "subtest"]
allowed_tools = ["read_file", "search_text", "run_tests"]
context_budget = 3000

[[scripts]]
description = "run focused go tests"
command = "go test ./..."
tool = "run_command"
```

## Skill Selection

Qodex always prefers keeping the active skill set small.

- the built-in or local `project` skill is included when present
- additional skills are selected by keyword heuristics
- model-assisted routing can be enabled with `agent.skill_routing = "model"`

Only relevant sections of large skills are rendered into the model context when section slicing is enabled.

## Pre-Approved Scripts

Skills can define scripts in `skill.toml`. These scripts are surfaced to the model as named, pre-approved actions and can be executed through `run_script`.

This is intentionally narrower than unconstrained shell access:

- the script must come from an active skill
- the request is matched by description
- provenance is recorded in the tool result metadata

## Guidance For Adding New Tools Or Skills

When adding a tool:

1. implement the executor
2. register it in `NewRegistry`
3. define its JSON schema
4. assign the correct effect
5. add registration and behavior tests

When adding a skill:

1. keep `SKILL.md` concise and actionable
2. use `skill.toml` only for routing, budgeting, and scripts
3. restrict `allowed_tools` if the workflow should stay narrow
4. avoid hidden assumptions that depend on one repository layout
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
