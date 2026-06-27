package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePathRejectsEscape(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	if _, err := registry.safePath("../outside.txt"); err == nil {
		t.Fatal("expected escape to be rejected")
	}
}

func TestWritePatch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(root)
	tool, ok := registry.Get("write_patch")
	if !ok {
		t.Fatal("write_patch not registered")
	}
	args, _ := json.Marshal(map[string]string{
		"patch": strings.Join([]string{
			"diff --git a/hello.txt b/hello.txt",
			"index ce01362..cc628cc 100644",
			"--- a/hello.txt",
			"+++ b/hello.txt",
			"@@ -1 +1 @@",
			"-hello",
			"+hello world",
			"",
		}, "\n"),
	})
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("write_patch error: %v: %s", err, res.Content)
	}
	data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\n" {
		t.Fatalf("content = %q", string(data))
	}
}

func TestValidatePatchPathsRejectsEscape(t *testing.T) {
	err := validatePatchPaths("--- a/../x\n+++ b/../x\n")
	if err == nil {
		t.Fatal("expected path validation error")
	}
}

func TestRunCommandArgv(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("run_command")
	if !ok {
		t.Fatal("run_command not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{
		"argv": []string{"go", "version"},
	})
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("run_command error: %v", err)
	}
	if !res.OK {
		t.Fatalf("result not ok: %#v", res)
	}
	if res.Metadata["shell"] != false {
		t.Fatalf("expected direct execution metadata, got %#v", res.Metadata)
	}
}

func TestRunCommandArgvRejectsDangerousCommand(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("run_command")
	if !ok {
		t.Fatal("run_command not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{
		"argv": []string{"rm", "-rf", "/"},
	})
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected dangerous argv rejection")
	}
}

func TestIsNetworkCommand(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{
		"argv": []string{"curl", "-I", "https://example.com"},
	})
	if !IsNetworkCommand(args) {
		t.Fatal("expected curl to be classified as network")
	}
	args, _ = json.Marshal(map[string]interface{}{
		"argv": []string{"go", "test", "./..."},
	})
	if IsNetworkCommand(args) {
		t.Fatal("did not expect go test to be classified as network")
	}
}

func TestRejectDangerousShellCommand(t *testing.T) {
	if err := rejectDangerousShellCommand("sudo rm -rf /tmp/example"); err == nil {
		t.Fatal("expected dangerous command rejection")
	}
}

func TestRunScriptRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("run_script")
	if !ok {
		t.Fatal("run_script not registered")
	}
	if tool.Description == "" {
		t.Fatal("run_script has no description")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestGitLogRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("git_log")
	if !ok {
		t.Fatal("git_log not registered")
	}
	if tool.Description == "" {
		t.Fatal("git_log has no description")
	}
	if tool.Effect != "read" {
		t.Fatalf("expected effect read, got %s", tool.Effect)
	}
}

func TestGitLogInRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", root, "config", "user.email", "test@test.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config email failed: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", root, "config", "user.name", "Test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config name failed: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", root, "add", "README.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", root, "commit", "-m", "initial")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v: %s", err, out)
	}

	registry := NewRegistry(root)
	tool, ok := registry.Get("git_log")
	if !ok {
		t.Fatal("git_log not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{"limit": 5, "oneline": true})
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("git_log error: %v", err)
	}
	if !res.OK {
		t.Fatalf("git_log not ok: %s", res.Content)
	}
	if !strings.Contains(res.Content, "initial") {
		t.Fatalf("expected commit message in log output, got: %s", res.Content)
	}
}

func TestCmakeToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"cmake_configure", "cmake_build"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Effect != "shell" {
			t.Fatalf("expected effect shell for %s, got %s", name, tool.Effect)
		}
	}
}

func TestClangToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"clang_format", "clang_tidy"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Effect != "shell" {
			t.Fatalf("expected effect shell for %s, got %s", name, tool.Effect)
		}
	}
}

func TestMakeBuildRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("make_build")
	if !ok {
		t.Fatal("make_build not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestJavaToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"javac_compile", "java_run"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Effect != "shell" {
			t.Fatalf("expected effect shell for %s, got %s", name, tool.Effect)
		}
	}
}

