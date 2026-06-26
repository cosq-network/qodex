package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProjectIndexQueryFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(root, "util.py"), []byte("def foo():\n    pass\n"), 0o644)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# Project\n"), 0o644)

	idx := NewProjectIndex(root)
	files := idx.QueryFiles("go", "", 100)
	if len(files) != 1 || files[0].Path != "main.go" {
		t.Fatalf("expected 1 go file, got %v", files)
	}
}

func TestProjectIndexQuerySymbols(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "example.go"), []byte("package main\nfunc Hello() {}\ntype User struct{}\n"), 0o644)

	idx := NewProjectIndex(root)
	syms := idx.QuerySymbols("Hello", "", "", 10)
	if len(syms) != 1 || syms[0].Name != "Hello" || syms[0].Kind != "function" {
		t.Fatalf("expected function Hello, got %v", syms)
	}
	syms = idx.QuerySymbols("User", "", "", 10)
	if len(syms) != 1 || syms[0].Name != "User" || syms[0].Kind != "type" {
		t.Fatalf("expected type User, got %v", syms)
	}
}

func TestProjectIndexSummary(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(root, "b.py"), []byte("# b\n"), 0o644)

	idx := NewProjectIndex(root)
	summary := idx.Summary()
	if !strings.Contains(summary, "2 files") {
		t.Fatalf("expected 2 files in summary, got: %s", summary)
	}
}

func TestRebuildIfStaleNotStale(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	idx := NewProjectIndex(root)

	// Should have 1 file initially
	if len(idx.files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(idx.files))
	}

	// Add a new file but don't rebuild
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644)

	// RebuildIfStale with a generous maxAge — should not rebuild
	idx.RebuildIfStale(root, time.Hour)
	if len(idx.files) != 1 {
		t.Fatalf("expected still 1 file (not stale), got %d", len(idx.files))
	}
}

func TestRebuildIfStaleForceRebuild(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	idx := NewProjectIndex(root)

	// Add a new file
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644)

	// RebuildIfStale with 0 maxAge — forces rebuild
	idx.RebuildIfStale(root, 0)
	if len(idx.files) != 2 {
		t.Fatalf("expected 2 files after rebuild, got %d", len(idx.files))
	}
}

func TestProjectIndexConcurrentAccess(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644)
	os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o644)

	r := NewRegistry(root)
	// Initialize the index
	r.mu.Lock()
	r.index = NewProjectIndex(root)
	r.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.mu.Lock()
			_ = r.index
			r.mu.Unlock()
		}()
	}
	wg.Wait()
}

func TestProjectIndexJavaSymbols(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "Example.java"), []byte("public class Example {}\nclass Internal {}\nprivate record Data(int x) {}\n"), 0o644)

	idx := NewProjectIndex(root)

	syms := idx.QuerySymbols("Example", "", "", 10)
	if len(syms) != 1 || syms[0].Name != "Example" || syms[0].Kind != "class" {
		t.Fatalf("expected class Example, got %v", syms)
	}

	syms = idx.QuerySymbols("Internal", "", "", 10)
	if len(syms) != 1 || syms[0].Name != "Internal" || syms[0].Kind != "class" {
		t.Fatalf("expected class Internal, got %v", syms)
	}

	syms = idx.QuerySymbols("Data", "", "", 10)
	if len(syms) != 1 || syms[0].Name != "Data" || syms[0].Kind != "record" {
		t.Fatalf("expected record Data, got %v", syms)
	}
}

func TestToolSchemas(t *testing.T) {
	r := NewRegistry(t.TempDir())
	schemas := r.ToolSchemas()
	if len(schemas) == 0 {
		t.Fatal("expected at least one tool schema")
	}
	names := make(map[string]bool)
	for _, s := range schemas {
		if s.Type != "function" {
			t.Fatalf("schema type = %q, want function", s.Type)
		}
		if s.Function.Name == "" {
			t.Fatal("expected non-empty name")
		}
		if s.Function.Description == "" {
			t.Fatalf("expected non-empty description for %s", s.Function.Name)
		}
		if s.Function.Parameters == nil {
			t.Fatalf("expected parameters for %s", s.Function.Name)
		}
		names[s.Function.Name] = true
	}
	if !names["list_files"] {
		t.Fatal("expected list_files in tool schemas")
	}
}
