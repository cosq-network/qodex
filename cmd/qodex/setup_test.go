package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
)

type fakeSetupRegistry struct {
	models      []model.ModelInfo
	downloaded  map[string]bool
	downloadErr error
	calls       []string
}

func (f *fakeSetupRegistry) List() ([]model.ModelInfo, error) {
	return f.models, nil
}

func (f *fakeSetupRegistry) Download(_ context.Context, name string) error {
	f.calls = append(f.calls, name)
	if f.downloadErr != nil {
		return f.downloadErr
	}
	if f.downloaded == nil {
		f.downloaded = map[string]bool{}
	}
	f.downloaded[name] = true
	return nil
}

func (f *fakeSetupRegistry) ModelsDir() string {
	return "/tmp/models"
}

func (f *fakeSetupRegistry) IsDownloaded(name string) bool {
	return f.downloaded[name]
}

func TestSelectSetupModelDownloadsChosenModel(t *testing.T) {
	reg := &fakeSetupRegistry{
		models: []model.ModelInfo{
			{Name: "first.gguf", Size: "1 GB"},
			{Name: "second.gguf", Size: "2 GB"},
		},
		downloaded: map[string]bool{},
	}
	reader := bufio.NewReader(strings.NewReader("2\ny\n"))
	var out bytes.Buffer
	var errOut bytes.Buffer

	name, ready, err := selectSetupModel(context.Background(), reader, reg, &out, &errOut)
	if err != nil {
		t.Fatalf("selectSetupModel error: %v", err)
	}
	if name != "second.gguf" {
		t.Fatalf("name = %q, want second.gguf", name)
	}
	if !ready {
		t.Fatal("expected model to be ready after download")
	}
	if len(reg.calls) != 1 || reg.calls[0] != "second.gguf" {
		t.Fatalf("download calls = %v", reg.calls)
	}
}

func TestSelectSetupModelAllowsManualContinuation(t *testing.T) {
	reg := &fakeSetupRegistry{
		models: []model.ModelInfo{
			{Name: "first.gguf", Size: "1 GB"},
		},
		downloaded: map[string]bool{},
	}
	reader := bufio.NewReader(strings.NewReader("1\nn\n"))
	var out bytes.Buffer
	var errOut bytes.Buffer

	name, ready, err := selectSetupModel(context.Background(), reader, reg, &out, &errOut)
	if err != nil {
		t.Fatalf("selectSetupModel error: %v", err)
	}
	if name != "first.gguf" {
		t.Fatalf("name = %q, want first.gguf", name)
	}
	if ready {
		t.Fatal("expected model to remain not ready when download is declined")
	}
	if len(reg.calls) != 0 {
		t.Fatalf("unexpected download calls: %v", reg.calls)
	}
	if !strings.Contains(errOut.String(), "Manual download required") {
		t.Fatalf("expected manual guidance, got %q", errOut.String())
	}
}

func TestWriteSetupFilesWritesExternalConfig(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults(root)
	cfg.Runtime.Backend = "external"
	cfg.Model.BaseURL = "https://example.test/v1"
	cfg.Model.Model = "remote-model"

	if err := writeSetupFiles(root, cfg); err != nil {
		t.Fatalf("writeSetupFiles error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".qodex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `backend = "external"`) {
		t.Fatalf("expected external backend in config:\n%s", text)
	}
	if !strings.Contains(text, `base_url = "https://example.test/v1"`) {
		t.Fatalf("expected base_url in config:\n%s", text)
	}
}
