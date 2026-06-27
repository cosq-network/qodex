# Conda

Use this skill for managing conda environments and packages with `conda_install` and `conda_create`.

## Workflow

1. Use `conda_create` to create a new environment. Accepts `name` (required), `python`, `packages`, `channel`, and `timeout_seconds`.
2. Use `conda_install` to install packages into the current environment. Accepts `package` (required), `channel`, and `version`.
3. For environment.yml-driven workflows, use `conda_create` with `packages` mapped from the file, or fall back to `run_command`.

## Tips

- Use `conda_create` with `python` to pin the Python version (e.g. `python: "3.11"`).
- Use `channel` to prioritize specific conda channels (e.g. `conda-forge`, `nvidia`, `bioconda`).
- Use `packages` with `conda_create` to pre-install common dependencies.
- For scientific Python stacks, consider `packages: ["numpy", "pandas", "scikit-learn"]`.

## Safety

- `conda_install` and `conda_create` download packages and are network operations.
- Review `package` and `channel` inputs carefully before installing.
- Avoid modifying the `base` environment; create dedicated environments instead.
