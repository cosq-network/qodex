package tools

import (
	"context"
	"encoding/json"
	"os"
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
