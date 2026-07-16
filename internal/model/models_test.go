package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestModelRegistryListMarksDownloaded(t *testing.T) {
	root := t.TempDir()
	reg := NewModelRegistry(root)
	if err := os.MkdirAll(reg.ModelsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reg.ModelsDir(), "qwen2.5-coder-7b-q4_k_m.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	models, err := reg.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	var found bool
	for _, m := range models {
		if m.Name == "qwen2.5-coder-7b-q4_k_m.gguf" {
			found = true
			if !m.Downloaded {
				t.Fatal("expected model to be marked downloaded")
			}
		}
	}
	if !found {
		t.Fatal("expected default model in list")
	}
}

func TestModelRegistryDownload(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("gguf-data"))
	}))
	defer srv.Close()

	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "1 B", URL: srv.URL}}

	root := t.TempDir()
	reg := NewModelRegistry(root)
	if err := reg.Download(context.Background(), "test.gguf"); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(reg.ModelsDir(), "test.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "gguf-data" {
		t.Fatalf("downloaded data = %q", string(data))
	}
}

func TestModelRegistryIsDownloaded(t *testing.T) {
	root := t.TempDir()
	reg := NewModelRegistry(root)
	if reg.IsDownloaded("test") {
		t.Fatal("expected model to be absent")
	}
	if err := os.MkdirAll(reg.ModelsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reg.ModelsDir(), "test.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !reg.IsDownloaded("test") {
		t.Fatal("expected extensionless lookup to detect downloaded model")
	}
}
