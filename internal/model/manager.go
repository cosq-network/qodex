package model

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Backend string

const (
	BackendLlamaCpp Backend = "llama.cpp"
	BackendVLLM     Backend = "vllm"
	BackendSGLang   Backend = "sglang"
	BackendExternal Backend = "external"
)

type ServerStatus struct {
	Running bool
	PID     int
	Port    int
	Model   string
	Error   string
}

type Diagnostics struct {
	Backend          Backend
	InstallRoot      string
	BinaryPath       string
	BackendInstalled bool
	ModelName        string
	ModelPath        string
	ModelPresent     bool
	Server           ServerStatus
}

type RuntimeState struct {
	Backend   string `json:"backend"`
	Model     string `json:"model"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	Endpoint  string `json:"endpoint"`
	UpdatedAt string `json:"updated_at"`
}

type Manager struct {
	backend Backend
	root    string
	port    int
	model   string
	client  *Client
}

func NewManager(backend Backend, installRoot, model string, port int) *Manager {
	if port <= 0 {
		port = defaultPort(backend)
	}
	return &Manager{
		backend: backend,
		root:    installRoot,
		model:   model,
		port:    port,
		client:  NewClient(fmt.Sprintf("http://127.0.0.1:%d/v1", port), model),
	}
}

func defaultPort(backend Backend) int {
	switch backend {
	case BackendLlamaCpp:
		return 8080
	case BackendExternal:
		return 0
	default:
		return 8000
	}
}

func (m *Manager) Port() int {
	return m.port
}

func (m *Manager) Client() *Client {
	return m.client
}

func (m *Manager) setPort(port int) {
	if port <= 0 {
		return
	}
	m.port = port
	m.client = NewClient(fmt.Sprintf("http://127.0.0.1:%d/v1", port), m.model)
}

func (m *Manager) Install(ctx context.Context) error {
	switch m.backend {
	case BackendLlamaCpp:
		return m.installLlamaCpp(ctx)
	case BackendVLLM:
		return m.installVLLM()
	case BackendSGLang:
		return m.installSGLang()
	case BackendExternal:
		return nil
	default:
		return fmt.Errorf("unknown backend: %s", m.backend)
	}
}

func (m *Manager) installLlamaCpp(ctx context.Context) error {
	binDir := filepath.Join(m.root, "bin")
	if _, err := os.Stat(filepath.Join(binDir, "llama-server")); err == nil {
		return nil
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	downloadURL := m.llamaDownloadURL()
	if downloadURL == "" {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("automatic llama.cpp setup is not supported on native Windows yet; use WSL2 or point Qodex at a manually managed OpenAI-compatible endpoint")
		}
		return fmt.Errorf("unsupported platform for automatic llama.cpp setup: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
	}

	tmpFile, err := os.CreateTemp("", "llama-server-*.tar.gz")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "qodex-setup")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	tmpFile.Close()

	if err := m.extractTar(tmpFile.Name(), binDir); err != nil {
		return fmt.Errorf("extract binary: %w", err)
	}

	binPerm := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		binPerm = 0o666
	}
	return os.Chmod(filepath.Join(binDir, "llama-server"), binPerm)
}

func (m *Manager) llamaDownloadURL() string {
	version := m.getLatestVersion()
	if version == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "https://github.com/ggml-org/llama.cpp/releases/download/" + version + "/llama-" + version + "-bin-macos-arm64.tar.gz"
		default:
			return "https://github.com/ggml-org/llama.cpp/releases/download/" + version + "/llama-" + version + "-bin-macos-x64.tar.gz"
		}
	case "linux":
		return "https://github.com/ggml-org/llama.cpp/releases/download/" + version + "/llama-" + version + "-bin-ubuntu-" + runtime.GOARCH + ".tar.gz"
	default:
		return ""
	}
}

func (m *Manager) getLatestVersion() string {
	resp, err := http.Get("https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=1")
	if err != nil {
		return "b9821"
	}
	defer resp.Body.Close()
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil || len(releases) == 0 {
		return "b9821"
	}
	return releases[0].TagName
}

func (m *Manager) extractTar(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		parts := strings.SplitN(header.Name, "/", 2)
		var rel string
		if len(parts) == 2 {
			rel = parts[1]
		} else {
			rel = header.Name
		}

		target := filepath.Join(destDir, rel)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			dst, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(dst, tr); err != nil {
				dst.Close()
				return err
			}
			dst.Close()
		case tar.TypeSymlink:
			if runtime.GOOS == "windows" {
				break
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			linkTarget := filepath.Join(destDir, header.Linkname)
			if err := os.Symlink(linkTarget, target); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) installVLLM() error {
	if _, err := exec.LookPath("vllm"); err == nil {
		return nil
	}
	if _, err := exec.LookPath("pip"); err != nil {
		return fmt.Errorf("pip not found - install Python 3.8+ with pip first")
	}
	cmd := exec.Command("pip", "install", "-q", "vllm", "fastapi", "uvicorn")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) installSGLang() error {
	if _, err := exec.LookPath("sglang"); err == nil {
		return nil
	}
	cmd := exec.Command("pip", "install", "-q", "sglang", "openai")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) binaryPath() string {
	return filepath.Join(m.root, "bin", "llama-server")
}

func (m *Manager) pidFile() string {
	return filepath.Join(m.root, "run", "server.pid")
}

func (m *Manager) stateFile() string {
	return filepath.Join(m.root, "run", "state.json")
}

func (m *Manager) modelsDir() string {
	return filepath.Join(m.root, "models")
}

func (m *Manager) Status(ctx context.Context) (ServerStatus, error) {
	m.applySavedState()
	status := ServerStatus{Port: m.port, Model: m.model}

	if data, err := os.ReadFile(m.pidFile()); err == nil {
		var pid int
		if _, perr := fmt.Sscanf(string(data), "%d", &pid); perr == nil {
			if p, err := os.FindProcess(pid); err == nil && p != nil {
				status.Running = true
				status.PID = pid
			}
		} else {
			status.Error = "invalid pid file"
		}
	}

	if status.Running {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := m.client.Check(checkCtx); err != nil {
			status.Running = false
			status.Error = err.Error()
		}
	}

	return status, nil
}

func (m *Manager) Diagnostics(ctx context.Context) Diagnostics {
	m.applySavedState()
	diag := Diagnostics{
		Backend:     m.backend,
		InstallRoot: m.root,
		ModelName:   m.model,
		Server:      ServerStatus{Port: m.port, Model: m.model},
	}

	switch m.backend {
	case BackendLlamaCpp:
		diag.BinaryPath = m.binaryPath()
		_, err := os.Stat(diag.BinaryPath)
		diag.BackendInstalled = err == nil
	case BackendVLLM:
		if path, err := exec.LookPath("vllm"); err == nil {
			diag.BinaryPath = path
			diag.BackendInstalled = true
		}
	case BackendSGLang:
		if path, err := exec.LookPath("sglang"); err == nil {
			diag.BinaryPath = path
			diag.BackendInstalled = true
		}
	case BackendExternal:
		diag.BackendInstalled = true
	}

	diag.ModelPath = m.findModel()
	diag.ModelPresent = diag.ModelPath != ""
	if status, err := m.Status(ctx); err == nil {
		diag.Server = status
	} else {
		diag.Server.Error = err.Error()
	}

	return diag
}

func (m *Manager) EnsureRunning(ctx context.Context) error {
	m.applySavedState()
	status, _ := m.Status(ctx)
	if status.Running {
		fmt.Println("Model server already running")
		return nil
	}

	if err := m.Install(ctx); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	switch m.backend {
	case BackendLlamaCpp:
		return m.startLlamaCpp()
	case BackendVLLM:
		return m.startVLLM(ctx)
	case BackendSGLang:
		return m.startSGLang(ctx)
	case BackendExternal:
		return nil
	default:
		return fmt.Errorf("unknown backend: %s", m.backend)
	}
}

func (m *Manager) startLlamaCpp() error {
	modelPath := m.findModel()
	if modelPath == "" {
		return fmt.Errorf("no model found - download with: qodex models download %s", m.model)
	}
	if err := m.ensureUsablePort(); err != nil {
		return err
	}

	runDir := filepath.Join(m.root, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}

	cmd := exec.Command(m.binaryPath(),
		"-m", modelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", m.port),
		"--ctx-size", "32768",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	if err := os.WriteFile(m.pidFile(), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0o666); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	_ = m.saveState(cmd.Process.Pid)

	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		status, _ := m.Status(context.Background())
		if status.Running {
			_ = m.saveState(cmd.Process.Pid)
			fmt.Printf("Model server started (PID: %d, Port: %d)\n", cmd.Process.Pid, m.port)
			return nil
		}
	}
	return fmt.Errorf("server failed to start within 15 seconds")
}

func (m *Manager) findModel() string {
	modelsDir := m.modelsDir()

	if _, err := os.Stat(filepath.Join(modelsDir, m.model)); err == nil {
		return filepath.Join(modelsDir, m.model)
	}

	exts := []string{".gguf", ".bin"}
	for _, ext := range exts {
		candidate := filepath.Join(modelsDir, m.model+ext)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	entries, _ := os.ReadDir(modelsDir)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".gguf") || strings.HasSuffix(entry.Name(), ".bin") {
			return filepath.Join(modelsDir, entry.Name())
		}
	}
	return ""
}

func (m *Manager) startVLLM(ctx context.Context) error {
	modelPath := m.findModel()
	if modelPath == "" {
		return fmt.Errorf("no model found - download with: qodex models download %s", m.model)
	}
	if err := m.ensureUsablePort(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "python", "-m", "vllm.entrypoints.openai.api_server",
		"--model", modelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", m.port),
	)
	setProcessGroup(cmd)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	_ = m.saveState(cmd.Process.Pid)

	fmt.Printf("Model server starting (Port: %d)...\n", m.port)
	return nil
}

func (m *Manager) startSGLang(ctx context.Context) error {
	modelPath := m.findModel()
	if modelPath == "" {
		return fmt.Errorf("no model found - download with: qodex models download %s", m.model)
	}
	if err := m.ensureUsablePort(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "python", "-m", "sglang.launch_server",
		"--model-path", modelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", m.port),
	)
	setProcessGroup(cmd)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	_ = m.saveState(cmd.Process.Pid)

	fmt.Printf("Model server starting (Port: %d)...\n", m.port)
	return nil
}

func (m *Manager) InstallRoot() string {
	return m.root
}

func (m *Manager) FindModelName() string {
	candidate := filepath.Join(m.modelsDir(), m.model)
	if _, err := os.Stat(candidate); err == nil {
		return strings.TrimSuffix(m.model, filepath.Ext(m.model))
	}

	candidate = filepath.Join(m.modelsDir(), m.model+".gguf")
	if _, err := os.Stat(candidate); err == nil {
		return m.model
	}

	candidate = filepath.Join(m.modelsDir(), m.model+".bin")
	if _, err := os.Stat(candidate); err == nil {
		return m.model
	}

	entries, _ := os.ReadDir(m.modelsDir())
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".gguf") || strings.HasSuffix(entry.Name(), ".bin") {
			return strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
	}
	return m.model
}

func (m *Manager) applySavedState() {
	state, err := m.LoadState()
	if err != nil || state.Port <= 0 {
		return
	}
	if state.Model != "" {
		m.model = state.Model
	}
	m.setPort(state.Port)
}

func (m *Manager) LoadState() (*RuntimeState, error) {
	data, err := os.ReadFile(m.stateFile())
	if err != nil {
		return nil, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) saveState(pid int) error {
	runDir := filepath.Join(m.root, "run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	state := RuntimeState{
		Backend:   string(m.backend),
		Model:     m.model,
		Port:      m.port,
		PID:       pid,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/v1", m.port),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.stateFile(), data, 0o666)
}

func (m *Manager) ClearState() error {
	_ = os.Remove(m.pidFile())
	if err := os.Remove(m.stateFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) ensureUsablePort() error {
	if isPortAvailable(m.port) {
		return nil
	}
	port, err := chooseFreePort()
	if err != nil {
		return fmt.Errorf("select free port: %w", err)
	}
	m.setPort(port)
	return nil
}

func isPortAvailable(port int) bool {
	if port <= 0 {
		return false
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func chooseFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected address type %T", ln.Addr())
	}
	return addr.Port, nil
}
