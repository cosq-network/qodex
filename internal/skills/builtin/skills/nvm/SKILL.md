# nvm

Use this skill for managing Node.js versions with `nvm_use`.

## Workflow

1. Use `nvm_use` to switch the active Node.js version. Accepts `version` (e.g. `18`, `20`, `lts/iron`, `node`).
2. The tool sources `nvm.sh` from common install locations (`~/.nvm/nvm.sh`, `/usr/local/nvm/nvm.sh`, `/opt/homebrew/nvm/nvm.sh`).

## Limitations

- `nvm_use` runs in a subprocess, so environment changes (PATH, etc.) do not persist to the parent shell or subsequent tool executions.
- For persistent version switching, run `nvm use` directly in your terminal before starting Qodex.
- After switching versions, use `node_run` or `npm_command` to verify the active version.

## Tips

- Use `node_run` with `eval: "process.version"` to check the active Node version.
- If `nvm_use` fails, fall back to `run_command` with `argv: ["bash", "-c", "source ~/.nvm/nvm.sh && nvm use <version> && node -v"]`.

## Safety

- Switching Node versions can break `node_modules` compiled for a different version. Use `npm rebuild` after switching if needed.
