package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

func (f *fakeSetupRegistry) SetProgressFunc(_ model.ProgressFunc) {}

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

func TestSetupModelForProfile(t *testing.T) {
	tests := []struct {
		profile setupProfile
		want    string
	}{
		{setupProfileConsumer, consumerSetupModel},
		{setupProfileServer, serverSetupModel},
		{setupProfileGPUServer, gpuServerSetupModel},
	}
	for _, tt := range tests {
		if got := setupModelForProfile(tt.profile); got != tt.want {
			t.Errorf("setupModelForProfile(%q) = %q, want %q", tt.profile, got, tt.want)
		}
	}
}

func TestChooseSetupTargetConfirmsBeforeLocalDownload(t *testing.T) {
	reg := &fakeSetupRegistry{
		models:     []model.ModelInfo{{Name: consumerSetupModel, Size: "3.8 GB"}},
		downloaded: map[string]bool{},
	}
	reader := bufio.NewReader(strings.NewReader("1\ny\n"))
	var out, errOut bytes.Buffer
	target, err := chooseSetupTarget(reader, setupProfileConsumer, model.BackendLlamaCpp, consumerSetupModel, reg, &out, &errOut)
	if err != nil {
		t.Fatalf("chooseSetupTarget error: %v", err)
	}
	if !target.local || target.model != consumerSetupModel {
		t.Fatalf("target = %+v, want local default target", target)
	}
	if len(reg.calls) != 0 {
		t.Fatalf("model downloaded before confirmation: %v", reg.calls)
	}
	if !strings.Contains(out.String(), "Download "+consumerSetupModel+" now?") {
		t.Fatalf("missing download confirmation: %q", out.String())
	}
}

func TestChooseSetupTargetCanConfigureOpenRouter(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	reg := &fakeSetupRegistry{}
	reader := bufio.NewReader(strings.NewReader("3\n2\n\n\n"))
	var out, errOut bytes.Buffer
	target, err := chooseSetupTarget(reader, setupProfileServer, model.BackendLlamaCpp, serverSetupModel, reg, &out, &errOut)
	if err != nil {
		t.Fatalf("chooseSetupTarget error: %v", err)
	}
	if target.local || target.backend != model.BackendExternal {
		t.Fatalf("target = %+v, want hosted external target", target)
	}
	if target.baseURL != "https://openrouter.ai/api/v1" || target.tokenEnv != "OPENROUTER_API_KEY" {
		t.Fatalf("unexpected OpenRouter target: %+v", target)
	}
	if target.authType != "bearer" || target.model != "openai/gpt-oss-20b" {
		t.Fatalf("unexpected hosted auth/model: %+v", target)
	}
}

func TestHostedCredentialRecognizesProviderKey(t *testing.T) {
	envName, key := hostedCredential("gsk_example_key", "GROQ_API_KEY")
	if envName != "GROQ_API_KEY" || key != "gsk_example_key" {
		t.Fatalf("credential = (%q, %q)", envName, key)
	}
	envName, key = hostedCredential("TEAM_GROQ_KEY", "GROQ_API_KEY")
	if envName != "TEAM_GROQ_KEY" || key != "" {
		t.Fatalf("environment credential = (%q, %q)", envName, key)
	}
}

func TestWriteHostedSetupStoresOnlyTokenEnvironmentName(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	root := t.TempDir()
	err := writeHostedSetup(root, setupTarget{
		backend:  model.BackendExternal,
		baseURL:  "https://api.groq.com/openai/v1",
		model:    "llama-3.3-70b-versatile",
		authType: "bearer",
		tokenEnv: "GROQ_API_KEY",
	})
	if err != nil {
		t.Fatalf("writeHostedSetup error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".qodex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `auth = { type = "bearer", token_env = "GROQ_API_KEY"`) {
		t.Fatalf("missing hosted auth configuration:\n%s", text)
	}
	if !strings.Contains(text, `tool_calls = "native"`) {
		t.Fatalf("hosted setup must enable native tool calls:\n%s", text)
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("config unexpectedly contains a secret: %s", text)
	}
}

func TestDetectSetupProfileOverride(t *testing.T) {
	t.Setenv("QODEX_SETUP_PROFILE", "gpu-server")
	if got := detectSetupProfile(); got != setupProfileGPUServer {
		t.Fatalf("detectSetupProfile() = %q, want %q", got, setupProfileGPUServer)
	}

	t.Setenv("QODEX_SETUP_PROFILE", "desktop")
	if got := detectSetupProfile(); got != setupProfileConsumer {
		t.Fatalf("detectSetupProfile() = %q, want %q", got, setupProfileConsumer)
	}
}

func TestEnsureSetupModelDownloadsRecommendedModel(t *testing.T) {
	reg := &fakeSetupRegistry{
		models:     []model.ModelInfo{{Name: serverSetupModel, Size: "4.7 GB"}},
		downloaded: map[string]bool{},
	}
	var out, errOut bytes.Buffer
	ready, err := ensureSetupModel(context.Background(), reg, serverSetupModel, &out, &errOut)
	if err != nil {
		t.Fatalf("ensureSetupModel error: %v", err)
	}
	if !ready || len(reg.calls) != 1 || reg.calls[0] != serverSetupModel {
		t.Fatalf("result = ready:%v calls:%v, want downloaded %s", ready, reg.calls, serverSetupModel)
	}
	if !strings.Contains(out.String(), "Downloading recommended model") {
		t.Fatalf("expected automatic download message, got %q", out.String())
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

func TestSelectSetupModelReportsDownloadFailure(t *testing.T) {
	reg := &fakeSetupRegistry{
		models:      []model.ModelInfo{{Name: "first.gguf", Size: "1 GB"}},
		downloaded:  map[string]bool{},
		downloadErr: errors.New("download failed"),
	}
	reader := bufio.NewReader(strings.NewReader("1\ny\n"))
	var out bytes.Buffer
	var errOut bytes.Buffer

	name, ready, err := selectSetupModel(context.Background(), reader, reg, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("error = %v, want download failure", err)
	}
	if name != "first.gguf" || ready {
		t.Fatalf("result = (%q, %v), want (first.gguf, false)", name, ready)
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
	if !strings.Contains(text, `tool_calls = "prompt"`) {
		t.Fatalf("expected prompt tool mode for default local-style config:\n%s", text)
	}
}

func TestSelectRemoteModelUsesBackendModelID(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("org/model\n"))

	got := selectRemoteModel(reader, model.BackendVLLM)
	if got != "org/model" {
		t.Fatalf("model = %q, want org/model", got)
	}
}

func TestWriteAtomicReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("writeAtomic error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("content = %q, want new", data)
	}
}
