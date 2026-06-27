# Windows Production Readiness Checklist

This document tracks remaining validation work before Qodex is considered production-ready on Windows. Linux and macOS are covered by existing CI; Windows has compile fixes in place but has not been runtime-validated.

## Already Fixed

These items were addressed in commit `319c775`:

- [x] `syscall.SIGTERM` compile error on Windows (`cmd/qodex/signals_windows.go`)
- [x] `os.Kill` compile error on Windows (`internal/model/stop_windows.go`)
- [x] `SysProcAttr.Setpgid` Unix-only usage (`internal/model/process_unix.go` / `process_windows.go`)
- [x] Hardcoded `sh -c` shell commands (`internal/tools/shell_unix.go` / `shell_windows.go`)
- [x] LSP `file://` URI invalid for Windows drive letters (`internal/lsp/client.go`)
- [x] Line-ending assumptions (`\r\n` not normalized in file reads)
- [x] TTY detection using `os.ModeCharDevice` (`cmd/qodex/setup.go` → `go-isatty`)
- [x] Path-separator hardcoding in TUI file sort (`internal/tui/tui.go`)
- [x] Dangerous-path detection missing Windows roots (`internal/tools/tools.go`)
- [x] Symlink extraction requiring elevation on Windows (`internal/model/manager.go`)
- [x] File permission mapping (`0o644` → read-only on Windows)
- [x] Test failure: `TestRunCommandWithFakeEndpoint` (`cmd/qodex/main_test.go`)

## Remaining Validation Required

### 1. Build And Basic CLI

**Risk**: Medium  
**Where**: Full binary build on Windows.

**Steps**:
```powershell
git clone https://github.com/benoybose/qodex.git
cd qodex
go build -o qodex.exe ./cmd/qodex
.\qodex.exe version
.\qodex.exe --help
```

**Pass criteria**: Binary builds without CGO, `version` prints metadata, `--help` lists all commands including `serve` and `models`.

---

### 2. Setup Wizard On Windows

**Risk**: High  
**Where**: `cmd/qodex/setup.go`, `internal/model/manager.go`.

**Steps**:
1. Open **Windows Terminal** (not `cmd.exe` or PowerShell ISE).
2. Run `.\qodex.exe` in an empty directory with no `.qodex` folder present.
3. Verify the interactive prompt appears: "Run the setup wizard now? [Y/n]:"
4. Select backend `1` (llama.cpp).
5. Accept the default model.
6. Let the installer run through download + extraction.

**Pass criteria**:
- TTY detection correctly identifies Windows Terminal as interactive.
- llama.cpp binary downloads and extracts to `%USERPROFILE%\.config\qodex\bin\`.
- `llama-server.exe` exists after extraction.
- `qodex.exe serve status` shows the server as running.

**Known concern**: The llama.cpp tarball contains symlinks (e.g., `llama-server` → `llama-server.exe` or shared libraries). Symlinks are skipped on Windows, which means:
- If the tarball ships `llama-server.exe` as a regular file, extraction works.
- If it ships `llama-server` as a symlink to a `.exe` or `.dll`, the binary may be missing after extraction.

**Workaround if it fails**: Manually download the Windows zip from `https://github.com/ggml-org/llama.cpp/releases` and extract to `%USERPROFILE%\.config\qodex\bin\`, then re-run `qodex serve start`.

---

### 3. Model Download On Windows

**Risk**: High  
**Where**: `internal/model/models.go`.

**Steps**:
1. After setup, run `.\qodex.exe models list`.
2. Run `.\qodex.exe models download qwen2.5-coder-7b-q4_k_m.gguf`.

**Pass criteria**:
- File downloads to `%USERPROFILE%\.config\qodex\models\qwen2.5-coder-7b-q4_k_m.gguf`.
- File size matches the HuggingFace metadata (≈4.7 GB for Q4_K_M).
- Downstream `qodex serve start` can load it.

**Known concern**: HuggingFace `resolve/main/` URLs redirect through CDN. The Go `http.DefaultClient` follows redirects, but corporate proxies or firewalls on Windows may block or modify them.

---

### 4. Server Lifecycle (serve start / stop / status)

**Risk**: Medium  
**Where**: `internal/model/manager.go`.

**Steps**:
```powershell
.\qodex.exe serve status     # should show running after setup
.\qodex.exe serve stop      # should stop cleanly
.\qodex.exe serve status    # should show not running
.\qodex.exe serve start     # should start again
```

**Pass criteria**:
- `start` returns within 15 seconds.
- `stop` kills the process and removes the PID file at `%USERPROFILE%\.config\qodex\run\server.pid`.
- No zombie `llama-server.exe` processes linger after `stop`.

**Known concern**: Windows `os.Process.Kill()` is a hard termination. If the llama server has child threads holding file locks, the PID file removal or model file may be briefly locked on the next start.

---

### 5. TUI And Chat

