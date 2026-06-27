# Node.js

Use this skill for running Node.js scripts and evaluating inline JavaScript with `node_run`.

## Workflow

1. Use `node_run` to execute JavaScript. Accepts `script` (path to a `.js` file) or `eval` (inline code), plus optional `args` and `vm_args`.
2. Either `script` or `eval` is required. Do not provide both.
3. Use `args` to pass CLI arguments to the script (e.g. `["--port", "3000"]`).
4. Use `vm_args` for Node.js VM flags (e.g. `["--max-old-space-size=4096"]`).
5. Set `timeout_seconds` for long-running scripts (default 60s).

## Tips

- Prefer `script` over `eval` for anything beyond a one-liner.
- Inspect the script with `read_file` before running it.
- Use `npm_command` or `npx_command` for package-based workflows instead of shelling out manually.

## Safety

- Do not run untrusted scripts without inspecting them first.
- Use `review_changes` after a script generates or modifies files.
