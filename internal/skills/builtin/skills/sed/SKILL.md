# Sed

Use this skill for text editing with `sed_edit`. Prefer sed for quick, line-based transformations when writing a full file with `write_file` would be excessive.

## Workflow

1. Use `sed_edit` to apply an expression to a file. Accepts `path` (required), `expression` (required), and `in_place` (default `false`).
2. With `in_place: false` (default), sed outputs the transformed text to the tool result without modifying the file.
3. With `in_place: true`, the file is modified in-place. This requires approval.
4. Set `expression` to a sed expression (e.g. `s/old/new/g`, `2d`, `/^#/d`).

## Tips

- Always preview (without `in_place`) before applying in-place edits.
- Use `read_file` to verify the result after `sed_edit`.
- For complex multi-step edits, prefer `write_patch` if the change spans many lines.
