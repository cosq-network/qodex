package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/benoybose/qodex/internal/model"
)

type Tool struct {
	Name        string
	Description string
	Effect      string
	Parameters  json.RawMessage // JSON Schema for native OpenAI tool calls
	Execute     func(context.Context, json.RawMessage) (Result, error)
}

type Result struct {
	OK       bool                   `json:"ok"`
	Summary  string                 `json:"summary"`
	Content  string                 `json:"content,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type Registry struct {
	root  string
	tools map[string]Tool
	index *ProjectIndex
	mu    sync.Mutex
}

func NewRegistry(projectRoot string) *Registry {
	r := &Registry{root: projectRoot, tools: map[string]Tool{}}
	r.add("list_files", "List files under the project root", "read", listFilesParams, r.listFiles)
	r.add("read_file", "Read a UTF-8 text file", "read", readFileParams, r.readFile)
	r.add("search_text", "Search text in project files", "read", searchTextParams, r.searchText)
	r.add("write_file", "Write a complete file under the project root", "write", writeFileParams, r.writeFile)
	r.add("write_patch", "Apply a unified diff under the project root", "write", writePatchParams, r.writePatch)
	r.add("run_command", "Run a command in the project root", "shell", runCommandParams, r.runCommand)
	r.add("run_script", "Run a pre-approved script from an active skill by description", "shell", runScriptParams, r.runScript)
	r.add("run_tests", "Run project tests. Accepts pattern, file, framework, and timeout_seconds.", "shell", runTestsParams, r.runTests)
	r.add("run_formatter", "Run a formatter such as gofmt, ruff, black, or prettier. Accepts tool and optional path.", "shell", runFormatterParams, r.runFormatter)
	r.add("review_changes", "Review uncommitted changes in the repository. Accepts scope (all, staged, working).", "read", reviewChangesParams, r.reviewChanges)
	r.add("project_index", "Query the lightweight project file and symbol index. Accepts query, symbol, kind, lang, max_results, and summary.", "read", projectIndexParams, r.projectIndex)
	r.add("lsp_diagnostics", "Get language server diagnostics for a file. Accepts path.", "read", lspDiagnosticsParams, r.lspDiagnostics)
	r.add("lsp_definition", "Find the definition of a symbol using a language server. Accepts path, line, and character.", "read", lspDefinitionParams, r.lspDefinition)
	r.add("lsp_find_references", "Find symbol references using a language server. Accepts path, line, and character.", "read", lspFindReferencesParams, r.lspFindReferences)
	r.add("git_status", "Show git status", "read", gitStatusParams, r.gitStatus)
	r.add("git_diff", "Show git diff", "read", gitDiffParams, r.gitDiff)
	r.add("git_log", "Show git log. Accepts limit (default 20) and oneline (bool).", "read", gitLogParams, r.gitLog)
	r.add("cmake_configure", "Run cmake configure. Accepts build_dir (default 'build'), source_dir, generator, and defs.", "shell", cmakeConfigureParams, r.cmakeConfigure)
	r.add("cmake_build", "Run cmake --build. Accepts build_dir (default 'build'), target, and config (Release/Debug).", "shell", cmakeBuildParams, r.cmakeBuild)
	r.add("clang_format", "Run clang-format on a file. Accepts path, in_place (bool), and style.", "shell", clangFormatParams, r.clangFormat)
	r.add("clang_tidy", "Run clang-tidy on a file. Accepts path, checks, and fix (bool).", "shell", clangTidyParams, r.clangTidy)
	r.add("make_build", "Run make. Accepts target, build_dir (default '.'), and jobs.", "shell", makeBuildParams, r.makeBuild)
	r.add("nmake_build", "Run nmake (Windows NMake). Accepts target, build_dir (default '.'), and macro overrides as key-value pairs.", "shell", nmakeBuildParams, r.nmakeBuild)
	r.add("curl", "Fetch a URL with curl. Accepts url (required), method, headers (map), data, output, location, and max_time.", "network", curlParams, r.curlFetch)
	r.add("wget", "Download a URL with wget. Accepts url (required), output, recursive, page_requisites, max_depth, reject, exclude_directories, and continue.", "network", wgetParams, r.wgetFetch)
	r.add("javac_compile", "Compile Java source files with javac. Accepts source (required), classpath, output_dir, sourcepath, and release.", "shell", javacCompileParams, r.javacCompile)
	r.add("java_run", "Run a compiled Java class or JAR. Accepts main_class (for classpath) or jar (for -jar), plus args, vm_args, and classpath.", "shell", javaRunParams, r.javaRun)
	r.add("rg_search", "Search project files with ripgrep. Accepts pattern (required), path, glob, file_type, case_sensitive, fixed_strings, and max_results.", "read", rgSearchParams, r.rgSearch)
	r.add("sed_edit", "Edit text in a file with sed. Accepts path (required), expression, and optional in_place (default outputs to result only). Requires approval for in_place.", "write", sedEditParams, r.sedEdit)
	r.add("base64_encode", "Encode or decode base64. Accepts input (string), path (file), decode (bool), and optional wrap width.", "shell", base64EncodeParams, r.base64Encode)
	r.add("node_run", "Run a Node.js script or eval inline JS. Accepts script (path), eval (inline code), args, vm_args, and timeout_seconds.", "shell", nodeRunParams, r.nodeRun)
	r.add("npm_command", "Run an npm command. Accepts command (e.g. install, run, test), script (for 'npm run <script>'), args, and timeout_seconds.", "shell", npmCommandParams, r.npmCommand)
	r.add("npx_command", "Run a package via npx. Accepts package (required), args, and timeout_seconds.", "network", npxCommandParams, r.npxCommand)
	r.add("nvm_use", "Switch Node.js version via nvm. Accepts version (required). Runs in a shell that sources nvm.sh.", "shell", nvmUseParams, r.nvmUse)
	r.add("dotnet_run", "Run a .NET project or app with 'dotnet run'. Accepts project path, args, configuration, framework, and timeout.", "shell", dotnetRunParams, r.dotnetRun)
	r.add("dotnet_build", "Build a .NET project with 'dotnet build'. Accepts project, configuration, framework, output, no_restore, and timeout.", "shell", dotnetBuildParams, r.dotnetBuild)
	r.add("dotnet_test", "Run tests in a .NET project with 'dotnet test'. Accepts project, filter, configuration, framework, no_build, logger, and timeout.", "shell", dotnetTestParams, r.dotnetTest)
	r.add("msbuild", "Build a project with msbuild. Accepts project path, target, configuration, properties (map), max_cpu_count, and timeout.", "shell", msbuildParams, r.msbuild)
	r.add("nuget_restore", "Restore NuGet packages with 'dotnet restore'. Accepts project, packages, sources, config_file, force, and timeout.", "network", nugetRestoreParams, r.nugetRestore)
	r.add("nuget_install", "Install a NuGet package into a packages folder. Accepts package (required), version, output_dir, sources, config_file, prerelease, and timeout.", "network", nugetInstallParams, r.nugetInstall)
	r.add("winget_install", "Install a package with Windows Package Manager (winget). Accepts package (required), version, scope, source, and timeout_seconds.", "network", wingetInstallParams, r.wingetInstall)
	r.add("choco_install", "Install a package with Chocolatey. Accepts package (required), version, source, force, and timeout_seconds.", "network", chocoInstallParams, r.chocoInstall)
	r.add("apt_install", "Install a package with apt (Debian/Ubuntu). Accepts package (required), update (bool), and timeout_seconds.", "shell", aptInstallParams, r.aptInstall)
	r.add("apt_get_install", "Install a package with apt-get (Debian/Ubuntu). Accepts package (required), update (bool), and timeout_seconds.", "shell", aptGetInstallParams, r.aptGetInstall)
	r.add("snap_install", "Install a package with snap. Accepts package (required), classic (bool), channel, and timeout_seconds.", "network", snapInstallParams, r.snapInstall)
	r.add("dnf_install", "Install a package with dnf (Fedora/RHEL). Accepts package (required), refresh (bool), and timeout_seconds.", "network", dnfInstallParams, r.dnfInstall)
	r.add("brew_install", "Install a package with Homebrew. Accepts package (required), cask (bool), tap, and timeout_seconds.", "network", brewInstallParams, r.brewInstall)
	r.add("python_run", "Run a Python script or inline code. Accepts script (path), eval (inline code), args, vm_args (e.g. -O), and timeout_seconds.", "shell", pythonRunParams, r.pythonRun)
	r.add("python3_run", "Run a Python 3 script or inline code. Same as python_run but explicitly uses python3.", "shell", pythonRunParams, r.python3Run)
	r.add("pip_install", "Install a Python package with pip. Accepts package (required), version, requirements, index_url, extra_index_url, upgrade, user, and timeout_seconds.", "network", pipInstallParams, r.pipInstall)
	r.add("pip3_install", "Install a Python package with pip3. Same as pip_install but explicitly uses pip3.", "network", pipInstallParams, r.pip3Install)
	r.add("conda_install", "Install a package with conda. Accepts package (required), channel, version, and timeout_seconds.", "network", condaInstallParams, r.condaInstall)
	r.add("conda_create", "Create a conda environment. Accepts name (required), python, packages (list), channel, and timeout_seconds.", "shell", condaCreateParams, r.condaCreate)
	r.add("flutter_run", "Run a Flutter app on a connected device or emulator. Accepts device, route, debug, verbose, and timeout_seconds.", "shell", flutterRunParams, r.flutterRun)
	r.add("flutter_build", "Build a Flutter app. Accepts targets (apk, appbundle, ios, web, etc.), device_id, web_renderer, release_mode, and timeout_seconds.", "shell", flutterBuildParams, r.flutterBuild)
	r.add("flutter_test", "Run Flutter widget and unit tests. Accepts path, tags (list), and timeout_seconds.", "shell", flutterTestParams, r.flutterTest)
	r.add("dart_run", "Run a Dart script. Accepts script (path), args, and timeout_seconds.", "shell", dartRunParams, r.dartRun)
	r.add("dart_analyze", "Analyze Dart code for errors and warnings. Accepts path and fatal_warnings.", "shell", dartAnalyzeParams, r.dartAnalyze)
	r.add("dart_format", "Format Dart code. Accepts path and set_exit_if_changed.", "shell", dartFormatParams, r.dartFormat)
	r.add("pub_get", "Resolve and download Flutter/Dart dependencies. Accepts working_dir, offline, and timeout_seconds.", "network", pubGetParams, r.pubGet)
	r.add("pub_upgrade", "Upgrade Flutter/Dart dependencies. Accepts working_dir, major_only, offline, and timeout_seconds.", "network", pubUpgradeParams, r.pubUpgrade)
	r.add("pub_add", "Add a dependency to a Flutter/Dart project. Accepts name (required), version, dev, working_dir, offline, and timeout_seconds.", "shell", pubAddParams, r.pubAdd)
	r.add("pub_remove", "Remove a dependency from a Flutter/Dart project. Accepts name (required), dev, working_dir, offline, and timeout_seconds.", "shell", pubRemoveParams, r.pubRemove)
	r.add("ar_create", "Create an ar archive. Accepts archive (required) and files (list).", "shell", arCreateParams, r.arCreate)
	r.add("ar_extract", "Extract an ar archive. Accepts archive (required) and output_dir.", "shell", arExtractParams, r.arExtract)
	r.add("ar_list", "List contents of an ar archive. Accepts archive (required).", "read", arListParams, r.arList)
	r.add("tar_create", "Create a tar archive. Accepts archive (required), files (list), compress (gz/bz2/xz), and working_dir.", "shell", tarCreateParams, r.tarCreate)
	r.add("tar_extract", "Extract a tar archive. Accepts archive (required), output_dir, compress, and strip_components.", "shell", tarExtractParams, r.tarExtract)
	r.add("tar_list", "List contents of a tar archive. Accepts archive (required), verbose, compress, and working_dir.", "read", tarListParams, r.tarList)
	r.add("zip_create", "Create a zip archive. Accepts archive (required) and files (list).", "shell", zipCreateParams, r.zipCreate)
	r.add("zip_extract", "Extract a zip archive. Accepts archive (required) and output_dir.", "shell", zipExtractParams, r.zipExtract)
	r.add("zip_list", "List contents of a zip archive. Accepts archive (required).", "read", zipListParams, r.zipList)
	r.add("grep_search", "Search file contents with grep. Accepts pattern (required), path, include, exclude, case_sensitive, fixed_strings, recursive, max_results, and context options.", "read", grepSearchParams, r.grepSearch)
	r.add("find_files", "Find files and directories. Accepts path, name, type, max_depth, modified (age in seconds), size, exec, print0, and timeout_seconds.", "read", findFilesParams, r.findFiles)
	r.add("tail_file", "Read the end of a file. Accepts path (required), lines, bytes, and follow.", "read", tailFileParams, r.tailFile)
	r.add("awk_process", "Process text with awk. Accepts script (required), path, field_separator, vars (map), and timeout_seconds.", "shell", awkProcessParams, r.awkProcess)
	r.add("ps_list", "List running processes. Accepts all, user, output_format, sort, and timeout_seconds.", "read", psListParams, r.psList)
	r.add("chmod_change", "Change file permissions. Accepts path (required), mode (e.g. 755, u+w), and recursive.", "shell", chmodChangeParams, r.chmodChange)
	r.add("chown_change", "Change file owner and group. Accepts path (required), owner (e.g. user or user:group), and recursive.", "destructive", chownChangeParams, r.chownChange)
	r.add("user_add", "Add a new system user. Accepts username (required), home, shell, groups, uid, gid, system, create_home, and no_create_home.", "destructive", userAddParams, r.userAdd)
	r.add("user_del", "Delete a system user. Accepts username (required), remove (delete home), and force.", "destructive", userDelParams, r.userDel)
	r.add("docker_build", "Build a Docker image. Accepts dockerfile, tag, context, no_cache, pull, build_args, target, and timeout_seconds.", "shell", dockerBuildParams, r.dockerBuild)
	r.add("docker_run", "Run a Docker container. Accepts image (required), command, name, ports, volumes, env, detach, rm, network, privileged, and timeout_seconds.", "shell", dockerRunParams, r.dockerRun)
	r.add("docker_compose_up", "Start Docker Compose services. Accepts file, project, services, detach, build, force_recreate, and timeout_seconds.", "shell", dockerComposeUpParams, r.dockerComposeUp)
	r.add("docker_compose_down", "Stop Docker Compose services. Accepts file, project, volumes, and timeout_seconds.", "shell", dockerComposeDownParams, r.dockerComposeDown)
	r.add("qemu_run", "Run a QEMU virtual machine. Accepts image (required), memory, smp, cpu, drive, net, nographic, monitor, serial, args, and timeout_seconds.", "shell", qemuRunParams, r.qemuRun)
	r.add("adb_devices", "List connected Android devices via ADB. Accepts timeout_seconds.", "read", adbDevicesParams, r.adbDevices)
	r.add("adb_shell", "Run a shell command on a connected Android device via ADB. Accepts command (required), serial, and timeout_seconds.", "shell", adbShellParams, r.adbShell)
	r.add("adb_push", "Push a file to a connected Android device via ADB. Accepts local (required), remote (required), serial, and timeout_seconds.", "shell", adbPushParams, r.adbPush)
	r.add("adb_pull", "Pull a file from a connected Android device via ADB. Accepts remote (required), local (required), serial, and timeout_seconds.", "shell", adbPullParams, r.adbPull)
	return r
}

var defaultParamSchema = json.RawMessage(`{"type":"object"}`)

var (
	listFilesParams         = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"max_results":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	readFileParams          = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"}},"required":["path"],"additionalProperties":false}`)
	searchTextParams        = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"path":{"type":"string"},"case_sensitive":{"type":"boolean"}},"required":["query"],"additionalProperties":false}`)
	writeFileParams         = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)
	writePatchParams        = json.RawMessage(`{"type":"object","properties":{"patch":{"type":"string"}},"required":["patch"],"additionalProperties":false}`)
	runCommandParams        = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"argv":{"type":"array","items":{"type":"string"}},"shell":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	runScriptParams         = json.RawMessage(`{"type":"object","properties":{"description":{"type":"string"}},"required":["description"],"additionalProperties":false}`)
	gitStatusParams         = json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	gitDiffParams           = json.RawMessage(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`)
	gitLogParams            = json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"},"oneline":{"type":"boolean"}},"required":[],"additionalProperties":false}`)
	cmakeConfigureParams    = json.RawMessage(`{"type":"object","properties":{"build_dir":{"type":"string"},"source_dir":{"type":"string"},"generator":{"type":"string"},"defs":{"type":"object"}},"required":[],"additionalProperties":false}`)
	cmakeBuildParams        = json.RawMessage(`{"type":"object","properties":{"build_dir":{"type":"string"},"target":{"type":"string"},"config":{"type":"string"}},"required":[],"additionalProperties":false}`)
	clangFormatParams       = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"in_place":{"type":"boolean"},"style":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	clangTidyParams         = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"checks":{"type":"string"},"fix":{"type":"boolean"}},"required":["path"],"additionalProperties":false}`)
	makeBuildParams         = json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"},"build_dir":{"type":"string"},"jobs":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	nmakeBuildParams        = json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"},"build_dir":{"type":"string"},"macro":{"type":"object"}},"required":[],"additionalProperties":false}`)
	curlParams              = json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"method":{"type":"string"},"headers":{"type":"object"},"data":{"type":"string"},"output":{"type":"string"},"location":{"type":"boolean"},"max_time":{"type":"integer"}},"required":["url"],"additionalProperties":false}`)
	wgetParams              = json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"},"output":{"type":"string"},"recursive":{"type":"boolean"},"page_requisites":{"type":"boolean"},"max_depth":{"type":"integer"},"reject":{"type":"string"},"exclude_directories":{"type":"string"},"continue":{"type":"boolean"}},"required":["url"],"additionalProperties":false}`)
	javaRunParams           = json.RawMessage(`{"type":"object","properties":{"main_class":{"type":"string"},"jar":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"vm_args":{"type":"array","items":{"type":"string"}},"classpath":{"type":"string"}},"required":[],"additionalProperties":false}`)
	javacCompileParams      = json.RawMessage(`{"type":"object","properties":{"source":{"type":"string"},"classpath":{"type":"string"},"output_dir":{"type":"string"},"sourcepath":{"type":"string"},"release":{"type":"string"}},"required":["source"],"additionalProperties":false}`)
	rgSearchParams          = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},"file_type":{"type":"string"},"case_sensitive":{"type":"boolean"},"fixed_strings":{"type":"boolean"},"max_results":{"type":"integer"}},"required":["pattern"],"additionalProperties":false}`)
	sedEditParams           = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"expression":{"type":"string"},"in_place":{"type":"boolean"}},"required":["path","expression"],"additionalProperties":false}`)
	base64EncodeParams      = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"},"path":{"type":"string"},"decode":{"type":"boolean"},"wrap":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	projectIndexParams      = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"symbol":{"type":"string"},"kind":{"type":"string"},"lang":{"type":"string"},"max_results":{"type":"integer"},"summary":{"type":"boolean"}},"required":[],"additionalProperties":false}`)
	nodeRunParams           = json.RawMessage(`{"type":"object","properties":{"script":{"type":"string"},"eval":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"vm_args":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	npmCommandParams        = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"script":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	npxCommandParams        = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	nvmUseParams            = json.RawMessage(`{"type":"object","properties":{"version":{"type":"string"}},"required":["version"],"additionalProperties":false}`)
	dotnetRunParams         = json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"configuration":{"type":"string"},"framework":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dotnetBuildParams       = json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"},"configuration":{"type":"string"},"framework":{"type":"string"},"output":{"type":"string"},"no_restore":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dotnetTestParams        = json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"},"filter":{"type":"string"},"configuration":{"type":"string"},"framework":{"type":"string"},"no_build":{"type":"boolean"},"logger":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	msbuildParams           = json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"},"target":{"type":"string"},"configuration":{"type":"string"},"property":{"type":"object"},"max_cpu_count":{"type":"integer"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	nugetRestoreParams      = json.RawMessage(`{"type":"object","properties":{"project":{"type":"string"},"packages":{"type":"string"},"source":{"type":"array","items":{"type":"string"}},"config_file":{"type":"string"},"force":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	nugetInstallParams      = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"version":{"type":"string"},"output_dir":{"type":"string"},"source":{"type":"array","items":{"type":"string"}},"config_file":{"type":"string"},"prerelease":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	wingetInstallParams     = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"version":{"type":"string"},"scope":{"type":"string"},"source":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	chocoInstallParams      = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"version":{"type":"string"},"source":{"type":"string"},"force":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	aptInstallParams        = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"update":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	aptGetInstallParams     = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"update":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	snapInstallParams       = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"classic":{"type":"boolean"},"channel":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	dnfInstallParams        = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"refresh":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	brewInstallParams       = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"cask":{"type":"boolean"},"tap":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	pythonRunParams         = json.RawMessage(`{"type":"object","properties":{"script":{"type":"string"},"eval":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"vm_args":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	pipInstallParams        = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"version":{"type":"string"},"requirements":{"type":"string"},"index_url":{"type":"string"},"extra_index_url":{"type":"string"},"upgrade":{"type":"boolean"},"user":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	condaInstallParams      = json.RawMessage(`{"type":"object","properties":{"package":{"type":"string"},"channel":{"type":"string"},"version":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["package"],"additionalProperties":false}`)
	condaCreateParams       = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"python":{"type":"string"},"packages":{"type":"array","items":{"type":"string"}},"channel":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["name"],"additionalProperties":false}`)
	flutterRunParams        = json.RawMessage(`{"type":"object","properties":{"device":{"type":"string"},"route":{"type":"string"},"debug":{"type":"boolean"},"verbose":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	flutterBuildParams      = json.RawMessage(`{"type":"object","properties":{"targets":{"type":"string"},"device_id":{"type":"string"},"web_renderer":{"type":"string"},"release_mode":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	flutterTestParams       = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dartRunParams           = json.RawMessage(`{"type":"object","properties":{"script":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dartAnalyzeParams       = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"fatal_warnings":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dartFormatParams        = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"set_exit_if_changed":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	pubGetParams            = json.RawMessage(`{"type":"object","properties":{"working_dir":{"type":"string"},"offline":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	pubUpgradeParams        = json.RawMessage(`{"type":"object","properties":{"working_dir":{"type":"string"},"major_only":{"type":"boolean"},"offline":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	pubAddParams            = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"version":{"type":"string"},"dev":{"type":"boolean"},"working_dir":{"type":"string"},"offline":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["name"],"additionalProperties":false}`)
	pubRemoveParams         = json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"dev":{"type":"boolean"},"working_dir":{"type":"string"},"offline":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["name"],"additionalProperties":false}`)
	arCreateParams          = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	arExtractParams         = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"output_dir":{"type":"string"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	arListParams            = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	tarCreateParams         = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"compress":{"type":"string"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	tarExtractParams        = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"output_dir":{"type":"string"},"compress":{"type":"string"},"strip_components":{"type":"integer"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	tarListParams           = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"verbose":{"type":"boolean"},"compress":{"type":"string"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	zipCreateParams         = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"files":{"type":"array","items":{"type":"string"}},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	zipExtractParams        = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"output_dir":{"type":"string"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	zipListParams           = json.RawMessage(`{"type":"object","properties":{"archive":{"type":"string"},"working_dir":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["archive"],"additionalProperties":false}`)
	grepSearchParams        = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"include":{"type":"string"},"exclude":{"type":"string"},"case_sensitive":{"type":"boolean"},"fixed_strings":{"type":"boolean"},"recursive":{"type":"boolean"},"max_results":{"type":"integer"},"after_context":{"type":"integer"},"before_context":{"type":"integer"},"context":{"type":"integer"}},"required":["pattern"],"additionalProperties":false}`)
	findFilesParams         = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"name":{"type":"string"},"type":{"type":"string"},"max_depth":{"type":"integer"},"modified":{"type":"integer"},"size":{"type":"string"},"exec":{"type":"string"},"print0":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	tailFileParams          = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"lines":{"type":"integer"},"bytes":{"type":"integer"},"follow":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["path"],"additionalProperties":false}`)
	awkProcessParams        = json.RawMessage(`{"type":"object","properties":{"script":{"type":"string"},"path":{"type":"string"},"field_separator":{"type":"string"},"vars":{"type":"object"},"timeout_seconds":{"type":"integer"}},"required":["script"],"additionalProperties":false}`)
	psListParams            = json.RawMessage(`{"type":"object","properties":{"all":{"type":"boolean"},"user":{"type":"string"},"output_format":{"type":"string"},"sort":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	chmodChangeParams       = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"mode":{"type":"string"},"recursive":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["path","mode"],"additionalProperties":false}`)
	chownChangeParams       = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"owner":{"type":"string"},"group":{"type":"string"},"recursive":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["path","owner"],"additionalProperties":false}`)
	userAddParams           = json.RawMessage(`{"type":"object","properties":{"username":{"type":"string"},"home":{"type":"string"},"shell":{"type":"string"},"groups":{"type":"array","items":{"type":"string"}},"system":{"type":"boolean"},"create_home":{"type":"boolean"},"no_create_home":{"type":"boolean"},"uid":{"type":"integer"},"gid":{"type":"integer"},"password":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["username"],"additionalProperties":false}`)
	userDelParams           = json.RawMessage(`{"type":"object","properties":{"username":{"type":"string"},"remove":{"type":"boolean"},"force":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["username"],"additionalProperties":false}`)
	runTestsParams          = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"file":{"type":"string"},"framework":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	runFormatterParams      = json.RawMessage(`{"type":"object","properties":{"tool":{"type":"string"},"path":{"type":"string"}},"required":["tool"],"additionalProperties":false}`)
	reviewChangesParams     = json.RawMessage(`{"type":"object","properties":{"scope":{"type":"string"}},"required":[],"additionalProperties":false}`)
	lspDiagnosticsParams    = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
	lspDefinitionParams     = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"character":{"type":"integer"}},"required":["path","line","character"],"additionalProperties":false}`)
	lspFindReferencesParams = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"},"character":{"type":"integer"}},"required":["path","line","character"],"additionalProperties":false}`)
	dockerBuildParams       = json.RawMessage(`{"type":"object","properties":{"dockerfile":{"type":"string"},"tag":{"type":"string"},"context":{"type":"string"},"no_cache":{"type":"boolean"},"pull":{"type":"boolean"},"build_args":{"type":"object"},"target":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dockerRunParams         = json.RawMessage(`{"type":"object","properties":{"image":{"type":"string"},"command":{"type":"array","items":{"type":"string"}},"name":{"type":"string"},"ports":{"type":"array","items":{"type":"string"}},"volumes":{"type":"array","items":{"type":"string"}},"env":{"type":"object"},"detach":{"type":"boolean"},"rm":{"type":"boolean"},"network":{"type":"string"},"privileged":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":["image"],"additionalProperties":false}`)
	dockerComposeUpParams   = json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"project":{"type":"string"},"services":{"type":"array","items":{"type":"string"}},"detach":{"type":"boolean"},"build":{"type":"boolean"},"force_recreate":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	dockerComposeDownParams = json.RawMessage(`{"type":"object","properties":{"file":{"type":"string"},"project":{"type":"string"},"volumes":{"type":"boolean"},"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	qemuRunParams           = json.RawMessage(`{"type":"object","properties":{"image":{"type":"string"},"memory":{"type":"string"},"smp":{"type":"integer"},"cpu":{"type":"string"},"drive":{"type":"string"},"net":{"type":"string"},"nographic":{"type":"boolean"},"monitor":{"type":"string"},"serial":{"type":"string"},"args":{"type":"array","items":{"type":"string"}},"timeout_seconds":{"type":"integer"}},"required":["image"],"additionalProperties":false}`)
	adbDevicesParams        = json.RawMessage(`{"type":"object","properties":{"timeout_seconds":{"type":"integer"}},"required":[],"additionalProperties":false}`)
	adbShellParams          = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"serial":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["command"],"additionalProperties":false}`)
	adbPushParams           = json.RawMessage(`{"type":"object","properties":{"local":{"type":"string"},"remote":{"type":"string"},"serial":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["local","remote"],"additionalProperties":false}`)
	adbPullParams           = json.RawMessage(`{"type":"object","properties":{"remote":{"type":"string"},"local":{"type":"string"},"serial":{"type":"string"},"timeout_seconds":{"type":"integer"}},"required":["remote","local"],"additionalProperties":false}`)
)

func (r *Registry) add(name, desc, effect string, parameters json.RawMessage, fn func(context.Context, json.RawMessage) (Result, error)) {
	if parameters == nil {
		parameters = defaultParamSchema
	}
	r.tools[name] = Tool{
		Name: name, Description: desc, Effect: effect,
		Parameters: parameters,
		Execute:    fn,
	}
}

func (r *Registry) ToolSchemas() []model.ToolSchema {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]model.ToolSchema, 0, len(names))
	for _, name := range names {
		t := r.tools[name]
		out = append(out, model.ToolSchema{
			Type: "function",
			Function: model.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) DiffPreview(name string, raw json.RawMessage) (string, error) {
	switch name {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
		path, err := r.safePath(args.Path)
		if err != nil {
			return "", err
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			existing = []byte{}
		}
		return generateDiff(args.Path, string(existing), args.Content), nil
	case "write_patch":
		var args struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
		if err := validatePatchPaths(args.Patch); err != nil {
			return "", err
		}
		return args.Patch, nil
	default:
		return "", nil
	}
}

type diffEdit struct {
	op   byte
	line string
}

func generateDiff(filename, old, new string) string {
	oldLines := splitLines(old)
	newLines := splitLines(new)

	var b strings.Builder
	b.WriteString("--- a/" + filename + "\n")
	b.WriteString("+++ b/" + filename + "\n")

	if len(oldLines) == 0 && len(newLines) == 0 {
		return b.String()
	}
	if len(oldLines) == 0 {
		b.WriteString("@@ -0,0 +1," + fmt.Sprint(len(newLines)) + " @@\n")
		for _, line := range newLines {
			b.WriteString("+" + line + "\n")
		}
		return b.String()
	}
	if len(newLines) == 0 {
		b.WriteString("@@ -1," + fmt.Sprint(len(oldLines)) + " +0,0 @@\n")
		for _, line := range oldLines {
			b.WriteString("-" + line + "\n")
		}
		return b.String()
	}

	lcs := lcsTable(oldLines, newLines)
	edits := backtrack(oldLines, newLines, lcs)

	// Walk the edit list and emit hunks with 1-line context
	oldPos, newPos := 1, 1
	i := 0
	for i < len(edits) {
		// Skip unchanged lines at start (context before first hunk)
		for i < len(edits) && edits[i].op == ' ' {
			i++
			oldPos++
			newPos++
		}
		if i >= len(edits) {
			break
		}

		// Find the end of this hunk
		hunkStart := i
		ctxBefore := 0
		for hunkStart > 0 && edits[hunkStart-1].op == ' ' && ctxBefore < 1 {
			hunkStart--
			ctxBefore++
		}
		hunkEnd := i
		for hunkEnd < len(edits) && edits[hunkEnd].op != ' ' {
			hunkEnd++
		}
		ctxAfter := 0
		tempEnd := hunkEnd
		for tempEnd < len(edits) && edits[tempEnd].op == ' ' && ctxAfter < 1 {
			tempEnd++
			ctxAfter++
		}

		// Compute hunk position
		hunkOld := oldPos - ctxBefore
		hunkNew := newPos - ctxBefore
		hunkOldLen := 0
		hunkNewLen := 0
		for j := hunkStart; j < tempEnd; j++ {
			switch edits[j].op {
			case '-':
				hunkOldLen++
			case '+':
				hunkNewLen++
			default:
				hunkOldLen++
				hunkNewLen++
			}
		}
		if hunkOldLen == 0 {
			hunkOldLen = 1
		}
		if hunkNewLen == 0 {
			hunkNewLen = 1
		}

		b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hunkOld, hunkOldLen, hunkNew, hunkNewLen))
		for j := hunkStart; j < tempEnd; j++ {
			b.WriteByte(edits[j].op)
			b.WriteString(edits[j].line)
			b.WriteByte('\n')
		}

		// Advance position counters past the hunk and its trailing context
		for j := i; j < tempEnd; j++ {
			switch edits[j].op {
			case '-':
				oldPos++
			case '+':
				newPos++
			default:
				oldPos++
				newPos++
			}
		}
		i = tempEnd
	}

	return b.String()
}

func lcsTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

func backtrack(a, b []string, lcs [][]int) []diffEdit {
	var stack []diffEdit
	i, j := len(a), len(b)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			stack = append(stack, diffEdit{' ', a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			stack = append(stack, diffEdit{'+', b[j-1]})
			j--
		} else if i > 0 {
			stack = append(stack, diffEdit{'-', a[i-1]})
			i--
		}
	}
	edits := make([]diffEdit, len(stack))
	for k := range stack {
		edits[k] = stack[len(stack)-1-k]
	}
	return edits
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (r *Registry) Prompt() string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("Available tools:\n")
	for _, name := range names {
		t := r.tools[name]
		b.WriteString("- ")
		b.WriteString(t.Name)
		b.WriteString(": ")
		b.WriteString(t.Description)
		b.WriteString(" (effect: ")
		b.WriteString(t.Effect)
		b.WriteString(")\n")
	}
	return b.String()
}

func (r *Registry) safePath(path string) (string, error) {
	if path == "" {
		path = "."
	}
	clean := filepath.Clean(path)
	full := filepath.Join(r.root, clean)
	rel, err := filepath.Rel(r.root, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(path) {
		return "", fmt.Errorf("path escapes project root: %s", path)
	}
	return full, nil
}

func (r *Registry) listFiles(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		fmt.Fprintf(os.Stderr, "[qodex] list_files: invalid arguments, using defaults: %v\n", err)
	}
	if args.MaxResults <= 0 {
		args.MaxResults = 200
	}
	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= args.MaxResults {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(r.root, path)
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Listed %d files.", len(files)), Content: strings.Join(files, "\n")}, nil
}

func (r *Registry) readFile(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	if args.StartLine > 0 || args.EndLine > 0 {
		lines := strings.Split(content, "\n")
		start := max(args.StartLine, 1)
		end := args.EndLine
		if end <= 0 || end > len(lines) {
			end = len(lines)
		}
		if start > end {
			content = ""
		} else {
			content = strings.Join(lines[start-1:end], "\n")
		}
	}
	return Result{OK: true, Summary: fmt.Sprintf("Read %s.", args.Path), Content: truncate(content, 20000)}, nil
}

func (r *Registry) searchText(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Query == "" {
		return Result{}, fmt.Errorf("query is required")
	}
	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	var matches []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(matches) >= 200 {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(args.Query)) {
				rel, _ := filepath.Rel(r.root, path)
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, i+1, strings.TrimSpace(line)))
				if len(matches) >= 200 {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Found %d matches.", len(matches)), Content: strings.Join(matches, "\n")}, nil
}

func (r *Registry) writeFile(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o666); err != nil {
		return Result{}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Wrote %s.", args.Path)}, nil
}

func (r *Registry) writePatch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(args.Patch) == "" {
		return Result{}, fmt.Errorf("patch is required")
	}
	if err := validatePatchPaths(args.Patch); err != nil {
		return Result{}, err
	}

	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "apply", "--whitespace=nowarn", "-")
	cmd.Dir = r.root
	cmd.Stdin = strings.NewReader(args.Patch)
	out, err := runWithKillStdin(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Applied unified diff.",
		Content: truncate(string(out), 12000),
	}
	if err != nil {
		res.Summary = "Failed to apply unified diff."
		return res, err
	}
	return res, nil
}

func (r *Registry) runCommand(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command        string   `json:"command"`
		Argv           []string `json:"argv"`
		Shell          bool     `json:"shell"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if len(args.Argv) == 0 && args.Command == "" {
		return Result{}, fmt.Errorf("command or argv is required")
	}
	if args.TimeoutSeconds <= 0 || args.TimeoutSeconds > 300 {
		args.TimeoutSeconds = 120
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(args.TimeoutSeconds)*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	summary := ""
	if len(args.Argv) > 0 {
		if args.Argv[0] == "" {
			return Result{}, fmt.Errorf("argv[0] is required")
		}
		if err := rejectDangerousArgv(args.Argv); err != nil {
			return Result{}, err
		}
		cmd = exec.CommandContext(cctx, args.Argv[0], args.Argv[1:]...)
		summary = "Ran command: " + strings.Join(args.Argv, " ")
	} else {
		if err := rejectDangerousShellCommand(args.Command); err != nil {
			return Result{}, err
		}
		shell, shellArgs := ShellCommand(args.Command)
		cmd = exec.CommandContext(cctx, shell, shellArgs...)
		args.Shell = true
		summary = "Ran shell command: " + args.Command
	}
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: summary,
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"shell":   args.Shell,
			"network": isNetworkCommand(args.Command, args.Argv),
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) runScript(ctx context.Context, raw json.RawMessage) (Result, error) {
	return Result{OK: false, Summary: "run_script is handled by the agent"}, fmt.Errorf("run_script must be dispatched by the agent")
}

func (r *Registry) gitStatus(ctx context.Context, raw json.RawMessage) (Result, error) {
	return r.git(ctx, "status --short")
}

func (r *Registry) gitDiff(ctx context.Context, raw json.RawMessage) (Result, error) {
	return r.git(ctx, "diff")
}

func (r *Registry) gitLog(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Limit   int  `json:"limit"`
		Oneline bool `json:"oneline"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Limit <= 0 {
		args.Limit = 20
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	parts := []string{"log"}
	if args.Oneline {
		parts = append(parts, "--oneline")
	}
	parts = append(parts, fmt.Sprintf("-%d", args.Limit))
	cmd := exec.CommandContext(cctx, "git", parts...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{OK: err == nil, Summary: "Ran git log", Content: truncate(string(out), 20000)}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) git(ctx context.Context, command string) (Result, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	parts := strings.Fields(command)
	cmd := exec.CommandContext(cctx, "git", parts...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{OK: err == nil, Summary: "Ran git " + command, Content: truncate(string(out), 20000)}
	if err != nil {
		return res, err
	}
	return res, nil
}

func rejectDangerousShellCommand(command string) error {
	normalized := strings.Join(strings.Fields(strings.ToLower(command)), " ")
	dangerous := []string{
		"rm -rf /",
		"rm -fr /",
		"rm -rf ~",
		"rm -fr ~",
		"rm -rf $home",
		"rm -fr $home",
		"rm -rf %userprofile%",
		"rm -fr %userprofile%",
		"sudo rm -rf",
		"sudo rm -fr",
		"mkfs",
		"diskutil erase",
		":(){ :|:& };:",
		"curl |",
		"wget |",
		"eval ",
		"remove-item -recurse -force c:\\",
		"remove-item -rec -force c:\\",
	}
	for _, pattern := range dangerous {
		if strings.Contains(normalized, pattern) {
			return fmt.Errorf("refusing dangerous shell command: %s", pattern)
		}
	}
	return nil
}

func isRootPath(p string) bool {
	if p == "/" {
		return true
	}
	if filepath.IsAbs(p) {
		cleaned := filepath.Clean(p)
		if cleaned == string(filepath.Separator) {
			return true
		}
		if vol := filepath.VolumeName(cleaned); vol != "" {
			return cleaned == vol+string(filepath.Separator)
		}
	}
	return false
}

func rejectDangerousArgv(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	if base == "rm" {
		hasNoPreserve := false
		for _, arg := range argv[1:] {
			if isRootPath(arg) {
				return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
			}
			if strings.HasPrefix(arg, "/") && (strings.Contains(arg, "*") || strings.Contains(arg, "..")) {
				return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
			}
			if filepath.IsAbs(arg) && (strings.Contains(arg, "*") || strings.Contains(arg, "..")) {
				return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
			}
			if arg == "--no-preserve-root" {
				hasNoPreserve = true
			}
			if strings.Contains(arg, "rf") && strings.HasPrefix(arg, "-") {
				for _, target := range argv[1:] {
					if isRootPath(target) {
						return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
					}
				}
			}
		}
		if hasNoPreserve {
			for _, arg := range argv[1:] {
				if isRootPath(arg) {
					return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
				}
			}
		}
	}
	if base == "mkfs" || strings.HasPrefix(base, "mkfs.") {
		return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
	}
	if base == "diskutil" && len(argv) > 1 && strings.EqualFold(argv[1], "erase") {
		return fmt.Errorf("refusing dangerous command: %s", strings.Join(argv, " "))
	}
	return nil
}

func IsNetworkCommand(raw json.RawMessage) bool {
	var args struct {
		Command string   `json:"command"`
		Argv    []string `json:"argv"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return false
	}
	return isNetworkCommand(args.Command, args.Argv)
}

func IsNpmCommandNetwork(raw json.RawMessage) bool {
	var args struct {
		Command string   `json:"command"`
		Script  string   `json:"script"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return false
	}
	if args.Script != "" {
		return true
	}
	if args.Command == "" {
		return false
	}
	normalized := strings.ToLower(args.Command)
	networkCommands := []string{
		"install", "i", "ci", "publish", "access", "adduser", "whoami", "logout", "ping",
	}
	for _, cmd := range networkCommands {
		if normalized == cmd {
			return true
		}
	}
	return false
}

func IsNpxCommandNetwork(raw json.RawMessage) bool {
	var args struct {
		Package string `json:"package"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return false
	}
	return args.Package != ""
}

func isNetworkCommand(command string, argv []string) bool {
	if len(argv) > 0 {
		return isNetworkArgv(argv)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(command)), " ")
	networkPatterns := []string{
		"curl ", "wget ", "git clone", "git pull", "git fetch", "go get", "go install",
		"npm install", "npm i", "pnpm install", "yarn install", "pip install",
		"cargo install", "brew install", "apt install", "apt-get install",
		"dnf install", "yum install", "apk add", "ssh ", "scp ", "rsync ",
	}
	for _, pattern := range networkPatterns {
		if strings.Contains(normalized, pattern) || strings.HasPrefix(normalized, strings.TrimSpace(pattern)) {
			return true
		}
	}
	return false
}

func isNetworkArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	switch base {
	case "curl", "wget", "ssh", "scp", "rsync":
		return true
	case "git":
		return len(argv) > 1 && (argv[1] == "clone" || argv[1] == "pull" || argv[1] == "fetch")
	case "go":
		return len(argv) > 1 && (argv[1] == "get" || argv[1] == "install")
	case "npm":
		return len(argv) > 1 && (argv[1] == "install" || argv[1] == "i")
	case "pnpm", "yarn", "pip", "cargo", "brew":
		return len(argv) > 1 && argv[1] == "install"
	case "apt", "apt-get", "dnf", "yum":
		return len(argv) > 1 && argv[1] == "install"
	case "apk":
		return len(argv) > 1 && argv[1] == "add"
	default:
		return false
	}
}

func runWithKill(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- result{out: out, err: err}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return nil, ctx.Err()
	}
}

func runWithKillStdin(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		done <- result{out: out, err: err}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return nil, ctx.Err()
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for !utf8.ValidString(s[:cut]) && cut > 0 {
		cut--
	}
	return s[:cut] + "\n... truncated ..."
}

func (r *Registry) runTests(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Pattern   string `json:"pattern"`
		File      string `json:"file"`
		Framework string `json:"framework"`
		Timeout   int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Pattern == "" && args.File == "" {
		args.Pattern = "./..."
	}
	if args.Timeout <= 0 || args.Timeout > 600 {
		args.Timeout = 300
	}

	if args.Framework == "" {
		if args.File != "" {
			args.Framework = detectFramework(filepath.Ext(args.File))
		} else {
			args.Framework = detectProjectFramework(r.root)
		}
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(args.Timeout)*time.Second)
	defer cancel()

	summary := ""
	var content string

	switch args.Framework {
	case "go":
		argv := []string{"go", "test"}
		if args.Pattern != "" {
			argv = append(argv, args.Pattern)
		}
		if args.File != "" {
			argv = append(argv, args.File)
		}
		cmd := exec.CommandContext(cctx, "go", argv[1:]...)
		cmd.Dir = r.root
		out, err := runWithKill(cctx, cmd)
		summary = fmt.Sprintf("Ran go test %s", args.Pattern)
		content = string(out)
		if err != nil {
			return Result{OK: false, Summary: "Tests failed", Content: truncate(content, 20000)}, err
		}
		return Result{OK: true, Summary: summary, Content: truncate(content, 20000), Metadata: map[string]interface{}{"shell": false, "network": false}}, nil

	case "pytest", "python":
		argv := []string{"python", "-m", "pytest"}
		if args.Pattern != "" {
			argv = append(argv, args.Pattern)
		}
		if args.File != "" {
			argv = append(argv, args.File)
		}
		cmd := exec.CommandContext(cctx, "python", argv[1:]...)
		cmd.Dir = r.root
		out, err := runWithKill(cctx, cmd)
		summary = fmt.Sprintf("Ran pytest %s", args.Pattern)
		content = string(out)
		if err != nil {
			return Result{OK: false, Summary: "Tests failed", Content: truncate(content, 20000)}, err
		}
		return Result{OK: true, Summary: summary, Content: truncate(content, 20000), Metadata: map[string]interface{}{"shell": false, "network": false}}, nil

	case "jest", "node":
		if hasFile(r.root, "package.json") {
			argv := []string{"npx", "jest"}
			if args.Pattern != "" {
				argv = append(argv, args.Pattern)
			}
			if args.File != "" {
				argv = append(argv, args.File)
			}
			cmd := exec.CommandContext(cctx, "npx", argv...)
			cmd.Dir = r.root
			out, err := runWithKill(cctx, cmd)
			summary = fmt.Sprintf("Ran jest %s", args.Pattern)
			content = string(out)
			if err != nil {
				return Result{OK: false, Summary: "Tests failed", Content: truncate(content, 20000)}, err
			}
			return Result{OK: true, Summary: summary, Content: truncate(content, 20000), Metadata: map[string]interface{}{"shell": false, "network": false}}, nil
		}

	default:
		if args.Framework == "" {
			return Result{}, fmt.Errorf("no supported test runner detected; try go, pytest, or jest")
		}
		return Result{}, fmt.Errorf("unsupported test framework %q; try go, pytest, or jest", args.Framework)
	}
	return Result{}, fmt.Errorf("unreachable: all framework cases return")
}

func hasFile(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

func detectFramework(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "pytest"
	case ".js", ".jsx", ".ts", ".tsx":
		return "jest"
	default:
		return ""
	}
}

func detectProjectFramework(root string) string {
	if hasFile(root, "go.mod") {
		return "go"
	}
	if hasFile(root, "pyproject.toml") || hasFile(root, "setup.py") || hasFile(root, "requirements.txt") {
		return "pytest"
	}
	if hasFile(root, "package.json") || hasFile(root, "jest.config.js") || hasFile(root, "jest.config.ts") {
		return "jest"
	}
	return ""
}

func (r *Registry) runFormatter(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Tool string `json:"tool"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Tool == "" {
		return Result{}, fmt.Errorf("tool is required (e.g. go, ruff, prettier, black)")
	}

	workPath := r.root
	if args.Path != "" {
		var err error
		workPath, err = r.safePath(args.Path)
		if err != nil {
			return Result{}, err
		}
	}

	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	summary := ""
	switch strings.ToLower(args.Tool) {
	case "go", "gofmt":
		argv := []string{"go", "fmt"}
		if args.Path != "" {
			dir := filepath.Dir(args.Path)
			if dir != "." {
				argv = append(argv, "./"+dir)
			} else {
				argv = append(argv, "./"+filepath.Base(args.Path))
			}
		} else {
			argv = append(argv, "./...")
		}
		// go fmt writes to a relative path; we must run it from the root
		cmd = exec.CommandContext(cctx, "go", "fmt", argv[2])
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran go fmt on %s", args.Path)
		if args.Path == "" {
			summary = "Ran go fmt on project"
		}

	case "ruff":
		argv := []string{"ruff", "format"}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, "ruff", argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran ruff format on %s", workPath)

	case "black":
		argv := []string{"black"}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, "black", argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran black on %s", workPath)

	case "prettier":
		argv := []string{"npx", "prettier", "--write"}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, "npx", argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran prettier on %s", workPath)

	default:
		// Try running the tool directly with the path as argument
		argv := []string{args.Tool}
		if args.Path != "" {
			argv = append(argv, workPath)
		} else {
			argv = append(argv, ".")
		}
		cmd = exec.CommandContext(cctx, args.Tool, argv[1:]...)
		cmd.Dir = r.root
		summary = fmt.Sprintf("Ran %s on %s", args.Tool, workPath)
	}

	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: summary,
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"shell":   false,
			"network": false,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) reviewChanges(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		fmt.Fprintf(os.Stderr, "[qodex] review_changes: invalid arguments, using defaults: %v\n", err)
	}
	if args.Scope == "" {
		args.Scope = "all"
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var b strings.Builder
	b.WriteString("# Review of Uncommitted Changes\n\n")

	// Get git diff
	diffArgs := []string{"diff"}
	if args.Scope == "staged" {
		diffArgs = []string{"diff", "--staged"}
	} else if args.Scope == "working" {
		diffArgs = []string{"diff"}
	} else {
		// all: staged + working
		diffArgs = []string{"diff", "HEAD"} // includes both staged and unstaged
	}

	cmd := exec.CommandContext(cctx, "git", diffArgs...)
	cmd.Dir = r.root
	diffOut, err := runWithKill(cctx, cmd)
	if err != nil {
		return Result{}, fmt.Errorf("git diff failed: %w", err)
	}
	diffText := string(diffOut)
	if strings.TrimSpace(diffText) == "" {
		return Result{OK: true, Summary: "No changes to review", Content: "No uncommitted changes found."}, nil
	}

	cmd2 := exec.CommandContext(cctx, "git", "diff", "--name-status")
	if args.Scope == "staged" {
		cmd2 = exec.CommandContext(cctx, "git", "diff", "--staged", "--name-status")
	} else if args.Scope == "working" {
		cmd2 = exec.CommandContext(cctx, "git", "diff", "--name-status")
	} else {
		cmd2 = exec.CommandContext(cctx, "git", "diff", "HEAD", "--name-status")
	}
	cmd2.Dir = r.root
	statusOut, _ := runWithKill(cctx, cmd2)

	cmd3 := exec.CommandContext(cctx, "git", "status", "--short")
	cmd3.Dir = r.root
	statusShort, _ := runWithKill(cctx, cmd3)

	b.WriteString("## Changed Files\n\n")
	b.WriteString("```\n")
	b.WriteString(string(statusOut))
	b.WriteString("```\n\n")

	b.WriteString("## Untracked Files\n\n")
	untracked := extractUntracked(string(statusShort))
	if len(untracked) > 0 {
		for _, u := range untracked {
			b.WriteString(fmt.Sprintf("- %s (untracked)\n", u))
		}
	} else {
		b.WriteString("No untracked files.\n")
	}
	b.WriteString("\n")
	b.WriteString("## Diff Summary\n\n")
	b.WriteString(fmt.Sprintf("Total diff size: %d bytes across changed files.\n", len(diffText)))
	b.WriteString("\n## Full Diff\n\n```diff\n")
	b.WriteString(diffText)
	b.WriteString("```")

	content := b.String()
	summary := fmt.Sprintf("Reviewing %d bytes of changes", len(diffText))
	if len(content) > 25000 {
		content = content[:25000] + "\n... truncated ..."
	}

	return Result{
		OK:      true,
		Summary: summary,
		Content: truncate(content, 50000),
		Metadata: map[string]interface{}{
			"diff_size": len(diffText),
			"scope":     args.Scope,
		},
	}, nil
}

func extractUntracked(gitStatus string) []string {
	var out []string
	for _, line := range strings.Split(gitStatus, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "?? ") {
			out = append(out, strings.TrimPrefix(line, "?? "))
		}
	}
	return out
}

func validatePatchPaths(patch string) error {
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[1] == "/dev/null" {
				continue
			}
			path := strings.TrimPrefix(fields[1], "a/")
			path = strings.TrimPrefix(path, "b/")
			if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(filepath.Clean(path), "../") {
				return fmt.Errorf("patch path escapes project root: %s", fields[1])
			}
		}
	}
	return nil
}

func (r *Registry) cmakeConfigure(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		BuildDir  string            `json:"build_dir"`
		SourceDir string            `json:"source_dir"`
		Generator string            `json:"generator"`
		Defs      map[string]string `json:"defs"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.BuildDir == "" {
		args.BuildDir = "build"
	}
	if args.SourceDir == "" {
		args.SourceDir = "."
	}
	buildPath, err := r.safePath(args.BuildDir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(buildPath, 0o755); err != nil {
		return Result{}, err
	}
	argv := []string{"-S", args.SourceDir, "-B", buildPath}
	if args.Generator != "" {
		argv = append([]string{"-G", args.Generator}, argv...)
	}
	for k, v := range args.Defs {
		argv = append(argv, fmt.Sprintf("-D%s=%s", k, v))
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "cmake", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran cmake configure in %s", buildPath),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) cmakeBuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		BuildDir string `json:"build_dir"`
		Target   string `json:"target"`
		Config   string `json:"config"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.BuildDir == "" {
		args.BuildDir = "build"
	}
	buildPath, err := r.safePath(args.BuildDir)
	if err != nil {
		return Result{}, err
	}
	argv := []string{"--build", buildPath}
	if args.Target != "" {
		argv = append(argv, "--target", args.Target)
	}
	if args.Config != "" {
		argv = append(argv, "--config", args.Config)
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "cmake", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran cmake --build %s", buildPath),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) clangFormat(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path    string `json:"path"`
		InPlace bool   `json:"in_place"`
		Style   string `json:"style"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	argv := []string{"clang-format"}
	if args.InPlace {
		argv = append(argv, "-i")
	}
	if args.Style != "" {
		argv = append(argv, "-style="+args.Style)
	}
	argv = append(argv, path)
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	action := "check"
	if args.InPlace {
		action = "format"
	}
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran clang-format %s on %s", action, args.Path),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) clangTidy(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path   string `json:"path"`
		Checks string `json:"checks"`
		Fix    bool   `json:"fix"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	argv := []string{"clang-tidy"}
	if args.Checks != "" {
		argv = append(argv, "-checks="+args.Checks)
	}
	if args.Fix {
		argv = append(argv, "-fix")
	}
	argv = append(argv, path)
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran clang-tidy on %s", args.Path),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) makeBuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Target   string `json:"target"`
		BuildDir string `json:"build_dir"`
		Jobs     int    `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	workDir := r.root
	if args.BuildDir != "" {
		buildPath, err := r.safePath(args.BuildDir)
		if err != nil {
			return Result{}, err
		}
		workDir = buildPath
	}
	argv := []string{"make"}
	if args.Target != "" {
		argv = append(argv, args.Target)
	}
	if args.Jobs > 0 {
		argv = append(argv, fmt.Sprintf("-j%d", args.Jobs))
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran make %s in %s", args.Target, workDir),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) nmakeBuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Target   string            `json:"target"`
		BuildDir string            `json:"build_dir"`
		Macro    map[string]string `json:"macro"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	workDir := r.root
	if args.BuildDir != "" {
		buildPath, err := r.safePath(args.BuildDir)
		if err != nil {
			return Result{}, err
		}
		workDir = buildPath
	}
	argv := []string{"nmake"}
	if args.Target != "" {
		argv = append(argv, args.Target)
	}
	for k, v := range args.Macro {
		argv = append(argv, fmt.Sprintf("%s=%s", k, v))
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran nmake %s in %s", args.Target, workDir),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dockerBuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Dockerfile     string            `json:"dockerfile"`
		Tag            string            `json:"tag"`
		Context        string            `json:"context"`
		NoCache        bool              `json:"no_cache"`
		Pull           bool              `json:"pull"`
		BuildArgs      map[string]string `json:"build_args"`
		Target         string            `json:"target"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"build"}
	if args.Dockerfile != "" {
		argv = append(argv, "-f", args.Dockerfile)
	}
	if args.Tag != "" {
		argv = append(argv, "-t", args.Tag)
	}
	if args.NoCache {
		argv = append(argv, "--no-cache")
	}
	if args.Pull {
		argv = append(argv, "--pull")
	}
	for k, v := range args.BuildArgs {
		argv = append(argv, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	if args.Target != "" {
		argv = append(argv, "--target", args.Target)
	}
	if args.Context != "" {
		ctxPath, err := r.safePath(args.Context)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, ctxPath)
	} else {
		argv = append(argv, ".")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Built Docker image %s", args.Tag),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": args.Pull,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dockerRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Image          string            `json:"image"`
		Command        []string          `json:"command"`
		Name           string            `json:"name"`
		Ports          []string          `json:"ports"`
		Volumes        []string          `json:"volumes"`
		Env            map[string]string `json:"env"`
		Detach         bool              `json:"detach"`
		Rm             bool              `json:"rm"`
		Network        string            `json:"network"`
		Privileged     bool              `json:"privileged"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Image == "" {
		return Result{}, fmt.Errorf("image is required")
	}

	argv := []string{"run"}
	if args.Detach {
		argv = append(argv, "-d")
	}
	if args.Rm {
		argv = append(argv, "--rm")
	}
	if args.Name != "" {
		argv = append(argv, "--name", args.Name)
	}
	for _, p := range args.Ports {
		argv = append(argv, "-p", p)
	}
	for _, v := range args.Volumes {
		argv = append(argv, "-v", v)
	}
	for k, v := range args.Env {
		argv = append(argv, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	if args.Network != "" {
		argv = append(argv, "--network", args.Network)
	}
	if args.Privileged {
		argv = append(argv, "--privileged")
	}
	argv = append(argv, args.Image)
	if len(args.Command) > 0 {
		argv = append(argv, args.Command...)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran Docker container %s", args.Image),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dockerComposeUp(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		File           string   `json:"file"`
		Project        string   `json:"project"`
		Services       []string `json:"services"`
		Detach         bool     `json:"detach"`
		Build          bool     `json:"build"`
		ForceRecreate  bool     `json:"force_recreate"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"up"}
	if args.Detach {
		argv = append(argv, "-d")
	}
	if args.Build {
		argv = append(argv, "--build")
	}
	if args.ForceRecreate {
		argv = append(argv, "--force-recreate")
	}
	if len(args.Services) > 0 {
		argv = append(argv, args.Services...)
	}

	timeout := 180
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker-compose", argv...)
	if args.File != "" {
		filePath, err := r.safePath(args.File)
		if err != nil {
			return Result{}, err
		}
		argv = append([]string{"-f", filePath}, argv...)
	}
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran docker-compose up",
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dockerComposeDown(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		File           string `json:"file"`
		Project        string `json:"project"`
		Volumes        bool   `json:"volumes"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"down"}
	if args.Volumes {
		argv = append(argv, "-v")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker-compose", argv...)
	if args.File != "" {
		filePath, err := r.safePath(args.File)
		if err != nil {
			return Result{}, err
		}
		argv = append([]string{"-f", filePath}, argv...)
	}
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran docker-compose down",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) qemuRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Image          string   `json:"image"`
		Memory         string   `json:"memory"`
		Smp            int      `json:"smp"`
		Cpu            string   `json:"cpu"`
		Drive          string   `json:"drive"`
		Net            string   `json:"net"`
		Nographic      bool     `json:"nographic"`
		Monitor        string   `json:"monitor"`
		Serial         string   `json:"serial"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Image == "" {
		return Result{}, fmt.Errorf("image is required")
	}

	imagePath, err := r.safePath(args.Image)
	if err != nil {
		return Result{}, err
	}

	argv := []string{"-drive", "file=" + imagePath + ",format=qcow2"}
	if args.Memory != "" {
		argv = append(argv, "-m", args.Memory)
	}
	if args.Smp > 0 {
		argv = append(argv, "-smp", strconv.Itoa(args.Smp))
	}
	if args.Cpu != "" {
		argv = append(argv, "-cpu", args.Cpu)
	}
	if args.Drive != "" {
		argv = append(argv, "-drive", args.Drive)
	}
	if args.Net != "" {
		argv = append(argv, "-net", args.Net)
	}
	if args.Nographic {
		argv = append(argv, "-nographic")
	}
	if args.Monitor != "" {
		argv = append(argv, "-monitor", args.Monitor)
	}
	if args.Serial != "" {
		argv = append(argv, "-serial", args.Serial)
	}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "qemu-system-x86_64", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran QEMU with image %s", args.Image),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) adbDevices(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		TimeoutSeconds int `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "adb", "devices", "-l")
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Listed ADB devices",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) adbShell(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command        string `json:"command"`
		Serial         string `json:"serial"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Command == "" {
		return Result{}, fmt.Errorf("command is required")
	}

	argv := []string{"shell"}
	if args.Serial != "" {
		argv = append(argv, "-s", args.Serial)
	}
	argv = append(argv, args.Command)

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "adb", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran ADB shell: %s", args.Command),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) adbPush(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Local          string `json:"local"`
		Remote         string `json:"remote"`
		Serial         string `json:"serial"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Local == "" || args.Remote == "" {
		return Result{}, fmt.Errorf("local and remote are required")
	}

	localPath, err := r.safePath(args.Local)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(localPath); err != nil {
		return Result{}, fmt.Errorf("local file not found: %s", args.Local)
	}

	argv := []string{"push", localPath, args.Remote}
	if args.Serial != "" {
		argv = append(argv, "-s", args.Serial)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "adb", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Pushed %s to %s via ADB", args.Local, args.Remote),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) adbPull(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Remote         string `json:"remote"`
		Local          string `json:"local"`
		Serial         string `json:"serial"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Remote == "" || args.Local == "" {
		return Result{}, fmt.Errorf("remote and local are required")
	}

	localPath, err := r.safePath(args.Local)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return Result{}, err
	}

	argv := []string{"pull", args.Remote, localPath}
	if args.Serial != "" {
		argv = append(argv, "-s", args.Serial)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "adb", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Pulled %s to %s via ADB", args.Remote, args.Local),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) grepSearch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		Include       string `json:"include"`
		Exclude       string `json:"exclude"`
		CaseSensitive bool   `json:"case_sensitive"`
		FixedStrings  bool   `json:"fixed_strings"`
		Recursive     bool   `json:"recursive"`
		MaxResults    int    `json:"max_results"`
		AfterContext  int    `json:"after_context"`
		BeforeContext int    `json:"before_context"`
		Context       int    `json:"context"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Pattern == "" {
		return Result{}, fmt.Errorf("pattern is required")
	}

	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}

	argv := []string{"--color=never"}
	if !args.CaseSensitive {
		argv = append(argv, "-i")
	}
	if args.FixedStrings {
		argv = append(argv, "-F")
	}
	if args.Recursive {
		argv = append(argv, "-r")
	}
	if args.Include != "" {
		argv = append(argv, "--include="+args.Include)
	}
	if args.Exclude != "" {
		argv = append(argv, "--exclude="+args.Exclude)
	}
	if args.AfterContext > 0 {
		argv = append(argv, "-A", strconv.Itoa(args.AfterContext))
	}
	if args.BeforeContext > 0 {
		argv = append(argv, "-B", strconv.Itoa(args.BeforeContext))
	}
	if args.Context > 0 {
		argv = append(argv, "-C", strconv.Itoa(args.Context))
	}
	if args.MaxResults > 0 {
		argv = append(argv, "-m", strconv.Itoa(args.MaxResults))
	}
	argv = append(argv, args.Pattern, root)

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "grep", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Searched for %q with grep", args.Pattern),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) findFiles(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path           string `json:"path"`
		Name           string `json:"name"`
		Type           string `json:"type"`
		MaxDepth       int    `json:"max_depth"`
		Modified       int    `json:"modified"`
		Size           string `json:"size"`
		Exec           string `json:"exec"`
		Print0         bool   `json:"print0"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}

	argv := []string{root}
	if args.Name != "" {
		argv = append(argv, "-name", args.Name)
	}
	if args.Type != "" {
		argv = append(argv, "-type", args.Type)
	}
	if args.MaxDepth > 0 {
		argv = append(argv, "-maxdepth", strconv.Itoa(args.MaxDepth))
	}
	if args.Modified > 0 {
		argv = append(argv, "-mtime", strconv.Itoa(args.Modified))
	}
	if args.Size != "" {
		argv = append(argv, "-size", args.Size)
	}
	if args.Exec != "" {
		argv = append(argv, "-exec", args.Exec)
	}
	if args.Print0 {
		argv = append(argv, "-print0")
	} else {
		argv = append(argv, "-print")
	}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "find", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran find",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) tailFile(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path           string `json:"path"`
		Lines          int    `json:"lines"`
		Bytes          int    `json:"bytes"`
		Follow         bool   `json:"follow"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}

	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return Result{}, fmt.Errorf("file not found: %s", args.Path)
	}

	argv := []string{"-n"}
	if args.Lines > 0 {
		argv = append(argv, strconv.Itoa(args.Lines))
	} else {
		argv = append(argv, "10")
	}
	if args.Bytes > 0 {
		argv = append(argv, "-c", strconv.Itoa(args.Bytes))
	}
	if args.Follow {
		argv = append(argv, "-f")
	}
	argv = append(argv, path)

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "tail", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Tail %s", args.Path),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) awkProcess(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Script         string            `json:"script"`
		Path           string            `json:"path"`
		FieldSeparator string            `json:"field_separator"`
		Vars           map[string]string `json:"vars"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Script == "" {
		return Result{}, fmt.Errorf("script is required")
	}

	argv := []string{args.Script}
	if args.FieldSeparator != "" {
		argv = append(argv, "-F", args.FieldSeparator)
	}
	for k, v := range args.Vars {
		argv = append(argv, "-v", fmt.Sprintf("%s=%s", k, v))
	}
	if args.Path != "" {
		path, err := r.safePath(args.Path)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Stat(path); err != nil {
			return Result{}, fmt.Errorf("file not found: %s", args.Path)
		}
		argv = append(argv, path)
	}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "awk", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran awk",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) psList(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		All            bool   `json:"all"`
		User           string `json:"user"`
		OutputFormat   string `json:"output_format"`
		Sort           string `json:"sort"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"-e"}
	if args.User != "" {
		argv = []string{"-u", args.User}
	}
	if args.OutputFormat != "" {
		argv = append(argv, "-o", args.OutputFormat)
	}
	if args.Sort != "" {
		argv = append(argv, "--sort", args.Sort)
	}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ps", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Listed processes",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) chmodChange(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path           string `json:"path"`
		Mode           string `json:"mode"`
		Recursive      bool   `json:"recursive"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Path == "" || args.Mode == "" {
		return Result{}, fmt.Errorf("path and mode are required")
	}

	target, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(target); err != nil {
		return Result{}, fmt.Errorf("path not found: %s", args.Path)
	}

	mode := args.Mode

	argv := []string{}
	if args.Recursive {
		argv = append(argv, "-R")
	}
	argv = append(argv, mode, target)

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "chmod", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Changed mode of %s to %s", args.Path, args.Mode),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) chownChange(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path           string `json:"path"`
		Owner          string `json:"owner"`
		Group          string `json:"group"`
		Recursive      bool   `json:"recursive"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Path == "" || args.Owner == "" {
		return Result{}, fmt.Errorf("path and owner are required")
	}

	target, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(target); err != nil {
		return Result{}, fmt.Errorf("path not found: %s", args.Path)
	}

	ownerGroup := args.Owner
	if args.Group != "" {
		ownerGroup = fmt.Sprintf("%s:%s", args.Owner, args.Group)
	}

	argv := []string{}
	if args.Recursive {
		argv = append(argv, "-R")
	}
	argv = append(argv, ownerGroup, target)

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "chown", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Changed owner of %s to %s", args.Path, ownerGroup),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) userAdd(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Username       string   `json:"username"`
		Home           string   `json:"home"`
		Shell          string   `json:"shell"`
		Groups         []string `json:"groups"`
		System         bool     `json:"system"`
		CreateHome     bool     `json:"create_home"`
		NoCreateHome   bool     `json:"no_create_home"`
		UID            int      `json:"uid"`
		GID            int      `json:"gid"`
		Password       string   `json:"password"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Username == "" {
		return Result{}, fmt.Errorf("username is required")
	}

	argv := []string{args.Username}
	if args.Home != "" {
		argv = append(argv, "-d", args.Home)
	}
	if args.Shell != "" {
		argv = append(argv, "-s", args.Shell)
	}
	if len(args.Groups) > 0 {
		argv = append(argv, "-G", strings.Join(args.Groups, ","))
	}
	if args.System {
		argv = append(argv, "-r")
	}
	if args.CreateHome {
		argv = append(argv, "-m")
	}
	if args.NoCreateHome {
		argv = append(argv, "-M")
	}
	if args.UID > 0 {
		argv = append(argv, "-u", strconv.Itoa(args.UID))
	}
	if args.GID > 0 {
		argv = append(argv, "-g", strconv.Itoa(args.GID))
	}
	if args.Password != "" {
		argv = append(argv, "-p", args.Password)
	}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "useradd", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Added user %s", args.Username),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) userDel(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Username       string `json:"username"`
		Remove         bool   `json:"remove"`
		Force          bool   `json:"force"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Username == "" {
		return Result{}, fmt.Errorf("username is required")
	}

	argv := []string{}
	if args.Remove {
		argv = append(argv, "-r")
	}
	if args.Force {
		argv = append(argv, "-f")
	}
	argv = append(argv, args.Username)

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "userdel", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Deleted user %s", args.Username),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) arCreate(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string   `json:"archive"`
		Files          []string `json:"files"`
		WorkingDir     string   `json:"working_dir"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"-cvq", archivePath}
	for _, f := range args.Files {
		filePath, err := r.safePath(f)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, filePath)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ar", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Created ar archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) arExtract(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string `json:"archive"`
		OutputDir      string `json:"output_dir"`
		WorkingDir     string `json:"working_dir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	outputDir := workDir
	if args.OutputDir != "" {
		out, err := r.safePath(args.OutputDir)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return Result{}, err
		}
		outputDir = out
	}

	argv := []string{"-x", archivePath}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ar", argv...)
	cmd.Dir = outputDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Extracted ar archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) arList(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string `json:"archive"`
		WorkingDir     string `json:"working_dir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"-tv", archivePath}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ar", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Listed ar archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) tarCreate(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string   `json:"archive"`
		Files          []string `json:"files"`
		Compress       string   `json:"compress"`
		WorkingDir     string   `json:"working_dir"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"-cf"}
	if args.Compress != "" {
		switch strings.ToLower(args.Compress) {
		case "gz", "gzip":
			argv = []string{"-czf"}
		case "bz2", "bzip2":
			argv = []string{"-cjf"}
		case "xz":
			argv = []string{"-cJf"}
		default:
			return Result{}, fmt.Errorf("unsupported compress format: %s", args.Compress)
		}
	}
	argv = append(argv, archivePath)
	for _, f := range args.Files {
		filePath, err := r.safePath(f)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, filePath)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "tar", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Created tar archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) tarExtract(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive         string `json:"archive"`
		OutputDir       string `json:"output_dir"`
		Compress        string `json:"compress"`
		StripComponents int    `json:"strip_components"`
		WorkingDir      string `json:"working_dir"`
		TimeoutSeconds  int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	outputDir := workDir
	if args.OutputDir != "" {
		out, err := r.safePath(args.OutputDir)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return Result{}, err
		}
		outputDir = out
	}

	argv := []string{"-xf", archivePath}
	if args.Compress != "" {
		switch strings.ToLower(args.Compress) {
		case "gz", "gzip":
			argv = []string{"-xzf", archivePath}
		case "bz2", "bzip2":
			argv = []string{"-xjf", archivePath}
		case "xz":
			argv = []string{"-xJf", archivePath}
		default:
			return Result{}, fmt.Errorf("unsupported compress format: %s", args.Compress)
		}
	}
	if args.StripComponents > 0 {
		argv = append(argv, "--strip-components", strconv.Itoa(args.StripComponents))
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "tar", argv...)
	cmd.Dir = outputDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Extracted tar archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) tarList(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string `json:"archive"`
		Verbose        bool   `json:"verbose"`
		Compress       string `json:"compress"`
		WorkingDir     string `json:"working_dir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"-tf", archivePath}
	if args.Compress != "" {
		switch strings.ToLower(args.Compress) {
		case "gz", "gzip":
			argv = []string{"-tzf", archivePath}
		case "bz2", "bzip2":
			argv = []string{"-tjf", archivePath}
		case "xz":
			argv = []string{"-tJf", archivePath}
		default:
			return Result{}, fmt.Errorf("unsupported compress format: %s", args.Compress)
		}
	}
	if args.Verbose {
		argv = append([]string{"-v"}, argv...)
	}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "tar", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Listed tar archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) zipCreate(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string   `json:"archive"`
		Files          []string `json:"files"`
		WorkingDir     string   `json:"working_dir"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"-r", archivePath}
	for _, f := range args.Files {
		filePath, err := r.safePath(f)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, filePath)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "zip", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Created zip archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) zipExtract(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string `json:"archive"`
		OutputDir      string `json:"output_dir"`
		WorkingDir     string `json:"working_dir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	outputDir := workDir
	if args.OutputDir != "" {
		out, err := r.safePath(args.OutputDir)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			return Result{}, err
		}
		outputDir = out
	}

	argv := []string{"-o", archivePath}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "unzip", argv...)
	cmd.Dir = outputDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Extracted zip archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) zipList(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Archive        string `json:"archive"`
		WorkingDir     string `json:"working_dir"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Archive == "" {
		return Result{}, fmt.Errorf("archive is required")
	}

	archivePath, err := r.safePath(args.Archive)
	if err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"-l", archivePath}

	timeout := 30
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "unzip", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Listed zip archive %s", args.Archive),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) flutterRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Device         string `json:"device"`
		Route          string `json:"route"`
		Debug          bool   `json:"debug"`
		Verbose        bool   `json:"verbose"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"run"}
	if args.Device != "" {
		argv = append(argv, "-d", args.Device)
	}
	if args.Route != "" {
		argv = append(argv, "--route", args.Route)
	}
	if args.Debug {
		argv = append(argv, "--debug")
	}
	if args.Verbose {
		argv = append(argv, "-v")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran flutter run",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) flutterBuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Targets        string `json:"targets"`
		DeviceID       string `json:"device_id"`
		WebRenderer    string `json:"web_renderer"`
		ReleaseMode    string `json:"release_mode"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"build", args.Targets}
	if args.DeviceID != "" {
		argv = append(argv, "-d", args.DeviceID)
	}
	if args.WebRenderer != "" {
		argv = append(argv, "--web-renderer", args.WebRenderer)
	}
	if args.ReleaseMode != "" {
		argv = append(argv, "--"+args.ReleaseMode)
	}

	timeout := 180
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran flutter build %s", args.Targets),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) flutterTest(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path           string   `json:"path"`
		Tags           []string `json:"tags"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"test"}
	if args.Path != "" {
		argv = append(argv, args.Path)
	}
	for _, tag := range args.Tags {
		argv = append(argv, "-t", tag)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran flutter test",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dartRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Script         string   `json:"script"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Script == "" {
		return Result{}, fmt.Errorf("script is required")
	}

	scriptPath, err := r.safePath(args.Script)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(scriptPath); err != nil {
		return Result{}, fmt.Errorf("script not found: %s", args.Script)
	}

	argv := []string{"run", scriptPath}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dart", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran dart run %s", args.Script),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dartAnalyze(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path           string `json:"path"`
		FatalWarnings  bool   `json:"fatal_warnings"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"analyze"}
	if args.Path != "" {
		argv = append(argv, args.Path)
	}
	if args.FatalWarnings {
		argv = append(argv, "--fatal-warnings")
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dart", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran dart analyze",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dartFormat(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path             string `json:"path"`
		SetExitIfChanged bool   `json:"set_exit_if_changed"`
		TimeoutSeconds   int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	argv := []string{"format"}
	if args.Path != "" {
		argv = append(argv, args.Path)
	}
	if args.SetExitIfChanged {
		argv = append(argv, "--set-exit-if-changed")
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dart", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran dart format",
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pubGet(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		WorkingDir     string `json:"working_dir"`
		Offline        bool   `json:"offline"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"pub", "get"}
	if args.Offline {
		argv = append(argv, "--offline")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran flutter pub get",
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": !args.Offline,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pubUpgrade(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		WorkingDir     string `json:"working_dir"`
		MajorOnly      bool   `json:"major_only"`
		Offline        bool   `json:"offline"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"pub", "upgrade"}
	if args.MajorOnly {
		argv = append(argv, "--major-only")
	}
	if args.Offline {
		argv = append(argv, "--offline")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: "Ran flutter pub upgrade",
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": !args.Offline,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pubAdd(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Name           string `json:"name"`
		Version        string `json:"version"`
		Dev            bool   `json:"dev"`
		WorkingDir     string `json:"working_dir"`
		Offline        bool   `json:"offline"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Name == "" {
		return Result{}, fmt.Errorf("name is required")
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"pub", "add", args.Name}
	if args.Version != "" {
		argv = append(argv, "--"+args.Version)
	}
	if args.Dev {
		argv = append(argv, "--dev")
	}
	if args.Offline {
		argv = append(argv, "--offline")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Added %s to pubspec.yaml", args.Name),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": !args.Offline,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pubRemove(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Name           string `json:"name"`
		Dev            bool   `json:"dev"`
		WorkingDir     string `json:"working_dir"`
		Offline        bool   `json:"offline"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Name == "" {
		return Result{}, fmt.Errorf("name is required")
	}

	workDir := r.root
	if args.WorkingDir != "" {
		wd, err := r.safePath(args.WorkingDir)
		if err != nil {
			return Result{}, err
		}
		workDir = wd
	}

	argv := []string{"pub", "remove", args.Name}
	if args.Dev {
		argv = append(argv, "--dev")
	}
	if args.Offline {
		argv = append(argv, "--offline")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "flutter", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Removed %s from pubspec.yaml", args.Name),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": !args.Offline,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pythonRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Script         string   `json:"script"`
		Eval           string   `json:"eval"`
		Args           []string `json:"args"`
		VMArgs         []string `json:"vm_args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Script == "" && args.Eval == "" {
		return Result{}, fmt.Errorf("script or eval is required")
	}

	argv := []string{}
	if len(args.VMArgs) > 0 {
		argv = append(argv, args.VMArgs...)
	}
	if args.Eval != "" {
		argv = append(argv, "-c", args.Eval)
	} else {
		scriptPath, err := r.safePath(args.Script)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Stat(scriptPath); err != nil {
			return Result{}, fmt.Errorf("script not found: %s", args.Script)
		}
		argv = append(argv, scriptPath)
	}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "python", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran Python %s", args.Script),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) python3Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Script         string   `json:"script"`
		Eval           string   `json:"eval"`
		Args           []string `json:"args"`
		VMArgs         []string `json:"vm_args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Script == "" && args.Eval == "" {
		return Result{}, fmt.Errorf("script or eval is required")
	}

	argv := []string{}
	if len(args.VMArgs) > 0 {
		argv = append(argv, args.VMArgs...)
	}
	if args.Eval != "" {
		argv = append(argv, "-c", args.Eval)
	} else {
		scriptPath, err := r.safePath(args.Script)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Stat(scriptPath); err != nil {
			return Result{}, fmt.Errorf("script not found: %s", args.Script)
		}
		argv = append(argv, scriptPath)
	}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "python3", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran Python3 %s", args.Script),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pipInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Version        string `json:"version"`
		Requirements   string `json:"requirements"`
		IndexURL       string `json:"index_url"`
		ExtraIndexURL  string `json:"extra_index_url"`
		Upgrade        bool   `json:"upgrade"`
		User           bool   `json:"user"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" && args.Requirements == "" {
		return Result{}, fmt.Errorf("package or requirements is required")
	}

	argv := []string{"install"}
	if args.Requirements != "" {
		reqPath, err := r.safePath(args.Requirements)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, "-r", reqPath)
	} else {
		pkg := args.Package
		if args.Version != "" {
			pkg = fmt.Sprintf("%s==%s", pkg, args.Version)
		}
		argv = append(argv, pkg)
	}
	if args.IndexURL != "" {
		argv = append(argv, "--index-url", args.IndexURL)
	}
	if args.ExtraIndexURL != "" {
		argv = append(argv, "--extra-index-url", args.ExtraIndexURL)
	}
	if args.Upgrade {
		argv = append(argv, "--upgrade")
	}
	if args.User {
		argv = append(argv, "--user")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "pip", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with pip", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) pip3Install(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Version        string `json:"version"`
		Requirements   string `json:"requirements"`
		IndexURL       string `json:"index_url"`
		ExtraIndexURL  string `json:"extra_index_url"`
		Upgrade        bool   `json:"upgrade"`
		User           bool   `json:"user"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" && args.Requirements == "" {
		return Result{}, fmt.Errorf("package or requirements is required")
	}

	argv := []string{"install"}
	if args.Requirements != "" {
		reqPath, err := r.safePath(args.Requirements)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, "-r", reqPath)
	} else {
		pkg := args.Package
		if args.Version != "" {
			pkg = fmt.Sprintf("%s==%s", pkg, args.Version)
		}
		argv = append(argv, pkg)
	}
	if args.IndexURL != "" {
		argv = append(argv, "--index-url", args.IndexURL)
	}
	if args.ExtraIndexURL != "" {
		argv = append(argv, "--extra-index-url", args.ExtraIndexURL)
	}
	if args.Upgrade {
		argv = append(argv, "--upgrade")
	}
	if args.User {
		argv = append(argv, "--user")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "pip3", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with pip3", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) condaInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Channel        string `json:"channel"`
		Version        string `json:"version"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}

	argv := []string{"install", "-y", args.Package}
	if args.Channel != "" {
		argv = append(argv, "-c", args.Channel)
	}
	if args.Version != "" {
		argv = append(argv, args.Version+"="+args.Version)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "conda", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with conda", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) condaCreate(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Name           string   `json:"name"`
		Python         string   `json:"python"`
		Packages       []string `json:"packages"`
		Channel        string   `json:"channel"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Name == "" {
		return Result{}, fmt.Errorf("name is required")
	}

	argv := []string{"create", "-n", args.Name, "-y"}
	if args.Python != "" {
		argv = append(argv, "python="+args.Python)
	}
	for _, pkg := range args.Packages {
		argv = append(argv, pkg)
	}
	if args.Channel != "" {
		argv = append(argv, "-c", args.Channel)
	}

	timeout := 180
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "conda", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Created conda env %s", args.Name),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) wingetInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Version        string `json:"version"`
		Scope          string `json:"scope"`
		Source         string `json:"source"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install", args.Package, "--exact"}
	if args.Version != "" {
		argv = append(argv, "--version", args.Version)
	}
	if args.Scope != "" {
		argv = append(argv, "--scope", args.Scope)
	}
	if args.Source != "" {
		argv = append(argv, "--source", args.Source)
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "winget", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with winget", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) chocoInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Version        string `json:"version"`
		Source         string `json:"source"`
		Force          bool   `json:"force"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install", args.Package, "-y"}
	if args.Version != "" {
		argv = append(argv, "--version", args.Version)
	}
	if args.Source != "" {
		argv = append(argv, "--source", args.Source)
	}
	if args.Force {
		argv = append(argv, "--force")
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "choco", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with chocolatey", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) aptInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Update         bool   `json:"update"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install", "-y", args.Package}
	if args.Update {
		argv = append([]string{"update"}, argv...)
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "apt", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with apt", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) aptGetInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Update         bool   `json:"update"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install", "-y", args.Package}
	if args.Update {
		argv = append([]string{"update"}, argv...)
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "apt-get", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with apt-get", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) snapInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Classic        bool   `json:"classic"`
		Channel        string `json:"channel"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install", args.Package}
	if args.Classic {
		argv = append(argv, "--classic")
	}
	if args.Channel != "" {
		argv = append(argv, "--channel", args.Channel)
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "snap", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with snap", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dnfInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Refresh        bool   `json:"refresh"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install", "-y", args.Package}
	if args.Refresh {
		argv = append([]string{"refresh"}, argv...)
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dnf", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with dnf", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) brewInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string `json:"package"`
		Cask           bool   `json:"cask"`
		Tap            string `json:"tap"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}
	argv := []string{"install"}
	if args.Cask {
		argv = append(argv, "--cask")
	}
	if args.Tap != "" {
		argv = append(argv, args.Tap+"/"+args.Package)
	} else {
		argv = append(argv, args.Package)
	}
	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "brew", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed %s with brew", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) curlFetch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		URL      string            `json:"url"`
		Method   string            `json:"method"`
		Headers  map[string]string `json:"headers"`
		Data     string            `json:"data"`
		Output   string            `json:"output"`
		Location bool              `json:"location"`
		MaxTime  int               `json:"max_time"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.URL == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	if err := rejectDangerousUrl(args.URL); err != nil {
		return Result{}, err
	}

	argv := []string{"-s", "-S"}
	if args.Method != "" {
		argv = append(argv, "-X", args.Method)
	}
	if args.MaxTime > 0 {
		argv = append(argv, "--max-time", strconv.Itoa(args.MaxTime))
	}
	if args.Location {
		argv = append(argv, "-L")
	}
	if args.Data != "" {
		argv = append(argv, "--data-raw", args.Data)
	}
	for k, v := range args.Headers {
		argv = append(argv, "-H", k+": "+v)
	}
	argv = append(argv, args.URL)
	if args.Output != "" {
		outPath, err := r.safePath(args.Output)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return Result{}, err
		}
		argv = append(argv, "-o", outPath)
	}

	summary := fmt.Sprintf("Fetched %s with curl", args.URL)
	if args.Output != "" {
		summary = fmt.Sprintf("Downloaded %s to %s with curl", args.URL, args.Output)
	}

	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "curl", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: summary,
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) wgetFetch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		URL                string `json:"url"`
		Output             string `json:"output"`
		Recursive          bool   `json:"recursive"`
		PageRequisites     bool   `json:"page_requisites"`
		MaxDepth           int    `json:"max_depth"`
		Reject             string `json:"reject"`
		ExcludeDirectories string `json:"exclude_directories"`
		Continue           bool   `json:"continue"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.URL == "" {
		return Result{}, fmt.Errorf("url is required")
	}
	if err := rejectDangerousUrl(args.URL); err != nil {
		return Result{}, err
	}

	argv := []string{"-q"}
	if args.Output != "" {
		outPath, err := r.safePath(args.Output)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return Result{}, err
		}
		argv = append(argv, "-O", outPath)
	} else {
		argv = append(argv, "-P", r.root)
	}
	if args.Recursive {
		argv = append(argv, "-r")
	}
	if args.PageRequisites {
		argv = append(argv, "-p")
	}
	if args.MaxDepth > 0 {
		argv = append(argv, "-l", strconv.Itoa(args.MaxDepth))
	}
	if args.Reject != "" {
		extList := []string{}
		for _, ext := range strings.Split(args.Reject, ",") {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				extList = append(extList, ext)
			}
		}
		if len(extList) > 0 {
			argv = append(argv, "-R", strings.Join(extList, ","))
		}
	}
	if args.ExcludeDirectories != "" {
		argv = append(argv, "-X", args.ExcludeDirectories)
	}
	if args.Continue {
		argv = append(argv, "-c")
	}
	argv = append(argv, args.URL)

	action := "download"
	if args.Recursive {
		action = "recursive download"
	}

	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "wget", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Fetched %s with wget (%s)", args.URL, action),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func rejectDangerousUrl(url string) error {
	lower := strings.ToLower(url)
	dangerous := []string{
		"file://",
		"ftp://",
		"gopher://",
		"dict://",
		"ldap://",
		"ldaps://",
		"sftp://",
		"tftp://",
	}
	for _, p := range dangerous {
		if strings.HasPrefix(lower, p) {
			return fmt.Errorf("refusing dangerous URL scheme: %s", p)
		}
	}
	return nil
}

func (r *Registry) javacCompile(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Source     string `json:"source"`
		Classpath  string `json:"classpath"`
		OutputDir  string `json:"output_dir"`
		Sourcepath string `json:"sourcepath"`
		Release    string `json:"release"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Source == "" {
		return Result{}, fmt.Errorf("source is required")
	}
	sourcePath, err := r.safePath(args.Source)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return Result{}, fmt.Errorf("source file not found: %s", args.Source)
	}

	workDir := r.root
	outputDir := ""
	if args.OutputDir != "" {
		outputDir, err = r.safePath(args.OutputDir)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return Result{}, err
		}
	}

	argv := []string{"-d"}
	if outputDir != "" {
		argv = append(argv, outputDir)
	} else {
		argv = append(argv, ".")
	}
	if args.Classpath != "" {
		argv = append(argv, "-cp", args.Classpath)
	}
	if args.Release != "" {
		argv = append(argv, "--release", args.Release)
	}
	argv = append(argv, sourcePath)

	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "javac", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Compiled %s with javac", args.Source),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) javaRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		MainClass string   `json:"main_class"`
		Jar       string   `json:"jar"`
		Args      []string `json:"args"`
		VMArgs    []string `json:"vm_args"`
		Classpath string   `json:"classpath"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.MainClass == "" && args.Jar == "" {
		return Result{}, fmt.Errorf("main_class or jar is required")
	}

	workDir := r.root
	argv := make([]string, 0)
	if len(args.VMArgs) > 0 {
		argv = append(argv, args.VMArgs...)
	}
	if args.Jar != "" {
		jarPath, err := r.safePath(args.Jar)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Stat(jarPath); err != nil {
			return Result{}, fmt.Errorf("jar file not found: %s", args.Jar)
		}
		argv = append(argv, "-jar", jarPath)
	} else {
		argv = append(argv, "-cp")
		if args.Classpath != "" {
			argv = append(argv, args.Classpath)
		} else {
			argv = append(argv, ".")
		}
		argv = append(argv, args.MainClass)
	}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "java", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	action := args.MainClass
	if args.Jar != "" {
		action = args.Jar
	}
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran Java %s", action),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) rgSearch(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Pattern       string `json:"pattern"`
		Path          string `json:"path"`
		Glob          string `json:"glob"`
		FileType      string `json:"file_type"`
		CaseSensitive bool   `json:"case_sensitive"`
		FixedStrings  bool   `json:"fixed_strings"`
		MaxResults    int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Pattern == "" {
		return Result{}, fmt.Errorf("pattern is required")
	}
	if args.MaxResults <= 0 {
		args.MaxResults = 100
	}
	root, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}

	argv := []string{"--no-heading", "--line-number"}
	if !args.CaseSensitive {
		argv = append(argv, "-i")
	}
	if args.FixedStrings {
		argv = append(argv, "-F")
	}
	if args.Glob != "" {
		argv = append(argv, "-g", args.Glob)
	}
	if args.FileType != "" {
		argv = append(argv, "-t", args.FileType)
	}
	argv = append(argv, "-l", strconv.Itoa(args.MaxResults))
	argv = append(argv, args.Pattern)
	argv = append(argv, root)

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "rg", argv...)
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Searched for %q with rg", args.Pattern),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) sedEdit(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path       string `json:"path"`
		Expression string `json:"expression"`
		Script     string `json:"script"`
		InPlace    bool   `json:"in_place"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Path == "" {
		return Result{}, fmt.Errorf("path is required")
	}
	if args.Expression == "" && args.Script == "" {
		return Result{}, fmt.Errorf("expression or script is required")
	}
	path, err := r.safePath(args.Path)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return Result{}, fmt.Errorf("file not found: %s", args.Path)
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if args.InPlace {
		cmd := exec.CommandContext(cctx, "sed", "-i", "-E", "-e", args.Expression, path)
		cmd.Dir = r.root
		out, err := runWithKill(cctx, cmd)
		res := Result{
			OK:      err == nil,
			Summary: fmt.Sprintf("Applied sed to %s in-place", args.Path),
			Content: truncate(string(out), 20000),
		}
		if err != nil {
			return res, err
		}
		return res, nil
	}

	cmd := exec.CommandContext(cctx, "sed", "-E", "-e", args.Expression, path)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	if err != nil {
		return Result{OK: false, Summary: fmt.Sprintf("sed failed on %s", args.Path), Content: truncate(string(out), 20000)}, err
	}
	return Result{OK: true, Summary: fmt.Sprintf("Ran sed on %s", args.Path), Content: truncate(string(out), 20000)}, nil
}