func TestJavacCompileRequiresSource(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("javac_compile")
	if !ok {
		t.Fatal("javac_compile not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestJavaRunRequiresMainOrJar(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("java_run")
	if !ok {
		t.Fatal("java_run not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing main_class or jar")
	}
}

func TestNewToolSchemasHaveProperties(t *testing.T) {
	r := NewRegistry(t.TempDir())
	for _, name := range []string{"git_log", "cmake_configure", "cmake_build", "clang_format", "clang_tidy", "make_build", "curl", "wget", "javac_compile", "java_run", "rg_search", "sed_edit", "base64_encode", "node_run", "npm_command", "npx_command", "nvm_use", "dotnet_run", "dotnet_build", "dotnet_test", "msbuild", "nuget_restore", "nuget_install", "nmake_build", "winget_install", "choco_install", "apt_install", "apt_get_install", "snap_install", "dnf_install", "brew_install", "python_run", "python3_run", "pip_install", "pip3_install", "conda_install", "conda_create", "flutter_run", "flutter_build", "flutter_test", "dart_run", "dart_analyze", "dart_format", "pub_get", "pub_upgrade", "pub_add", "pub_remove", "ar_create", "ar_extract", "ar_list", "tar_create", "tar_extract", "tar_list", "zip_create", "zip_extract", "zip_list", "grep_search", "find_files", "tail_file", "awk_process", "ps_list", "chmod_change", "chown_change", "user_add", "user_del"} {
		tool, ok := r.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		var params map[string]interface{}
		if err := json.Unmarshal(tool.Parameters, &params); err != nil {
			t.Fatalf("invalid schema for %s: %v", name, err)
		}
		if params["type"] != "object" {
			t.Fatalf("expected type=object for %s", name)
		}
		props, ok := params["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected properties map for %s", name)
		}
		if len(props) == 0 {
			t.Fatalf("expected non-empty properties for %s", name)
		}
		required, ok := params["required"].([]interface{})
		if !ok {
			t.Fatalf("expected required array for %s", name)
		}
		if name == "curl" || name == "wget" {
			foundUrl := false
			for _, r := range required {
				if r == "url" {
					foundUrl = true
					break
				}
			}
			if !foundUrl {
				t.Fatalf("expected url required for %s", name)
			}
		}
		if name == "javac_compile" {
			foundSource := false
			for _, r := range required {
				if r == "source" {
					foundSource = true
					break
				}
			}
			if !foundSource {
				t.Fatalf("expected source required for %s", name)
			}
		}
		if name == "nuget_install" {
			foundPackage := false
			for _, r := range required {
				if r == "package" {
					foundPackage = true
					break
				}
			}
			if !foundPackage {
				t.Fatalf("expected package required for %s", name)
			}
		}
		if name == "winget_install" || name == "choco_install" || name == "apt_install" || name == "apt_get_install" || name == "snap_install" || name == "dnf_install" || name == "brew_install" {
			foundPackage := false
			for _, r := range required {
				if r == "package" {
					foundPackage = true
					break
				}
			}
			if !foundPackage {
				t.Fatalf("expected package required for %s", name)
			}
		}
	}
}

func TestCurlRejectsDangerousScheme(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("curl")
	if !ok {
		t.Fatal("curl not registered")
	}
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com", "gopher://example.com"} {
		args, _ := json.Marshal(map[string]interface{}{"url": url})
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatalf("expected error for dangerous scheme %s", url)
		}
	}
}

func TestWgetRejectsDangerousScheme(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("wget")
	if !ok {
		t.Fatal("wget not registered")
	}
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com", "ldap://example.com"} {
		args, _ := json.Marshal(map[string]interface{}{"url": url})
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatalf("expected error for dangerous scheme %s", url)
		}
	}
}

func TestRgSearchRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("rg_search")
	if !ok {
		t.Fatal("rg_search not registered")
	}
	if tool.Effect != "read" {
		t.Fatalf("expected effect read, got %s", tool.Effect)
	}
}

func TestSedEditRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("sed_edit")
	if !ok {
		t.Fatal("sed_edit not registered")
	}
	if tool.Effect != "write" {
		t.Fatalf("expected effect write, got %s", tool.Effect)
	}
}

