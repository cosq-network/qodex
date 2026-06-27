# Archives

Use this skill for managing archive files (ar, tar, zip) with dedicated tools.

## Workflow

1. Use `ar_list`, `tar_list`, or `zip_list` to inspect archive contents before extracting.
2. Use `ar_create`, `tar_create`, or `zip_create` to create archives.
3. Use `ar_extract`, `tar_extract`, or `zip_extract` to extract archives to a directory.
4. Always validate the archive path with `safePath` semantics before operating.

## Tools

### ar (Unix archive)
- `ar_create` — create an ar archive; accepts `archive` (required) and `files` (list)
- `ar_extract` — extract an ar archive; accepts `archive` (required) and `output_dir`
- `ar_list` — list ar contents; accepts `archive` (required)

### tar (tape archive)
- `tar_create` — create a tar archive; accepts `archive` (required), `files` (list), `compress` (gz/bz2/xz)
- `tar_extract` — extract a tar archive; accepts `archive` (required), `output_dir`, `compress`, `strip_components`
- `tar_list` — list tar contents; accepts `archive` (required), `verbose`, `compress`

### zip
- `zip_create` — create a zip archive; accepts `archive` (required) and `files` (list)
- `zip_extract` — extract a zip archive; accepts `archive` (required) and `output_dir`
- `zip_list` — list zip contents; accepts `archive` (required)

## Tips

- Use `tar_create` with `compress: "gz"` for gzipped tarballs (`.tar.gz`).
- Use `strip_components` with `tar_extract` to flatten nested directories.
- Use `working_dir` to resolve relative paths consistently.
- For large archives, increase `timeout_seconds`.

## Safety

- Archive operations can overwrite files in the output directory. Review paths before extracting.
- `ar_extract`, `tar_extract`, and `zip_extract` write to disk and require approval.
- Avoid extracting archives from untrusted sources without inspection.
