# NuGet

Use this skill for NuGet package management with `nuget_restore` and `nuget_install`.

## Workflow

1. Use `nuget_restore` to restore packages for a project or solution. Accepts `project` (path to .csproj/.sln), `packages` directory, `source`, `config_file`, and `force`.
2. Use `nuget_install` to install a specific package version into a packages folder. Accepts `package` (required), `version`, `output_dir`, `source`, `config_file`, and `prerelease`.
3. Use `dotnet restore` indirectly via `dotnet_build` with `no_restore` only when you've already restored.

## Tips

- Common sources: `https://api.nuget.org/v3/index.json` (default), private feeds.
- Use `nuget_restore` before `dotnet_build` or `dotnet_test` in CI-like workflows for explicit control.
- Use `nuget_install` to retarget or pin specific package versions without modifying project files.
- Place packages in a shared `packages/` directory with `output_dir` when working with multiple projects.

## Safety

- Both tools are network operations and respect the approval policy.
- Review `package` and `source` inputs carefully before installing.
- Use `prerelease: true` only when you specifically want prerelease packages.
