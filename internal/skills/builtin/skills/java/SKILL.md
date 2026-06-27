# Java

Use this skill for Java projects, including compilation and execution with `javac` and `java`.

## Workflow

1. Start with `list_files` and `search_text` to locate `.java` source files and build files (`pom.xml`, `build.gradle`).
2. Inspect source with `read_file` before editing.
3. Use `javac_compile` to compile a specific source file. Supply `source` (required), optional `classpath`, `output_dir`, `sourcepath`, and `release`.
4. Use `java_run` to execute a compiled class or JAR. Supply `main_class` (for classpath execution) or `jar` (for `-jar`), plus optional `args`, `vm_args`, and `classpath`.
5. For multi-file projects, compile the entry point; `javac` resolves dependencies via the classpath.

## Tips

- Set `output_dir` to keep build artifacts out of the source tree.
- Use `release` to target a specific Java version (e.g. "11", "17", "21").
- If the project uses Maven or Gradle, prefer `run_command` with `argv: ["mvn", "compile"]` or `argv: ["./gradlew", "build"]` instead of raw `javac`.
- Run `java_run` from the directory containing the compiled classes, or set `classpath` accordingly.

## Safety

- Do not run untrusted classes with elevated permissions.
- Prefer explicit classpaths over wildcards when possible.
