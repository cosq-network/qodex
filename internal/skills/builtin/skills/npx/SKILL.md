# npx

Use this skill for executing packages and binaries with `npx_command`.

## Workflow

1. Use `npx_command` to run a package without installing it globally. Accepts `package` (required) and optional `args`.
2. `npx_command` runs `npx -y <package>`, which auto-installs missing packages into a cache.
3. For packages with bin entries, use the package name directly. For package executables, use `package run-script <script>`.

## Tips

- Use `npx_command` for one-off tools like `create-react-app`, `typescript`, `eslint`, etc.
- Set `args` to forward arguments (e.g. `args: ["src/index.ts"]` for `npx ts-node src/index.ts`).
- Prefer `npx_command` over `run_command` for npx workflows to get structured approval and metadata.

## Safety

- `npx` may download and execute code from the registry. Always review the package before running.
- `npx_command` is classified as a network operation and requires approval.
