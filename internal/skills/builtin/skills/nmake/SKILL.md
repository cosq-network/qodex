# NMake (nmake_build)

Use this skill for building with Microsoft NMake on Windows. Prefer `nmake_build` over `make_build` when working with Visual Studio projects, Windows drivers, or makefiles that target NMake-specific syntax.

## Workflow

1. Use `list_files` and `search_text` to locate `Makefile`, `makefile`, or `.vcxproj` files.
2. Use `nmake_build` to build. Accepts `target` (default target if empty), `build_dir` (default project root), and `macro` (key-value map of NMake macro overrides).
3. NMake macros are passed as `MACRO=value` arguments. Use the `macro` field instead of shelling out with `run_command`.
4. If NMake is not found, use `msbuild` or `dotnet_build` as alternatives.

## Tips

- Common targets: `all`, `clean`, `rebuild`, `debug`, `release`.
- Pass build configuration via macros (e.g. `macro: {"CFG": "release"}`).
- Inspect the makefile with `read_file` before running to understand targets and macros.
- Use `review_changes` after build artifacts are generated.

## Safety

- `nmake_build` executes arbitrary commands defined in makefiles. Inspect makefiles before running.
- Windows-specific builds may modify system files or registry. Review carefully.
