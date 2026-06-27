# Dart

Use this skill for Dart language development, including scripts, CLI tools, and package management.

## Workflow

1. Use `dart_run` to execute Dart scripts. Accepts `script` (path) and optional `args`.
2. Use `dart_analyze` to check for errors and warnings. Accepts `path` and `fatal_warnings`.
3. Use `dart_format` to format Dart code. Accepts `path` and `set_exit_if_changed`.
4. Use `pub_get`, `pub_upgrade`, `pub_add`, and `pub_remove` for dependency management (same as Flutter).

## Tips

- Prefer `dart_run` over `run_command` for Dart execution to get structured approval.
- Use `dart_analyze` in CI-like workflows before committing.
- Use `dart_format --set_exit_if_changed` in pre-commit checks.
- For package management, the `pub_*` tools work for both pure Dart and Flutter projects.

## Safety

- `dart_run` executes arbitrary Dart code. Inspect scripts before running.
- `pub_add` and `pub_upgrade` download packages and are network operations.
