# System Packages

Use this skill for managing system-level packages across operating systems. Choose the appropriate tool based on the target platform and available package manager.

## Available Tools

### Debian / Ubuntu
- `apt_install` — modern apt frontend. Accepts `package` (required), `update` (bool).
- `apt_get_install` — traditional apt-get. Same parameters as `apt_install`.

### Fedora / RHEL
- `dnf_install` — DNF package manager. Accepts `package` (required), `refresh` (bool).

### Windows
- `winget_install` — Windows Package Manager. Accepts `package` (required), `version`, `scope`, `source`.
- `choco_install` — Chocolatey. Accepts `package` (required), `version`, `source`, `force`.

### Snap (cross-distro)
- `snap_install` — Snap packages. Accepts `package` (required), `classic` (bool), `channel`.

### macOS / Linux
- `brew_install` — Homebrew. Accepts `package` (required), `cask` (bool), `tap`.

## Workflow

1. Detect the OS and available package manager with `run_command` or by inspecting the project environment.
2. Prefer the native package manager for the platform (apt on Debian/Ubuntu, dnf on Fedora, brew on macOS, winget on Windows).
3. Inspect the package name carefully before installing.
4. Use version fields when pinning is required.
5. All install operations are network operations and require approval.

## Tips

- On Debian/Ubuntu, `apt_install` is preferred over `apt_get_install` for newer scripts, but both work.
- Use `snap_install --classic` for packages that need full system access.
- Use `brew_install --cask` for GUI applications and large binary packages.
- Use `winget_install --source` to target specific repositories.

## Safety

- All package installation tools download and execute code. Always review the package name and source.
- Use `run_command` with `--dry-run` or equivalent flags when supported to preview changes.
- Avoid force-installing packages (`choco_install --force`, `snap_install --dangerous`) unless you understand the implications.
