# Base64

Use this skill for encoding and decoding base64 data with `base64_encode`. Prefer the dedicated tool over raw shell for base64 operations.

## Workflow

1. Use `base64_encode` with `input` for inline string data, or `path` to read from a file.
2. Set `decode: true` to decode base64 data.
3. Use `wrap` to set line wrapping (e.g. `76` for MIME-style wrapping).

## Tips

- For large files, use `path` rather than embedding the entire content in `input`.
- Use `decode: true` when the model needs to extract original content from a base64 string.
- Combine `base64_encode` with `run_command` for piping only when the dedicated tool is insufficient.

## Safety

- Do not decode untrusted base64 payloads without inspecting the result first.
- Use `read_file` on decoded output if it appears suspicious.
