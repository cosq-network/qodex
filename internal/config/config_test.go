package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectConfigAndNormalizeStorePath(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	if err := os.MkdirAll(filepath.Join(root, ".locha"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := os.WriteFile(filepath.Join(root, ".locha", "config.toml"), []byte(`[model]
base_url = "http://127.0.0.1:9000/v1"
model = "qwen-test"

[store]
path = ".locha/test.db"
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model.BaseURL != "http://127.0.0.1:9000/v1" {
		t.Fatalf("base url = %q", cfg.Model.BaseURL)
	}
	wantStore := filepath.Join(root, ".locha", "test.db")
	gotEval, _ := filepath.EvalSymlinks(filepath.Dir(cfg.Store.Path))
	wantEval, _ := filepath.EvalSymlinks(filepath.Dir(wantStore))
	if gotEval != wantEval || filepath.Base(cfg.Store.Path) != filepath.Base(wantStore) {
		t.Fatalf("store path = %q, want %q", cfg.Store.Path, wantStore)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	if err := os.MkdirAll(filepath.Join(root, ".locha"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := os.WriteFile(filepath.Join(root, ".locha", "config.toml"), []byte(`[runtime]
temperature = 4
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "temperature") {
		t.Fatalf("expected temperature validation error, got %v", err)
	}
}

func TestLoadRejectsMalformedTOML(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	if err := os.MkdirAll(filepath.Join(root, ".locha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".locha", "config.toml"), []byte(`[runtime
temperature = 0.2
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	root := t.TempDir()
	restore := chdir(t, root)
	defer restore()

	if err := os.MkdirAll(filepath.Join(root, ".locha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".locha", "config.toml"), []byte(`[runtime]
temperature = 0.2
unknown = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "missing in the target struct") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestSetProjectValue(t *testing.T) {
	root := t.TempDir()
	if err := SetProjectValue(root, "runtime.temperature", "0.1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ProjectConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "temperature = 0.1") {
		t.Fatalf("config did not contain updated temperature:\n%s", string(data))
	}
}

func TestSetProjectValueRejectsUnknownKey(t *testing.T) {
	if err := SetProjectValue(t.TempDir(), "bad.key", "x"); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestDefaultsIncludeSkillRouting(t *testing.T) {
	cfg := Defaults("/test")
	if cfg.Agent.SkillRouting != "auto" {
		t.Fatalf("expected skill_routing=auto, got %q", cfg.Agent.SkillRouting)
	}
}

func TestValidateAcceptsSkillRoutingAuto(t *testing.T) {
	cfg := Defaults("/test")
	cfg.Agent.SkillRouting = "auto"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for auto, got %v", err)
	}
}

func TestValidateAcceptsSkillRoutingModel(t *testing.T) {
	cfg := Defaults("/test")
	cfg.Agent.SkillRouting = "model"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for model, got %v", err)
	}
}

func TestValidateRejectsInvalidSkillRouting(t *testing.T) {
	cfg := Defaults("/test")
	cfg.Agent.SkillRouting = "invalid"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "skill_routing") {
		t.Fatalf("expected skill_routing validation error, got %v", err)
	}
}

func TestValuesIncludeSkillRouting(t *testing.T) {
	cfg := Defaults("/test")
	vals := cfg.Values()
	if v, ok := vals["agent.skill_routing"]; !ok || v != "auto" {
		t.Fatalf("expected agent.skill_routing=auto in values, got %q", v)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(original); err != nil {
			t.Fatal(err)
		}
	}
}
