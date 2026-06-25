# Security Model

Qodex is a local coding agent. Its core security boundary is the project root plus explicit approval for effects that can modify files, execute commands, or use the network.

## Trust Assumptions

- The user controls the project checkout and local Qodex configuration.
- The default model endpoint is local, usually `llama.cpp` at `http://127.0.0.1:8080/v1`.
- Skills are local files loaded from `.qodex/skills` and `~/.config/qodex/skills`.
- Tool execution happens on the same machine as the CLI.

If `model.base_url` points at a remote endpoint, prompts, code excerpts, skill content, and tool results may leave the machine.

## Tool Boundaries

File tools validate paths against the project root. Absolute paths and parent-directory escapes are rejected for normal file reads, writes, and patch paths.

Write tools require approval unless auto-approval is explicitly enabled. `write_patch` validates file paths before passing the diff to `git apply`.

Command execution supports direct `argv` execution and legacy shell commands. Direct `argv` is preferred because it avoids shell parsing. Legacy shell mode remains for compatibility, but it is treated as a shell-effect tool and requires approval.

## Command Risk Classes

Qodex classifies tools by effect:

- `read`: local inspection only.
- `write`: file changes under the project root.
- `shell`: local command execution.
- `network`: likely network activity such as `curl`, `wget`, `git clone`, `git pull`, `go get`, package installs, `ssh`, `scp`, or `rsync`.
- `destructive`: reserved for future high-risk policy handling.

Network detection is conservative pattern matching. It is intended to make approvals clearer, not to prove a command has no network behavior.

## Approval Policy

The default project config asks before writes, command execution, and likely network commands:

```toml
[approval]
write_files = "ask"
run_commands = "ask"
network = "ask"
```

The `--yes` flag and `approval.auto_approve = true` bypass prompts. These modes are useful for trusted automation but should not be used on unreviewed prompts, skills, or repositories.

## Known Limits

- Shell commands can hide behavior behind scripts, aliases, subshells, and tools that perform network access internally.
- Path validation protects Qodex's built-in file tools, not arbitrary commands run through `run_command`.
- Symlink handling is root-relative but not a full sandbox. A shell command can still follow symlinks according to normal OS rules.
- The TUI approval panel is intentionally minimal. Rich diff rendering before approval is still planned.
- Qodex does not yet persist approval decisions as first-class records.

## Recommended Usage

- Keep `llama.cpp` bound to localhost for the local-first workflow.
- Prefer model requests that use direct `argv` command calls.
- Review generated patches before approval.
- Decline broad or unclear command requests and ask for a narrower command.
- Do not enable auto-approval for unfamiliar repositories or skills.
