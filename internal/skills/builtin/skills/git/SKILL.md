# Git

Use this skill for Git operations. Prefer the dedicated git tools over raw shell commands when available.

## Workflow

1. Start with `git_status` and `git_diff` to inspect the current state.
2. Use `git_log` to review recent commits (defaults to last 20, `oneline: true` for compact view).
3. For complex operations, use `run_command` with `argv: ["git", ...]` rather than shell strings.

## Commit Messages

- Use Conventional Commits when the project follows them.
- Keep the first line under 72 characters.

## Safety

- Avoid force-push to shared branches.
- Review `review_changes` before committing.