**Risk**: Medium  
**Where**: `internal/tui/tui.go`, Bubble Tea / Lipgloss.

**Steps**:
```powershell
.\qodex.exe chat
# Type a message and verify streaming output renders correctly.
# Press q or Ctrl+C to exit.
```

**Pass criteria**:
- Token-by-token streaming renders without raw ANSI escape codes visible.
- Multi-line input, file autocomplete (`@`), and approval prompts all work.
- Exit via `q` or `Ctrl+C` is clean (no panic or zombie process).

**Known concern**: Classic `cmd.exe` does not support ANSI escape sequences. This must be tested in **Windows Terminal**, **PowerShell 7+**, or **ConEmu**. If the user is on `cmd.exe`, rendering will be broken.

---

### 6. Shell Command Execution (run_command)

**Risk**: Medium  
**Where**: `internal/tools/tools.go`, `internal/agent/agent.go`.

**Steps**:
From within `qodex chat` or `qodex run`:
```
Run the command: dir
Run the command: echo hello
```

**Pass criteria**:
- `dir` and `echo` succeed via `cmd.exe /C`.
- Multi-command strings using `&&` or `||` work.
- Commands with paths like `C:\Users\...` are recognized and not rejected by the dangerous-path filter.

**Known concern**: `cmd.exe /C` does not support bash syntax (`$()`, `[[ ]]`, pipes to `head`/`grep`). If the agent emits bash-specific commands, they will fail. This is expected behavior — the agent should be prompted to use Windows-native syntax or WSL2.

---

### 7. LSP Tools With A Windows LSP Server

**Risk**: Medium  
**Where**: `internal/lsp/client.go`.

**Steps**:
1. Install a Windows LSP server, e.g.:
   ```powershell
   go install golang.org/x/tools/gopls@latest
   ```
2. From a Go project:
   ```
   Run lsp diagnostics on .\main.go
   Run lsp definition on .\main.go line 1 character 1
   ```

**Pass criteria**:
- `pathToURI` produces `file:///C:/Users/.../main.go`.
- LSP server starts, responds, and returns diagnostics.
- File paths in tool output use `C:/Users/...` (forward slashes after normalization).

**Known concern**: `gopls` on Windows requires Go in `PATH`. LSP tool execution also depends on `cmd.exe /C` launching the server correctly.

---

### 8. Skill Scripts (run_script)

**Risk**: Low-Medium  
**Where**: `internal/agent/agent.go`.

**Steps**:
1. Create a skill with a simple script:
   ```toml
   # .qodex/skills/test/skill.toml
   name = "test"
   description = "test skill"
   scripts.run = "echo skill-ran"
   ```
2. Invoke it from chat: `/skill test` then a prompt that triggers the script.

**Pass criteria**:
- Script executes and output appears in the tool result.
- `cmd.exe /C` handles the script command.

**Known concern**: Any bundled or user-authored skill scripts using bash-specific syntax will fail silently on Windows. There is no portability lint yet.

---

### 9. File Paths In All Tool Outputs

**Risk**: Low  
**Where**: `internal/tools/tools.go`, `internal/tui/tui.go`, `internal/lsp/lsp.go`.

**Steps**:
Run tools that return file paths:
```
list files in .
read file .\go.mod
search text for "package main"
```

**Pass criteria**:
- Paths in tool output use the OS-native separator (`\` on Windows).
- Relative paths (e.g., from `filepath.Rel`) are correct, not double-backslashed or empty.
- No `\r` characters appear at the end of lines in diffs or search results.

---

## Validation Matrix

| # | Area | Command / Action | Pass Criteria | Risk |
|---|------|------------------|---------------|------|
| 1 | Build | `go build` | Binary + `--help` works | Medium |
| 2 | Setup wizard | `qodex` in empty dir | Interactive, installs backend | High |
| 3 | Model download | `qodex models download ...` | GGUF in `%USERPROFILE%\.config\qodex\models\` | High |
| 4 | Server lifecycle | `serve start/stop/status` | Clean start/stop, no zombies | Medium |
| 5 | TUI chat | `qodex chat` | Streaming renders, clean exit | Medium |
| 6 | Shell commands | `run_command` with `dir`, `&&` | `cmd.exe /C` works | Medium |
| 7 | LSP tools | `lsp_diagnostics` with `gopls` | `file:///C:/` URIs, correct output | Medium |
| 8 | Skill scripts | `run_script` from a test skill | Executes via `cmd.exe /C` | Low-Medium |
| 9 | Paths in output | `list_files`, `read_file`, `search_text` | OS-native separators, no `\r` | Low |

## How To Add CI For Windows

Add a job to `.github/workflows/ci.yml`:

```yaml
windows:
  runs-on: windows-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version: '1.26'
    - run: go build -v ./...
    - run: go vet ./...
    - run: go test ./...
```

This validates compile and unit tests. Items 2–9 above require a real Windows runner with Windows Terminal for manual verification.
