# npm

Use this skill for package and script management with `npm_command`.

## Workflow

1. Use `npm_command` to run any npm subcommand. Accepts `command` (e.g. `install`, `run`, `test`, `build`) and optional `args`.
2. For running scripts defined in `package.json`, use `script` instead of `command` (e.g. `script: "build"` runs `npm run build`).
3. Combine with `run_command` only when `npm_command` does not cover the use case.

## Tips

- Common commands: `install` (or `i`), `run <script>`, `test`, `build`, `lint`, `start`.
- Use `npm_command` for `npm install`, `npm ci`, `npm run <script>`.
- Inspect `package.json` with `read_file` before running unfamiliar scripts.

## Safety

- `npm install` and `npm ci` modify `node_modules` and may execute postinstall scripts. Review before running.
- `npm publish` requires authentication and should be explicit.