func TestBase64EncodeRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("base64_encode")
	if !ok {
		t.Fatal("base64_encode not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestNodeRunRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("node_run")
	if !ok {
		t.Fatal("node_run not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestNpmCommandRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("npm_command")
	if !ok {
		t.Fatal("npm_command not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestNpxCommandRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("npx_command")
	if !ok {
		t.Fatal("npx_command not registered")
	}
	if tool.Effect != "network" {
		t.Fatalf("expected effect network, got %s", tool.Effect)
	}
}

func TestNvmUseRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("nvm_use")
	if !ok {
		t.Fatal("nvm_use not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestIsNpmCommandNetwork(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]interface{}
		want bool
	}{
		{"install", map[string]interface{}{"command": "install"}, true},
		{"i", map[string]interface{}{"command": "i"}, true},
		{"ci", map[string]interface{}{"command": "ci"}, true},
		{"publish", map[string]interface{}{"command": "publish"}, true},
		{"run build", map[string]interface{}{"command": "run", "script": "build"}, true},
		{"test", map[string]interface{}{"command": "test"}, false},
		{"run build no script", map[string]interface{}{"command": "build"}, false},
	}
	for _, c := range cases {
		args, _ := json.Marshal(c.raw)
		got := IsNpmCommandNetwork(args)
		if got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsNpxCommandNetwork(t *testing.T) {
	args1, _ := json.Marshal(map[string]interface{}{"package": "eslint"})
	if !IsNpxCommandNetwork(args1) {
		t.Fatal("expected npx with package to be network")
	}
	args2, _ := json.Marshal(map[string]interface{}{})
	if IsNpxCommandNetwork(args2) {
		t.Fatal("expected empty npx to not be network")
	}
}

func TestDotnetToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"dotnet_run", "dotnet_build", "dotnet_test", "msbuild", "nuget_restore", "nuget_install"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Description == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	runTool, _ := registry.Get("dotnet_run")
	if runTool.Effect != "shell" {
		t.Fatal("expected dotnet_run effect shell")
	}
	restoreTool, _ := registry.Get("nuget_restore")
	if restoreTool.Effect != "network" {
		t.Fatal("expected nuget_restore effect network")
	}
	installTool, _ := registry.Get("nuget_install")
	if installTool.Effect != "network" {
		t.Fatal("expected nuget_install effect network")
	}
}

func TestMsbuildRequiresProject(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("msbuild")
	if !ok {
		t.Fatal("msbuild not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
}

func TestNugetInstallRequiresPackage(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("nuget_install")
	if !ok {
		t.Fatal("nuget_install not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing package")
	}
}

func TestNmakeBuildRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("nmake_build")
	if !ok {
		t.Fatal("nmake_build not registered")
	}
	if tool.Description == "" {
		t.Fatal("nmake_build has no description")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestPackageManagersRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	expected := map[string]string{
		"winget_install":    "network",
		"choco_install":     "network",
		"apt_install":       "shell",
		"apt_get_install":   "shell",
		"snap_install":      "network",
		"dnf_install":       "network",
		"brew_install":      "network",
	}
	for name, effect := range expected {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Description == "" {
			t.Fatalf("%s has no description", name)
		}
		if tool.Effect != effect {
			t.Fatalf("expected effect %s for %s, got %s", effect, name, tool.Effect)
		}
	}
}

func TestPackageManagerRequiresPackage(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"winget_install", "choco_install", "apt_install", "apt_get_install", "snap_install", "dnf_install", "brew_install"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		args, _ := json.Marshal(map[string]interface{}{})
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatalf("expected error for missing package in %s", name)
		}
	}
}

func TestPythonToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"python_run", "python3_run", "pip_install", "pip3_install", "conda_install", "conda_create"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Description == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	runTool, _ := registry.Get("python_run")
	if runTool.Effect != "shell" {
		t.Fatal("expected python_run effect shell")
	}
	pipTool, _ := registry.Get("pip_install")
	if pipTool.Effect != "network" {
		t.Fatal("expected pip_install effect network")
	}
	condaTool, _ := registry.Get("conda_install")
	if condaTool.Effect != "network" {
		t.Fatal("expected conda_install effect network")
	}
}

func TestPythonRunRequiresScriptOrEval(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("python_run")
	if !ok {
		t.Fatal("python_run not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing script or eval")
	}
}

func TestPipInstallRequiresPackageOrRequirements(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("pip_install")
	if !ok {
		t.Fatal("pip_install not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing package or requirements")
	}
}

func TestCondaInstallRequiresPackage(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("conda_install")
	if !ok {
		t.Fatal("conda_install not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing package")
	}
}

func TestCondaCreateRequiresName(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("conda_create")
	if !ok {
		t.Fatal("conda_create not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestFlutterToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"flutter_run", "flutter_build", "flutter_test", "pub_get", "pub_upgrade", "pub_add", "pub_remove"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Description == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	runTool, _ := registry.Get("flutter_run")
	if runTool.Effect != "shell" {
		t.Fatal("expected flutter_run effect shell")
	}
	pubTool, _ := registry.Get("pub_get")
	if pubTool.Effect != "network" {
		t.Fatal("expected pub_get effect network")
	}
}

func TestDartToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"dart_run", "dart_analyze", "dart_format"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Description == "" {
			t.Fatalf("%s has no description", name)
		}
		if tool.Effect != "shell" {
			t.Fatalf("expected effect shell for %s, got %s", name, tool.Effect)
		}
	}
}

func TestPubAddRequiresName(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("pub_add")
	if !ok {
		t.Fatal("pub_add not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestPubRemoveRequiresName(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("pub_remove")
	if !ok {
		t.Fatal("pub_remove not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestDartRunRequiresScript(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("dart_run")
	if !ok {
		t.Fatal("dart_run not registered")
	}
	args, _ := json.Marshal(map[string]interface{}{})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for missing script")
	}
}

func TestArchiveToolsRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"ar_create", "ar_extract", "ar_list", "tar_create", "tar_extract", "tar_list", "zip_create", "zip_extract", "zip_list"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if tool.Description == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	createTool, _ := registry.Get("ar_create")
	if createTool.Effect != "shell" {
		t.Fatal("expected ar_create effect shell")
	}
	listTool, _ := registry.Get("ar_list")
	if listTool.Effect != "read" {
		t.Fatal("expected ar_list effect read")
	}
}

func TestArchiveToolsRequireArchive(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	for _, name := range []string{"ar_create", "ar_extract", "ar_list", "tar_create", "tar_extract", "tar_list", "zip_create", "zip_extract", "zip_list"} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		args, _ := json.Marshal(map[string]interface{}{})
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatalf("expected error for missing archive in %s", name)
		}
	}
}

func TestGrepSearchRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("grep_search")
	if !ok {
		t.Fatal("grep_search not registered")
	}
	if tool.Effect != "read" {
		t.Fatalf("expected effect read, got %s", tool.Effect)
	}
}

func TestFindFilesRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("find_files")
	if !ok {
		t.Fatal("find_files not registered")
	}
	if tool.Effect != "read" {
		t.Fatalf("expected effect read, got %s", tool.Effect)
	}
}

func TestTailFileRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("tail_file")
	if !ok {
		t.Fatal("tail_file not registered")
	}
	if tool.Effect != "read" {
		t.Fatalf("expected effect read, got %s", tool.Effect)
	}
}

func TestAwkProcessRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("awk_process")
	if !ok {
		t.Fatal("awk_process not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestPsListRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("ps_list")
	if !ok {
		t.Fatal("ps_list not registered")
	}
	if tool.Effect != "read" {
		t.Fatalf("expected effect read, got %s", tool.Effect)
	}
}

func TestChmodChangeRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("chmod_change")
	if !ok {
		t.Fatal("chmod_change not registered")
	}
	if tool.Effect != "shell" {
		t.Fatalf("expected effect shell, got %s", tool.Effect)
	}
}

func TestChownChangeRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("chown_change")
	if !ok {
		t.Fatal("chown_change not registered")
	}
	if tool.Effect != "destructive" {
		t.Fatalf("expected effect destructive, got %s", tool.Effect)
	}
}

func TestUserAddRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("user_add")
	if !ok {
		t.Fatal("user_add not registered")
	}
	if tool.Effect != "destructive" {
		t.Fatalf("expected effect destructive, got %s", tool.Effect)
	}
}

