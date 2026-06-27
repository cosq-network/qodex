# System Administration

Use this skill for system administration tasks with dedicated tools.

## Workflow

1. Use read-only tools first to inspect system state.
2. Use write/destructive tools only after confirmation and with approval.
3. Prefer dedicated tools over `run_command` for standard operations.

## Read-Only Tools

- `grep_search` — search file contents with grep (supports -r, -i, -F, --include, --exclude, context)
- `find_files` — find files/directories by name, type, size, mtime
- `tail_file` — read end of files (supports -n, -c, -f)
- `awk_process` — process text with awk scripts
- `ps_list` — list processes (supports -e, -u, -o, --sort)

## File System Tools

- `chmod_change` — change permissions (e.g. mode: "755", "u+w", recursive: true)
- `chown_change` — change owner:group (e.g. owner: "www-data", group: "www-data", recursive: true)

## User Management (Dangerous)

- `user_add` — create users (requires username; supports home, shell, groups, uid, gid, system)
- `user_del` — remove users (requires username; supports --remove, --force)

## Tips

- Use `grep_search` instead of `search_text` when you need recursive search, context lines, or fixed strings.
- Use `find_files` with `type: "f"` for files or `type: "d"` for directories.
- Use `tail_file` with `follow: true` for log streaming (short-lived only).
- Use `awk_process` for column-based text extraction (e.g. `$1`, `NR==1`).
- Use `chmod_change` with symbolic modes like `u+x` or numeric like `755`.
- Use `chown_change` with `owner:group` format for both at once.
- User management tools require approval and may fail without root privileges.

## Safety

- `chmod_change`, `chown_change`, `user_add`, and `user_del` are **destructive** and require explicit approval.
- Never run `user_del --remove` or `user_del --force` without double-checking the username.
- Avoid `chmod_change` with recursive on large directories without previewing.
- User management on production systems can cause outages. Review carefully.
