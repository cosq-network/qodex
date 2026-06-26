package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findGopls(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls not found, skipping LSP test")
	}
	return p
}

func initGoModule(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

func TestNewClient(t *testing.T) {
	gopls := findGopls(t)
	ctx := context.Background()

	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if c.cmd == nil {
		t.Fatal("expected cmd to be non-nil")
	}
}

func TestInitialize(t *testing.T) {
	gopls := findGopls(t)
	ctx := context.Background()

	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(t.TempDir()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
}

func TestDiagnostics(t *testing.T) {
	gopls := findGopls(t)
	dir := t.TempDir()
	initGoModule(t, dir)

	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func main() {
	var x int
	println(x)
	println(y)
}
`), 0o644)

	ctx := context.Background()
	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(dir); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.OpenDocument("main.go"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	diags, err := c.Diagnostics("main.go")
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}

	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic for undeclared name 'y'")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "y") && (strings.Contains(d.Message, "undeclared") || strings.Contains(d.Message, "declared") || strings.Contains(d.Message, "undefined")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected diagnostic about undeclared name 'y', got: %v", diags)
	}
}

func TestGotoDefinition(t *testing.T) {
	gopls := findGopls(t)
	dir := t.TempDir()
	initGoModule(t, dir)

	os.WriteFile(filepath.Join(dir, "greet.go"), []byte(`package main

func Greet(name string) string {
	return "Hello, " + name
}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func main() {
	Greet("world")
}
`), 0o644)

	ctx := context.Background()
	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(dir); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.OpenDocument("main.go"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	// "Greet" is at line 3 (0-based), character 2 (after tab)
	loc, err := c.GotoDefinition("main.go", 3, 2)
	if err != nil {
		t.Fatalf("GotoDefinition: %v", err)
	}
	if !strings.HasSuffix(loc.URI, "greet.go") {
		t.Fatalf("expected definition in greet.go, got URI: %s", loc.URI)
	}
}

func TestFindReferences(t *testing.T) {
	gopls := findGopls(t)
	dir := t.TempDir()
	initGoModule(t, dir)

	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func Hello() string {
	return "hi"
}

func main() {
	msg := Hello()
	println(msg)
}
`), 0o644)

	ctx := context.Background()
	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(dir); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.OpenDocument("main.go"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	// "Hello" is at line 2 (0-based), character 5 (after 'func ')
	refs, err := c.FindReferences("main.go", 2, 5)
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) < 2 {
		t.Fatalf("expected at least 2 references (definition + call), got %d: %v", len(refs), refs)
	}
}

func TestPathToURI(t *testing.T) {
	uri := pathToURI("/home/user/project")
	if uri != "file:///home/user/project" {
		t.Fatalf("expected file:///home/user/project, got %s", uri)
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"test.py", "python"},
		{"app.js", "javascript"},
		{"component.tsx", "typescript"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"Makefile", ""},
		{"README.md", ""},
	}
	for _, tc := range tests {
		got := detechLanguage(tc.path)
		if got != tc.want {
			t.Errorf("detectLanguage(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestOpenDocument(t *testing.T) {
	gopls := findGopls(t)
	dir := t.TempDir()
	initGoModule(t, dir)

	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\n"), 0o644)

	ctx := context.Background()
	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	if err := c.Initialize(dir); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.OpenDocument("foo.go"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	diags, err := c.Diagnostics("foo.go")
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if diags == nil {
		t.Fatal("expected non-nil diagnostics slice")
	}
}

func TestCloseMultiple(t *testing.T) {
	gopls := findGopls(t)
	ctx := context.Background()

	c, err := NewClient(ctx, gopls, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.Initialize(t.TempDir()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestServerNotInstalled(t *testing.T) {
	ctx := context.Background()
	_, err := NewClient(ctx, "nonexistent-lsp-server-12345", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent server")
	}
}
