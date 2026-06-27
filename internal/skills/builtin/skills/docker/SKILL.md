# Docker

Use this skill for container operations with `docker_build` and `docker_run`.

## Workflow

1. Use `docker_build` to build images. Accepts `dockerfile`, `tag`, `context`, `no_cache`, `pull`, `build_args`, `target`.
2. Use `docker_run` to start containers. Accepts `image` (required), `command`, `name`, `ports`, `volumes`, `env`, `detach`, `rm`, `network`, `privileged`.
3. Prefer `docker_run` over `run_command` for standard container execution.

## Tips

- Use `tag` to label images (e.g. `myapp:latest`).
- Use `ports` like `["8080:80"]` for port mappings.
- Use `volumes` like `["./data:/app/data"]` for bind mounts.
- Use `env` for environment variables (e.g. `{"NODE_ENV": "production"}`).
- Use `detach: true` for long-running containers.
- Use `rm: true` to auto-remove containers on exit.

## Safety

- `docker_run` with `privileged: true` gives the container full host access. Avoid unless necessary.
- `docker_build` with `pull: true` pulls base images from the network.
- Review `dockerfile` and build context before building.
