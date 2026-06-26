# Managed Backend: Qodex-Managed llama.cpp

## Goal

Qodex downloads, starts, stops, and monitors its own `llama.cpp` server process — no manual setup required. A user can run `qodex chat` on a fresh machine and be talking to a local model within minutes.

## Constraints

- **No CGO / native extension** dependencies in the main binary. Downloads of platform-specific binaries are acceptable.
- **Startup latency** must be under 5 seconds for a warm server. Cold starts (first download + model load) are expected to be slow.
- **Model storage** uses a shared cache at `~/.cache/qodex/models/`. No re-downloads.
- **Disk space** is the user's responsibility. Warn when model + binary would exceed available space.
- **Privacy** — all downloads from GitHub Releases (llama.cpp) and Hugging Face (models). No telemetry.

## Design

### Component: Backend Manager

A new `internal/backend/` package with the following responsibilities:

| Concern | Approach |
|---------|----------|
| Binary download | Fetch the latest `llama.cpp` release asset from `github.com/ggerganov/llama.cpp/releases`. Cache in `~/.cache/qodex/backends/llama.cpp/`. Verify checksum if provided. |
| Binary update | Check for newer version on startup (at most once per 24h). Prompt user before upgrading. |
| Model download | Fetch GGUF files from Hugging Face via the Hugging Face Hub API. Support `hf.co/Qwen/Qwen2.5-Coder-7B-GGUF/resolve/main/qwen2.5-coder-7b-q4_k_m.gguf`-style URLs. |
| Model cache | `~/.cache/qodex/models/<model-name>/`. Index with a local SQLite or JSON manifest. |
| Process lifecycle | Start `llama-server` with the selected GGUF, health-check via `/v1/models`, restart on unexpected exit (max 3 attempts), graceful shutdown on SIGTERM. |
| Port management | Pick an ephemeral port (net.Listen on :0), update config in-memory. Persist the chosen port to `.qodex/config.toml`. |
| Health & metrics | Periodic health checks. Expose status via `qodex doctor --managed` and the TUI status bar. |

### Package Layout

```
internal/backend/
    backend.go          — Manager struct, Start/Stop/Status/Restart
    download.go         — Binary and model download helpers
    process.go          — Process lifecycle (exec, health-check, restart policy)
    models.go           — Model index / cache management
    releases.go         — GitHub Releases API client for llama.cpp assets
```

### Configuration

New `[backend]` section in `.qodex/config.toml`:

```toml
[backend]
mode = "managed"                  # "managed" | "external" (default: "external")
llama_cpp_version = "latest"      # or pinned like "b4735"
model = "qwen2.5-coder-7b-q4_k_m" # shorthand name in the model index
model_url = ""                    # optional: full HF URL overrides shorthand
port = 0                          # 0 = auto
max_restarts = 3
```

When `mode = "external"` (default), behaviour is unchanged — user runs their own server at `model.base_url`.

### CLI Commands

```
qodex backend status        Show managed backend status (running/stopped/downloading)
qodex backend start         Start the managed llama.cpp server
qodex backend stop          Stop it
qodex backend logs          Tail server logs
qodex backend download      Download llama.cpp binary + model (non-blocking, progress bar)
qodex backend list-models   List cached and available models
qodex setup                 Updated wizard: add "auto-manage backend" option
```

### Integration With Existing Commands

**`qodex chat`** / **`qodex run`** / **`qodex review`**:
1. If `backend.mode = managed`, check if server is running.
2. If not running, start it (may trigger binary download + model download on first run).
3. Wait for health check to pass (with progress indicator).
4. Proceed normally — the agent uses the local endpoint.

**`qodex doctor`**:
- Extended to show managed backend status.
- If managed, run diagnostics on the local process (PID, uptime, port, model loaded).

### Download Flow

```
User runs qodex chat (first time, managed mode)

  ┌─ Does ~/.cache/qodex/backends/llama.cpp/llama-server exist?
  │   YES → skip
  │   NO  → Fetch latest release from GitHub Releases API
  │         Show progress bar: "Downloading llama.cpp (XX MB)..."
  │         Verify checksum (if available)
  │         Extract binary to ~/.cache/qodex/backends/llama.cpp/<version>/
  │         Symlink ~/.cache/qodex/backends/llama.cpp/current -> <version>/
  │
  ├─ Does ~/.cache/qodex/models/qwen2.5-coder-7b-q4_k_m/ exist?
  │   YES → skip
  │   NO  → Resolve model URL from shorthand or config
  │         Show size estimate + ask user to confirm (large download)
  │         Show progress bar: "Downloading model (X GB)..."
  │         Verify file integrity (SHA256 if available)
  │         Store in ~/.cache/qodex/models/<name>/
  │
  ├─ Start llama-server: ~/.cache/.../llama-server -m <model> --host 127.0.0.1 --port <free_port>
  │   Show spinner: "Starting model server..."
  │   Poll /v1/models every 500ms, timeout after 120s
  │   On success → update config with the chosen port
  │   On failure → show error + logs
  │
  └─ Agent runs against http://127.0.0.1:<port>/v1
```

