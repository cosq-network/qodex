# Wget

Use this skill for file downloads and mirroring with wget. Prefer the dedicated `wget` tool over raw `run_command` for download operations.

## Workflow

1. Start with `wget` for single-file downloads. Supply `url` (required) and optional `output`.
2. For recursive downloads or mirroring, set `recursive: true` and configure `page_requisites`, `max_depth`, `reject`, and `exclude_directories`.
3. For interrupted downloads, set `continue: true` to resume.
4. Use `output` to write to a specific file under the project root; otherwise files land in the project root.

## Safety

- The `wget` tool rejects dangerous URL schemes.
- Recursive downloads are network-heavy and respect the approval policy.
- Avoid recursive downloads to paths outside the project root.
- Review `review_changes` after large downloads.
