package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		_, _ = w.Write([]byte("GGUF-data"))
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
	if string(data) != "GGUF-data" {
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

func rangeServer(t *testing.T, full []byte) (*httptest.Server, *int) {
	t.Helper()
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(full)))
			w.WriteHeader(http.StatusOK)
			return
		}
		gets++
		start := int64(0)
		if rng := r.Header.Get("Range"); rng != "" {
			if _, err := fmt.Sscanf(strings.TrimPrefix(rng, "bytes="), "%d-", &start); err != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
		}
		if start >= int64(len(full)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(full)-int(start)))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(full)-1, len(full)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(full[start:])
	}))
	return srv, &gets
}

func TestModelRegistryDownloadReportsProgress(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	full := append([]byte("GGUF"), []byte(strings.Repeat("x", 4096))...)
	srv, _ := rangeServer(t, full)
	defer srv.Close()
	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "4 KB", URL: srv.URL}}

	root := t.TempDir()
	reg := NewModelRegistry(root)
	var last, total int64
	reg.SetProgressFunc(func(d, t int64) {
		last, total = d, t
	})
	if err := reg.Download(context.Background(), "test.gguf"); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if total != int64(len(full)) {
		t.Fatalf("expected total %d, got %d", len(full), total)
	}
	if last != int64(len(full)) {
		t.Fatalf("expected final progress %d, got %d", len(full), last)
	}
	data, err := os.ReadFile(filepath.Join(reg.ModelsDir(), "test.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(full) {
		t.Fatalf("downloaded data = %q", string(data))
	}
}

func TestModelRegistryDownloadResumesPartialFile(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	full := append([]byte("GGUF"), []byte(strings.Repeat("y", 4096))...)
	srv, _ := rangeServer(t, full)
	defer srv.Close()
	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "4 KB", URL: srv.URL}}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "models", "test.gguf")
	if err := os.WriteFile(dest, []byte("GGUF"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewModelRegistry(root)
	if err := reg.Download(context.Background(), "test.gguf"); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(full) {
		t.Fatalf("resumed data = %q", string(data))
	}
}

func TestModelRegistryDownloadSkipsValidExisting(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	full := append([]byte("GGUF"), []byte(strings.Repeat("z", 1024))...)
	srv, gets := rangeServer(t, full)
	defer srv.Close()
	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "1 KB", URL: srv.URL}}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "models", "test.gguf")
	if err := os.WriteFile(dest, full, 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewModelRegistry(root)
	if err := reg.Download(context.Background(), "test.gguf"); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if *gets != 0 {
		t.Fatalf("expected no GET requests, got %d", *gets)
	}
}

func TestModelRegistryDownloadRejectsInvalidPayload(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-a-gguf"))
	}))
	defer srv.Close()
	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "1 B", URL: srv.URL}}

	root := t.TempDir()
	reg := NewModelRegistry(root)
	if err := reg.Download(context.Background(), "test.gguf"); err == nil {
		t.Fatal("expected download to fail for invalid GGUF payload")
	}
	if _, err := os.Stat(filepath.Join(reg.ModelsDir(), "test.gguf")); !os.IsNotExist(err) {
		t.Fatalf("expected corrupt file to be removed, stat err = %v", err)
	}
}

func TestModelRegistryDownloadRestartsWhenServerLacksRange(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	full := append([]byte("GGUF"), []byte(strings.Repeat("v", 2048))...)
	gets := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(full)))
			w.WriteHeader(http.StatusOK)
			return
		}
		gets++
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(full)))
		_, _ = w.Write(full)
	}))
	defer srv.Close()
	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "2 KB", URL: srv.URL}}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "models", "test.gguf")
	if err := os.WriteFile(dest, []byte("GGUFpartial"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewModelRegistry(root)
	if err := reg.Download(context.Background(), "test.gguf"); err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if gets != 1 {
		t.Fatalf("expected exactly one GET request, got %d", gets)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(full) {
		t.Fatalf("restarted data = %q", string(data))
	}
}

func TestModelRegistryDownloadRejectsCorruptResume(t *testing.T) {
	orig := defaultModels
	defer func() { defaultModels = orig }()

	full := append([]byte("GGUF"), []byte(strings.Repeat("w", 1024))...)
	srv, _ := rangeServer(t, full)
	defer srv.Close()
	defaultModels = []ModelInfo{{Name: "test.gguf", Size: "1 KB", URL: srv.URL}}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "models", "test.gguf")
	if err := os.WriteFile(dest, []byte("XXXX"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewModelRegistry(root)
	if err := reg.Download(context.Background(), "test.gguf"); err == nil {
		t.Fatal("expected download to fail when partial file is corrupt")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("expected corrupt partial file to be removed, stat err = %v", err)
	}
}

func TestVerifyGGUF(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.gguf")
	if err := os.WriteFile(valid, []byte("GGUFxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGGUF(valid); err != nil {
		t.Fatalf("expected valid GGUF to pass: %v", err)
	}

	invalid := filepath.Join(dir, "invalid.gguf")
	if err := os.WriteFile(invalid, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGGUF(invalid); err == nil {
		t.Fatal("expected invalid GGUF to fail")
	}

	if err := VerifyGGUF(filepath.Join(dir, "missing.gguf")); err == nil {
		t.Fatal("expected missing file to fail")
	}
}
