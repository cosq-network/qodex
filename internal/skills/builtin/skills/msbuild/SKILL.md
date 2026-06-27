# MSBuild

Use this skill for direct `msbuild` invocation in .NET projects, especially on Windows or for custom build targets.

## Workflow

1. Use the `msbuild` tool for direct MSBuild builds. Accepts `project` (required), `target`, `configuration`, `property` (map), and `max_cpu_count`.
2. Prefer `dotnet_build` for cross-platform .NET Core/5+ projects unless you need MSBuild-specific targets or properties.
3. Use `msbuild` when building .NET Framework projects, WPF, or Windows-specific workloads where `dotnet build` is insufficient.

## Tips

- Common targets: `Build`, `Clean`, `Rebuild`, `Publish`, `Restore`.
- Use `target: "Publish"` with `configuration: "Release"` for deployment-ready outputs.
- Pass MSBuild properties via `property` map (e.g. `{"DeployOnBuild": "true", "PublishProfile": "FolderProfile"}`).
- Use `max_cpu_count` to enable parallel builds (e.g. `4`).

## Safety

- `msbuild` can execute custom targets and imported `.targets` files. Inspect project files before running.
- `Publish` targets may copy or archive outputs. Review the scope with `review_changes`.