func (r *Registry) base64Encode(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Input  string `json:"input"`
		Path   string `json:"path"`
		Decode bool   `json:"decode"`
		Wrap   int    `json:"wrap"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Input == "" && args.Path == "" {
		return Result{}, fmt.Errorf("input or path is required")
	}

	var data []byte
	if args.Path != "" {
		path, err := r.safePath(args.Path)
		if err != nil {
			return Result{}, err
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return Result{}, err
		}
	} else {
		data = []byte(args.Input)
	}

	argv := []string{"base64"}
	if args.Decode {
		argv = []string{"base64", "-d"}
	}
	if args.Wrap > 0 {
		argv = append(argv, "-w", strconv.Itoa(args.Wrap))
	}
	argv = append(argv, "-")

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = r.root
	cmd.Stdin = bytes.NewReader(data)
	out, err := runWithKill(cctx, cmd)

	action := "encode"
	if args.Decode {
		action = "decode"
	}
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Performed base64 %s", action),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) nodeRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Script         string   `json:"script"`
		Eval           string   `json:"eval"`
		Args           []string `json:"args"`
		VMArgs         []string `json:"vm_args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Script == "" && args.Eval == "" {
		return Result{}, fmt.Errorf("script or eval is required")
	}

	argv := []string{}
	if len(args.VMArgs) > 0 {
		argv = append(argv, args.VMArgs...)
	}
	if args.Eval != "" {
		argv = append(argv, "-e", args.Eval)
	} else {
		scriptPath, err := r.safePath(args.Script)
		if err != nil {
			return Result{}, err
		}
		if _, err := os.Stat(scriptPath); err != nil {
			return Result{}, fmt.Errorf("script not found: %s", args.Script)
		}
		argv = append(argv, scriptPath)
	}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 60
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "node", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran Node.js %s", args.Script),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) npmCommand(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Command        string   `json:"command"`
		Script         string   `json:"script"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Command == "" && args.Script == "" {
		return Result{}, fmt.Errorf("command or script is required")
	}

	argv := []string{"npm"}
	if args.Script != "" {
		argv = append(argv, "run", args.Script)
	} else {
		argv = append(argv, args.Command)
	}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran npm %s", args.Command),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": IsNpmCommandNetwork(raw),
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) npxCommand(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string   `json:"package"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}

	argv := []string{"npx", "-y", args.Package}
	if len(args.Args) > 0 {
		argv = append(argv, args.Args...)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran npx %s", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) nvmUse(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Version == "" {
		return Result{}, fmt.Errorf("version is required")
	}

	nvmScript := ""
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".nvm", "nvm.sh"),
		"/usr/local/nvm/nvm.sh",
		"/opt/homebrew/nvm/nvm.sh",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			nvmScript = c
			break
		}
	}
	if nvmScript == "" {
		return Result{}, fmt.Errorf("nvm not found: checked ~/.nvm/nvm.sh, /usr/local/nvm/nvm.sh, /opt/homebrew/nvm/nvm.sh")
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "bash", "-c", fmt.Sprintf("source %s && nvm use %s", nvmScript, args.Version))
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Switched Node.js to %s via nvm", args.Version),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dotnetRun(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Project        string   `json:"project"`
		Args           []string `json:"args"`
		Config         string   `json:"configuration"`
		Framework      string   `json:"framework"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	workDir := r.root
	argv := []string{"run"}
	if args.Project != "" {
		projectPath, err := r.safePath(args.Project)
		if err != nil {
			return Result{}, err
		}
		workDir = filepath.Dir(projectPath)
		argv = append(argv, "--project", projectPath)
	}
	if args.Config != "" {
		argv = append(argv, "--configuration", args.Config)
	}
	if args.Framework != "" {
		argv = append(argv, "--framework", args.Framework)
	}
	if len(args.Args) > 0 {
		argv = append(argv, "--")
		argv = append(argv, args.Args...)
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dotnet", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran dotnet run in %s", workDir),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dotnetBuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Project        string `json:"project"`
		Config         string `json:"configuration"`
		Framework      string `json:"framework"`
		Output         string `json:"output"`
		NoRestore      bool   `json:"no_restore"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	workDir := r.root
	argv := []string{"build"}
	if args.Project != "" {
		projectPath, err := r.safePath(args.Project)
		if err != nil {
			return Result{}, err
		}
		workDir = filepath.Dir(projectPath)
		argv = append(argv, projectPath)
	}
	if args.Config != "" {
		argv = append(argv, "--configuration", args.Config)
	}
	if args.Framework != "" {
		argv = append(argv, "--framework", args.Framework)
	}
	if args.Output != "" {
		outPath, err := r.safePath(args.Output)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(outPath, 0o755); err != nil {
			return Result{}, err
		}
		argv = append(argv, "--output", outPath)
	}
	if args.NoRestore {
		argv = append(argv, "--no-restore")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dotnet", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran dotnet build in %s", workDir),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) dotnetTest(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Project        string `json:"project"`
		Filter         string `json:"filter"`
		Config         string `json:"configuration"`
		Framework      string `json:"framework"`
		NoBuild        bool   `json:"no_build"`
		Logger         string `json:"logger"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	workDir := r.root
	argv := []string{"test"}
	if args.Project != "" {
		projectPath, err := r.safePath(args.Project)
		if err != nil {
			return Result{}, err
		}
		workDir = filepath.Dir(projectPath)
		argv = append(argv, projectPath)
	}
	if args.Filter != "" {
		argv = append(argv, "--filter", args.Filter)
	}
	if args.Config != "" {
		argv = append(argv, "--configuration", args.Config)
	}
	if args.Framework != "" {
		argv = append(argv, "--framework", args.Framework)
	}
	if args.NoBuild {
		argv = append(argv, "--no-build")
	}
	if args.Logger != "" {
		argv = append(argv, "--logger", args.Logger)
	}

	timeout := 180
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dotnet", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran dotnet test in %s", workDir),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) msbuild(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Project        string            `json:"project"`
		Target         string            `json:"target"`
		Config         string            `json:"configuration"`
		Property       map[string]string `json:"property"`
		MaxCPUCount    int               `json:"max_cpu_count"`
		TimeoutSeconds int               `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Project == "" {
		return Result{}, fmt.Errorf("project is required")
	}
	projectPath, err := r.safePath(args.Project)
	if err != nil {
		return Result{}, err
	}

	argv := []string{projectPath}
	if args.Target != "" {
		argv = append(argv, "-t:"+args.Target)
	}
	if args.Config != "" {
		argv = append(argv, "-p:Configuration="+args.Config)
	}
	for k, v := range args.Property {
		argv = append(argv, "-p:"+k+"="+v)
	}
	if args.MaxCPUCount > 0 {
		argv = append(argv, fmt.Sprintf("-maxcpucount:%d", args.MaxCPUCount))
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "msbuild", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Ran msbuild on %s", args.Project),
		Content: truncate(string(out), 20000),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) nugetRestore(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Project        string   `json:"project"`
		Packages       string   `json:"packages"`
		Source         []string `json:"source"`
		ConfigFile     string   `json:"config_file"`
		Force          bool     `json:"force"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}

	workDir := r.root
	argv := []string{"restore"}
	if args.Project != "" {
		projectPath, err := r.safePath(args.Project)
		if err != nil {
			return Result{}, err
		}
		workDir = filepath.Dir(projectPath)
		argv = append(argv, projectPath)
	}
	if args.Packages != "" {
		argv = append(argv, "-PackagesDirectory", args.Packages)
	}
	for _, src := range args.Source {
		argv = append(argv, "-Source", src)
	}
	if args.ConfigFile != "" {
		configPath, err := r.safePath(args.ConfigFile)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, "-ConfigFile", configPath)
	}
	if args.Force {
		argv = append(argv, "-Force")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dotnet", argv...)
	cmd.Dir = workDir
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Restored NuGet packages in %s", workDir),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (r *Registry) nugetInstall(ctx context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Package        string   `json:"package"`
		Version        string   `json:"version"`
		OutputDir      string   `json:"output_dir"`
		Source         []string `json:"source"`
		ConfigFile     string   `json:"config_file"`
		Prerelease     bool     `json:"prerelease"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	if args.Package == "" {
		return Result{}, fmt.Errorf("package is required")
	}

	argv := []string{"add", "package", args.Package}
	if args.Version != "" {
		argv = append(argv, "-v", args.Version)
	}
	if args.OutputDir != "" {
		outPath, err := r.safePath(args.OutputDir)
		if err != nil {
			return Result{}, err
		}
		if err := os.MkdirAll(outPath, 0o755); err != nil {
			return Result{}, err
		}
		argv = append(argv, "--output-directory", outPath)
	}
	for _, src := range args.Source {
		argv = append(argv, "--source", src)
	}
	if args.ConfigFile != "" {
		configPath, err := r.safePath(args.ConfigFile)
		if err != nil {
			return Result{}, err
		}
		argv = append(argv, "--configfile", configPath)
	}
	if args.Prerelease {
		argv = append(argv, "--prerelease")
	}

	timeout := 120
	if args.TimeoutSeconds > 0 {
		timeout = args.TimeoutSeconds
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "dotnet", argv...)
	cmd.Dir = r.root
	out, err := runWithKill(cctx, cmd)
	res := Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("Installed NuGet package %s", args.Package),
		Content: truncate(string(out), 20000),
		Metadata: map[string]interface{}{
			"network": true,
		},
	}
	if err != nil {
		return res, err
	}
	return res, nil
}
