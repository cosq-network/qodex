# Curl

Use this skill for HTTP operations with curl. Prefer the dedicated `curl` tool over raw `run_command` for fetch operations.

## Workflow

1. Start with `curl` for simple fetches. Supply `url` (required), optional `method`, `headers`, `data`, `output`, and `location`.
2. For binary downloads, supply `output` to write to a file under the project root.
3. For API calls, set `method` and `headers` explicitly.
4. Use `max_time` to bound long-running requests.

## Safety

- The `curl` tool rejects dangerous URL schemes (`file://`, `ftp://`, `gopher://`, etc.).
- The tool runs as a network operation and respects the approval policy.
- Avoid piping curl output to shell commands. Use `output` to save files instead.
- Do not use `curl` to execute remote scripts.
