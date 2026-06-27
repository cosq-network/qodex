# Python

Use this skill for running Python scripts and inline code with `python_run` and `python3_run`.

## Workflow

1. Use `python_run` for Python 2 or generic `python` invocations. Accepts `script` (path) or `eval` (inline code), plus `args` and `vm_args`.
2. Use `python3_run` when you need to explicitly target Python 3. Same parameters as `python_run`.
3. Prefer `script` over `eval` for anything beyond a one-liner.
4. Inspect scripts with `read_file` before running them.

## Tips

- Use `vm_args` for flags like `-O` (optimize), `-B` (no bytecode), or `-m` (module execution).
- Use `args` to pass CLI arguments (e.g. `["--port", "3000"]`).
- For package management, use `pip_install`, `pip3_install`, `conda_install`, or `conda_create` instead of shelling out.
- For running modules, prefer `run_command` with `argv: ["python", "-m", "module_name"]` when the dedicated tools don't fit.

## Safety

- Do not run untrusted scripts without inspecting them first.
- Use `review_changes` after a script generates or modifies files.
