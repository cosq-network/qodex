# Clang

Use this skill for Clang-based C/C++ codebases.

## Formatting

- Use `clang_format` with `in_place: true` to format a file, or `in_place: false` to check formatting without modifying.
- Supply an optional `style` (e.g. "file", "Google", "LLVM").

## Diagnostics

- Use `clang_tidy` to run static analysis on a file.
- Supply `checks` to target specific rule sets (e.g. "clang-analyzer-*", "bugprone-*").
- Use `fix: true` to auto-apply safe fixes.

## Workflow

1. Inspect the file with `read_file`.
2. Run `clang_tidy` before editing to surface existing issues.
3. Format with `clang_format --in-place` after changes.
4. Re-run `clang_tidy` to verify fixes.
