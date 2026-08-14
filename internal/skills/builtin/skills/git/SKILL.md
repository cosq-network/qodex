# Git

Use this skill for Git operations. Prefer the dedicated git tools over raw shell commands when available.

## Workflow

1. Start with `git_workspace_summary` for branch, staged/unstaged state, diff statistics, and recent commits.
2. Use `git_status`, `git_diff`, and `review_changes` for detailed inspection before edits or commits.
3. Keep commits focused: use `git_stage` with explicit paths, then `git_commit` with a concise message.
4. Save a `git_snapshot` before risky multi-file operations and use `git_restore_snapshot` only after reviewing the target diff.
5. Use `git_branch` and `git_worktree` for isolated parallel work instead of modifying unrelated branches.
6. Use `git_undo` to create a history-preserving revert; never reset or force-push shared history without explicit user direction.
7. For unsupported operations, use `run_command` with `argv: ["git", ...]` rather than shell strings.

## Commit Messages

- Use Conventional Commits when the project follows them.
- Keep the first line under 72 characters.

## Safety

- Avoid force-push to shared branches.
- Review `review_changes` before committing.
