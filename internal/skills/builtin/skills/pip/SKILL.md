# pip / pip3

Use this skill for installing Python packages with `pip_install` and `pip3_install`.

## Workflow

1. Use `pip_install` for generic pip installs. Accepts `package` (required), `version`, `requirements`, `index_url`, `extra_index_url`, `upgrade`, and `user`.
2. Use `pip3_install` when you need to explicitly target the `pip3` binary. Same parameters as `pip_install`.
3. For requirements files, use `requirements` instead of `package`.
4. For version pinning, use `version` (e.g. `1.2.3`).
5. For private indexes, use `index_url` and `extra_index_url`.

## Tips

- Common flags: `--upgrade` to force reinstall, `--user` for user-site installs.
- Use `requirements` for bulk installs from `requirements.txt`.
- Prefer `pip_install` / `pip3_install` over `run_command` for structured approval and metadata.
- Use `python_run` with `eval: "import pkg; print(pkg.__version__)"` to verify installs.

## Safety

- `pip_install` and `pip3_install` download and execute code from PyPI or other indexes. Always review the package name before installing.
- Both tools are network operations and respect the approval policy.
- Avoid `--user` in shared environments; prefer virtual environments or conda.
