# Make

Use this skill for Makefile-driven projects.

## Workflow

1. Use `make_build` to run make. Supply `target` (default empty for the default target), `build_dir` (default project root), and `jobs` for parallel builds.
2. If no Makefile exists, use `list_files` to confirm structure before suggesting one.

## Tips

- Use `jobs` for parallel builds (e.g. `jobs: 4`).
- Inspect output carefully; make stops at the first failed target.
- Use `run_command` with `argv: ["make", "-n", "target"]` to dry-run if needed.