### UX: Progress & Status

All long-running operations show progress:

- **Binary download**: progress bar with MB/s and ETA.
- **Model download**: progress bar with GB/s and ETA. Large file warning with confirmation.
- **Server startup**: spinner with "Starting model server..." and elapsed time.
- **First-time cold start**: estimated total time shown ("This may take a few minutes on the first run.").

### Error Handling

| Failure | Behaviour |
|---------|-----------|
| Download fails (network) | Retry 3 times with backoff. If all fail, suggest `qodex backend download` later. |
| Binary is not executable | `chmod +x` and retry. |
| Server crashes at startup | Show last 20 lines of stderr. Offer to switch to external mode. |
| Server crashes mid-session | Auto-restart (up to `max_restarts`). Agent session continues if backend reconnects. |
| Disk space insufficient | Check before download. Show available vs required. |
| Port conflict | Increment port and retry. |

### Security

- Binaries are downloaded from `github.com/ggerganov/llama.cpp/releases` (official releases).
- Models are downloaded from Hugging Face Hub (`huggingface.co`).
- No execution of untrusted content — the binary is a well-known open-source project.
- Consider verifying GPG signatures or checksums when llama.cpp provides them.
- The process runs as the current user — no `sudo` or elevated permissions.

## Implementation Phases

### Phase 1: Scaffolding & Binary Download

- Create `internal/backend/` package skeleton.
- Implement GitHub Releases API client (fetch latest release asset URL).
- Implement HTTP download with progress bar.
- Cache management (versioned directory + symlink).
- `qodex backend download` CLI command.
- Tests: download with a real/fake HTTP server, cache layout.

**Estimated: 3–5 days.**

### Phase 2: Model Download

- Implement Hugging Face Hub download (direct URL, no SDK dependency).
- Model cache directory layout.
- Large file warning + confirmation.
- Shorthand-to-URL resolution (e.g., `qwen2.5-coder-7b-q4_k_m` → full HF URL).
- `qodex backend list-models`.
- Tests: download mock, cache index, shorthand resolution.

**Estimated: 2–3 days.**

### Phase 3: Process Lifecycle

- Start `llama-server` as a child process.
- Health checking via `/v1/models`.
- Restart policy (max attempts, backoff).
- Graceful shutdown (SIGTERM → SIGKILL timeout).
- Port management (auto-detect free port).
- `qodex backend start/stop/status/logs`.
- Tests: fake server process, health check timeouts, restart count.

**Estimated: 3–4 days.**

### Phase 4: Integration

- Update `buildRuntime` to start managed backend before agent loop.
- Update `qodex setup` wizard with managed mode option.
- Update `qodex doctor` to show backend status.
- Update TUI status bar to show backend state.
- Configuration migration (`[backend]` section in config).
- Tests: end-to-end with fake backend binary.

**Estimated: 2–3 days.**

### Phase 5: Hardening

- Download resumption for large model files (Range headers).
- Concurrent download safety (lock file per model).
- Automatic version checking for llama.cpp (prompt on upgrade).
- Disk space estimation before download.
- Log rotation for server logs.
- Optional: GPU detection (Metal/CUDA fallback flags for llama-server).

**Estimated: 3–5 days.**

## Total Estimated Effort

**13–20 days** for a complete implementation across all phases.

## Open Questions

1. **Model selection**: Should Qodex ship with a recommended model list, or rely entirely on user-provided URLs?
2. **GPU acceleration**: How to detect Metal vs CUDA vs CPU-only and pass the right flag to `llama-server`?
3. **Concurrent sessions**: What if the user runs two `qodex` instances? Share one server or start two?
4. **Model quantization**: Offer to download multiple quantizations (Q4_K_M, Q8_0, etc.) and let the user pick?
5. **Binary trust**: GPG verification of llama.cpp releases is not yet standard — start without it and add later?

## Out Of Scope

- Running non-llama.cpp backends (vLLM, SGLang, Ollama) in managed mode.
- Multi-model serving.
- Fine-tuning or model modification.
- Cloud/remote model fallback.
