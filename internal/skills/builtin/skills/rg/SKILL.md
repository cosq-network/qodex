# Ripgrep (rg)

Use this skill for fast text search with `rg_search`. Prefer `rg_search` over `search_text` when ripgrep is installed, as it supports richer pattern matching.

## Workflow

1. Use `rg_search` for pattern-based searches. Accepts `pattern` (required), `path`, `glob`, `file_type`, `case_sensitive`, `fixed_strings`, and `max_results`.
2. For fixed-string (literal) searches, set `fixed_strings: true` to avoid regex interpretation.
3. Use `glob` to narrow by filename pattern (e.g. `*.go`, `*_test.go`).
4. Use `file_type` to limit by language (e.g. `go`, `python`, `rust`).
5. Adjust `max_results` if the default 100 is too restrictive.

## Tips

- Case-insensitive search is the default (`case_sensitive: false`). Set `case_sensitive: true` when needed.
- For very large repos, narrow with `path` or `max_results` before broadening.
- Use `rg_search` from the project root; it respects the project boundaries set by the agent.