func TestUserDelRegistered(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	tool, ok := registry.Get("user_del")
	if !ok {
		t.Fatal("user_del not registered")
	}
	if tool.Effect != "destructive" {
		t.Fatalf("expected effect destructive, got %s", tool.Effect)
	}
}

func TestSystemToolsRequireFields(t *testing.T) {
	registry := NewRegistry(t.TempDir())
	cases := []struct {
		name string
		toolName string
		args map[string]interface{}
	}{
		{"grep_pattern", "grep_search", map[string]interface{}{}},
		{"tail_path", "tail_file", map[string]interface{}{}},
		{"awk_script", "awk_process", map[string]interface{}{}},
		{"chmod_mode", "chmod_change", map[string]interface{}{"path": "/tmp"}},
		{"chown_owner", "chown_change", map[string]interface{}{"path": "/tmp"}},
		{"useradd_name", "user_add", map[string]interface{}{}},
		{"userdel_name", "user_del", map[string]interface{}{}},
	}
	for _, c := range cases {
		tool, ok := registry.Get(c.toolName)
		if !ok {
			t.Fatalf("%s not registered", c.toolName)
		}
		args, _ := json.Marshal(c.args)
		_, err := tool.Execute(context.Background(), args)
		if err == nil {
			t.Fatalf("expected error for missing required field in %s", c.name)
		}
	}
}
