# CMake

Use this skill for CMake-based C/C++ projects.

## Workflow

1. Start with `list_files` to locate `CMakeLists.txt` and the project structure.
2. Use `cmake_configure` to generate the build system. Supply `build_dir` (default "build"), `source_dir`, and optional `generator` or `defs`.
3. Use `cmake_build` to compile. Supply `build_dir`, optional `target`, and optional `config` (Release/Debug).

## Tips

- Prefer out-of-source builds (`build_dir` separate from source).
- If `cmake_configure` fails, inspect `CMakeLists.txt` before retrying.
- Use `review_changes` after modifying `CMakeLists.txt` or generated files.
