# .NET (dotnet)

Use this skill for .NET projects using the `dotnet` CLI and `msbuild`.

## Workflow

1. Start with `list_files` and `search_text` to locate `.csproj`, `.sln`, `Program.cs`, and other project files.
2. Inspect project files with `read_file` before running commands.
3. Use `nuget_restore` to restore packages when needed.
4. Use `dotnet_build` to build. It accepts `project`, `configuration` (Debug/Release), `framework`, and `output`.
5. Use `dotnet_test` to run tests. It accepts `project`, `filter` (e.g. `FullyQualifiedName~MyTest`), `configuration`, and `logger`.
6. Use `dotnet_run` to execute an app. It accepts `project`, `args`, `configuration`, and `framework`.
7. For direct MSBuild invocation (e.g. custom targets, Windows-specific builds), use the `msbuild` tool.

## Tips

- Common configurations: `Debug` and `Release`. Use `Release` for performance testing.
- Use `no_restore: true` on `dotnet_build` or `dotnet_test` after a `nuget_restore`.
- For solution-wide operations, pass the `.sln` file to `project`.
- Use `filter` with `dotnet_test` to narrow to specific tests (FullyQualifiedName, Category, Priority).
- On Windows, `msbuild` may be preferred for certain project types (e.g. .NET Framework, WPF).

## Safety

- `dotnet run` executes the app. Inspect it before running.
- `dotnet build` and `dotnet test` are generally safe and auto-approved.
- `nuget_restore` and `nuget_install` download packages and are treated as network operations.
