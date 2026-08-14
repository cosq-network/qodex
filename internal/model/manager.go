package model

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	backend       Backend
	root          string
	port          int
	model         string
	contextTokens int
	threads       int
	client        *Client
}

// Keep the managed llama.cpp artifact reproducible. Operators can opt into a
// newer release explicitly with QODEX_LLAMA_CPP_VERSION after validating it.
const defaultLlamaCppVersion = "b9821"

func NewManager(backend Backend, installRoot, model string, port int) *Manager {
	if port <= 0 {
		port = defaultPort(backend)
	}
	return &Manager{
		backend:       backend,
		root:          installRoot,
		model:         model,
		port:          port,
		contextTokens: 32768,
		client:        NewClient(fmt.Sprintf("http://127.0.0.1:%d/v1", port), model),
	}
}

// SetContextTokens overrides the --ctx-size flag used when starting llama.cpp.
// Values <= 0 are ignored (the built-in default stays in effect).
func (m *Manager) SetContextTokens(n int) {
	if n > 0 {
		m.contextTokens = n
	}
}

// SetThreads overrides the --threads flag used when starting llama.cpp.
// Values <= 0 are ignored and the flag is omitted so llama.cpp auto-detects.
func (m *Manager) SetThreads(n int) {
	if n > 0 {
		m.threads = n
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
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	defer func() { _ = tmpFile.Close() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "qodex-setup")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close download: %w", err)
	}
	if expected := strings.TrimSpace(os.Getenv("QODEX_LLAMA_CPP_SHA256")); expected != "" {
		if err := verifySHA256(tmpFile.Name(), expected); err != nil {
			return fmt.Errorf("llama.cpp archive verification failed: %w", err)
		}
	}

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
	if version := strings.TrimSpace(os.Getenv("QODEX_LLAMA_CPP_VERSION")); version != "" {
		return version
	}
	return defaultLlamaCppVersion
}

func verifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

func (m *Manager) extractTar(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

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
				_ = dst.Close()
				return err
			}
			_ = dst.Close()
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
	python, err := pythonExecutable()
	if err != nil {
		return err
	}
	if err := exec.Command(python, "-c", "import vllm").Run(); err == nil {
		return nil
	}
	cmd := exec.Command(python, "-m", "pip", "install", "-q", "vllm", "fastapi", "uvicorn")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) installSGLang() error {
	python, err := pythonExecutable()
	if err != nil {
		return err
	}
	if err := exec.Command(python, "-c", "import sglang").Run(); err == nil {
		return nil
	}
	cmd := exec.Command(python, "-m", "pip", "install", "-q", "sglang", "openai")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pythonExecutable() (string, error) {
	for _, candidate := range []string{"python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("python 3 with pip not found - install Python 3.8+ and activate a virtual environment first")
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
			if p, err := os.FindProcess(pid); err == nil && p != nil && processAlive(p) {
				status.Running = true
				status.PID = pid
			} else {
				status.Error = "stale pid file"
				_ = m.ClearState()
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

	cmdArgs := []string{
		"-m", modelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", m.port),
		"--ctx-size", fmt.Sprintf("%d", m.contextTokens),
	}
	if m.threads > 0 {
		cmdArgs = append(cmdArgs, "--threads", fmt.Sprintf("%d", m.threads))
	}
	cmd := exec.Command(m.binaryPath(), cmdArgs...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	m.watchProcess(cmd)
	if err := writeAtomicFile(m.pidFile(), []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0o666); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	_ = m.saveState(cmd.Process.Pid)

	if err := m.waitForReady(context.Background(), cmd.Process.Pid, 30*time.Second); err != nil {
		_ = cmd.Process.Kill()
		_ = m.ClearState()
		return err
	}
	_ = m.saveState(cmd.Process.Pid)
	fmt.Printf("Model server started (PID: %d, Port: %d)\n", cmd.Process.Pid, m.port)
	return nil
}

// waitForReady waits for the backend's OpenAI-compatible health endpoint. A
// process being alive is not sufficient: model loading and GPU initialization
// can take a substantial amount of time before /v1/models responds.
func (m *Manager) waitForReady(ctx context.Context, pid int, timeout time.Duration) error {
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		checkCtx, checkCancel := context.WithTimeout(readyCtx, 2*time.Second)
		lastErr = m.client.Check(checkCtx)
		checkCancel()
		if lastErr == nil {
			return nil
		}
		select {
		case <-readyCtx.Done():
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
			}
			_ = m.ClearState()
			if lastErr != nil {
				return fmt.Errorf("server process %d did not become ready within %s: %w", pid, timeout, lastErr)
			}
			return fmt.Errorf("server process %d did not become ready within %s", pid, timeout)
		case <-ticker.C:
		}
	}
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
	if strings.TrimSpace(m.model) == "" {
		return fmt.Errorf("no model configured - set model.model to a Hugging Face model ID")
	}
	python, err := pythonExecutable()
	if err != nil {
		return err
	}
	if err := m.ensureUsablePort(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, python, "-m", "vllm.entrypoints.openai.api_server",
		"--model", m.model,
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
	m.watchProcess(cmd)

	fmt.Printf("Model server starting (Port: %d)...\n", m.port)
	return m.waitForReady(ctx, cmd.Process.Pid, 2*time.Minute)
}

func (m *Manager) startSGLang(ctx context.Context) error {
	if strings.TrimSpace(m.model) == "" {
		return fmt.Errorf("no model configured - set model.model to a Hugging Face model ID")
	}
	python, err := pythonExecutable()
	if err != nil {
		return err
	}
	if err := m.ensureUsablePort(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, python, "-m", "sglang.launch_server",
		"--model-path", m.model,
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
	m.watchProcess(cmd)

	fmt.Printf("Model server starting (Port: %d)...\n", m.port)
	return m.waitForReady(ctx, cmd.Process.Pid, 2*time.Minute)
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
	return writeAtomicFile(m.stateFile(), data, 0o666)
}

func (m *Manager) watchProcess(cmd *exec.Cmd) {
	pid := cmd.Process.Pid
	go func() {
		_ = cmd.Wait()
		state, err := m.LoadState()
		if err == nil && state.PID == pid {
			_ = m.ClearState()
		}
	}()
}

func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qodex-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpName, path)
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
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected address type %T", ln.Addr())
	}
	return addr.Port, nil
}
