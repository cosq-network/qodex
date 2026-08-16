package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	runtimepkg "runtime"
	"strconv"
	"strings"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/credentials"
	"github.com/benoybose/qodex/internal/model"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

type setupModelSource interface {
	List() ([]model.ModelInfo, error)
	Download(context.Context, string) error
	ModelsDir() string
	IsDownloaded(string) bool
	SetProgressFunc(model.ProgressFunc)
}

type setupProfile string

type setupTarget struct {
	backend  model.Backend
	model    string
	baseURL  string
	authType string
	tokenEnv string
	header   string
	apiKey   string // setup-only; never written to config
	local    bool
}

const (
	setupProfileConsumer  setupProfile = "consumer"
	setupProfileServer    setupProfile = "server"
	setupProfileGPUServer setupProfile = "gpu-server"
)

const (
	consumerSetupModel  = "deepseek-coder-6.7b-q4_k_m.gguf"
	serverSetupModel    = "qwen2.5-coder-7b-q4_k_m.gguf"
	gpuServerSetupModel = "qwen2.5-coder-32b-q4_k_m.gguf"
)

func downloadProgress() model.ProgressFunc {
	bar := model.NewProgress(0)
	return func(downloaded, total int64) {
		bar.Update(downloaded, total)
		if isatty.IsTerminal(os.Stderr.Fd()) {
			bar.WriteCLI(os.Stderr, 30)
		} else {
			bar.WriteLine(os.Stderr, 30)
		}
	}
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Automatically configure Qodex for this system",
		Long: `Walks through configuring Qodex:
1. Detect the system profile and compatible local backend
2. Download/install the backend automatically
3. Select and download the profile-appropriate model
4. Start the model server
5. Create configuration`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			return runSetup(cwd)
		},
	}
}

func runSetup(projectRoot string) error {
	if isatty.IsTerminal(os.Stdin.Fd()) {
		return runSetupTUI(projectRoot)
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         Qodex Setup Wizard                  ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Detect the system and choose a compatible backend/model pair.
	profile := detectSetupProfile()
	defaultBackend := automaticSetupBackend()
	defaultModel := setupModelForProfile(profile)
	fmt.Printf("Step 1: Detecting system\n  Profile: %s\n  Backend: %s\n  Model:   %s\n", profile, defaultBackend, defaultModel)
	registry := model.NewModelRegistry(getInstallRoot())
	target, err := chooseSetupTarget(reader, profile, defaultBackend, defaultModel, registry, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	backend := target.backend
	modelName := target.model
	if !target.local {
		return writeHostedSetup(projectRoot, target)
	}

	// Step 2: Install backend
	fmt.Println("\nStep 3: Install Backend")
	installRoot := getInstallRoot()
	mgr := model.NewManager(backend, installRoot, modelName, 0)

	fmt.Printf("Installing %s...\n", backend)
	if err := mgr.Install(context.Background()); err != nil {
		fmt.Printf("  ✗ Install failed: %s\n", err)
		fmt.Println("  Setup stopped; no project configuration was written.")
		return fmt.Errorf("install %s: %w", backend, err)
	} else {
		fmt.Printf("  ✓ %s installed\n", backend)
	}

	// Step 3: Acquire the automatically selected GGUF model.
	fmt.Println("\nStep 4: Preparing Model")
	modelReady, err := ensureSetupModel(context.Background(), registry, modelName, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Printf("  ✗ Model setup failed: %s\n", err)
	}
	if !modelReady {
		return fmt.Errorf("model %q is not available; download it and rerun qodex setup", modelName)
	}
	mgr = model.NewManager(backend, installRoot, modelName, 0)
	mgr.SetContextTokens(config.Defaults(projectRoot).Runtime.ContextTokens)
	mgr.SetThreads(runtimepkg.NumCPU())

	// Step 4: Start server
	fmt.Println("\nStep 5: Start Model Server")
	if !modelReady {
		fmt.Println("Skipping automatic start because no local model is available yet.")
		fmt.Println("Download the selected model with 'qodex models download <model-name>' and then run 'qodex serve start'.")
	} else {
		fmt.Println("Starting model server...")
		if err := mgr.EnsureRunning(context.Background()); err != nil {
			fmt.Printf("  ✗ Failed to start: %s\n", err)
			fmt.Println("  Setup stopped; no project configuration was written.")
			return fmt.Errorf("start %s: %w", backend, err)
		} else {
			fmt.Println("  ✓ Model server running")
		}
	}

	// Step 5: Create config
	fmt.Println("\nStep 6: Creating Configuration")
	cfg := config.Defaults(projectRoot)
	cfg.Runtime.Backend = string(backend)
	cfg.Model.BaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", mgr.Port())
	cfg.Model.Model = mgr.FindModelName()
	if err := writeSetupFiles(projectRoot, cfg); err != nil {
		return err
	}

	// Summary
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         Setup Complete!                     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Model endpoint: http://127.0.0.1:%d/v1\n", mgr.Port())
	fmt.Printf("  Backend:        %s\n", backend)
	fmt.Printf("  Model:          %s\n", cfg.Model.Model)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    qodex chat               Start the interactive TUI")
	fmt.Println(`    qodex run "hello"         Run a one-shot prompt`)
	fmt.Println("    qodex serve status        Check server status")
	fmt.Println("    qodex serve stop          Stop the server when done")
	fmt.Println()

	return nil
}

func chooseSetupTarget(reader *bufio.Reader, profile setupProfile, backend model.Backend, defaultModel string, registry setupModelSource, out, errOut io.Writer) (setupTarget, error) {
	for {
		_, _ = fmt.Fprintf(out, "\nStep 2: Choose how to continue\n  1. Download the recommended local model (%s)\n  2. Choose a different catalog model\n  3. Configure Groq or OpenRouter with a BYOK environment variable\nChoose [1]: ", defaultModel)
		choice := readInput(reader, "1")
		switch choice {
		case "", "1":
			_, _ = fmt.Fprintf(out, "Download %s now? [Y/n]: ", defaultModel)
			if promptYesNo(reader, nil, "", true) {
				return setupTarget{backend: backend, model: defaultModel, local: true}, nil
			}
			_, _ = fmt.Fprintln(out, "No model will be downloaded. Choose another option to continue.")
		case "2":
			modelName, err := chooseCatalogModel(reader, registry, out)
			if err != nil {
				return setupTarget{}, err
			}
			_, _ = fmt.Fprintf(out, "Download %s now? [Y/n]: ", modelName)
			if promptYesNo(reader, nil, "", true) {
				return setupTarget{backend: backend, model: modelName, local: true}, nil
			}
			_, _ = fmt.Fprintln(out, "No model will be downloaded. Choose another option to continue.")
		case "3":
			return configureHostedTarget(reader, out)
		default:
			_, _ = fmt.Fprintln(errOut, "Please choose 1, 2, or 3.")
		}
	}
}

func chooseCatalogModel(reader *bufio.Reader, registry setupModelSource, out io.Writer) (string, error) {
	models, err := registry.List()
	if err != nil {
		return "", fmt.Errorf("list catalog models: %w", err)
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no catalog models are available")
	}
	_, _ = fmt.Fprintln(out, "Available catalog models:")
	for i, item := range models {
		_, _ = fmt.Fprintf(out, "  %d. %s (%s)\n", i+1, item.Name, item.Size)
	}
	_, _ = fmt.Fprintf(out, "Choose model [1]: ")
	choice := readInput(reader, "1")
	index := 1
	if _, err := fmt.Sscanf(choice, "%d", &index); err != nil || index < 1 || index > len(models) {
		index = 1
	}
	return models[index-1].Name, nil
}

func configureHostedTarget(reader *bufio.Reader, out io.Writer) (setupTarget, error) {
	_, _ = fmt.Fprintln(out, "\nHosted provider setup (API keys are never written to Qodex config)")
	_, _ = fmt.Fprint(out, "Provider [1=Groq, 2=OpenRouter]: ")
	choice := readInput(reader, "1")
	target := setupTarget{backend: model.BackendExternal, local: false, authType: "bearer"}
	switch choice {
	case "", "1":
		target.baseURL = "https://api.groq.com/openai/v1"
		target.model = "llama-3.3-70b-versatile"
		credential := readCredential(reader, out, "API key environment variable or pasted key [GROQ_API_KEY]: ", "GROQ_API_KEY")
		target.tokenEnv, target.apiKey = hostedCredential(credential, "GROQ_API_KEY")
	case "2":
		target.baseURL = "https://openrouter.ai/api/v1"
		target.model = "openai/gpt-oss-20b"
		credential := readCredential(reader, out, "API key environment variable or pasted key [OPENROUTER_API_KEY]: ", "OPENROUTER_API_KEY")
		target.tokenEnv, target.apiKey = hostedCredential(credential, "OPENROUTER_API_KEY")
	default:
		return setupTarget{}, fmt.Errorf("unsupported hosted provider choice %q", choice)
	}
	if !validEnvironmentVariableName(target.tokenEnv) {
		return setupTarget{}, fmt.Errorf("invalid environment variable name %q", target.tokenEnv)
	}
	target.model = selectHostedModel(reader, out, target.baseURL, target.authType, target.tokenEnv, target.apiKey, target.model)
	if target.apiKey != "" || strings.TrimSpace(os.Getenv(target.tokenEnv)) != "" {
		_, _ = fmt.Fprintf(out, "Credential accepted; Qodex will store it securely after configuration.\n")
	} else {
		_, _ = fmt.Fprintf(out, "No credential found; Qodex will use its secure credential store or %s when available.\n", target.tokenEnv)
	}
	return target, nil
}

func hostedCredential(value, fallbackEnv string) (envName, apiKey string) {
	value = strings.TrimSpace(value)
	if looksLikeHostedAPIKey(value) {
		return fallbackEnv, value
	}
	return value, ""
}

func looksLikeHostedAPIKey(value string) bool {
	return strings.HasPrefix(value, "gsk_") || strings.HasPrefix(value, "sk-or-")
}

func selectHostedModel(reader *bufio.Reader, out io.Writer, baseURL, authType, tokenEnv, apiKey, fallback string) string {
	if apiKey == "" && strings.TrimSpace(os.Getenv(tokenEnv)) == "" {
		_, _ = fmt.Fprintf(out, "%s is not set; using provisional model %s. Set the key and use /model %s list to validate and discover models later.\n", tokenEnv, fallback, hostedProviderName(baseURL))
		return fallback
	}
	client := model.NewClient(baseURL, fallback)
	client.SetAuth(authType, tokenEnv, "")
	client.SetAuthToken(apiKey)
	info, err := client.ListHostedModelInfo(context.Background(), strings.Contains(strings.ToLower(baseURL), "openrouter.ai"))
	if err != nil {
		_, _ = fmt.Fprintf(out, "Could not discover provider models (%v); using %s.\n", err, fallback)
		return fallback
	}
	return chooseHostedModel(reader, out, info, fallback)
}

func chooseHostedModel(reader *bufio.Reader, out io.Writer, models []model.HostedModelInfo, fallback string) string {
	_, _ = fmt.Fprintln(out, "Available hosted models:")
	for i, item := range models {
		_, _ = fmt.Fprintf(out, "  %d. %s [%s]\n", i+1, item.ID, hostedModelBadge(item))
	}
	_, _ = fmt.Fprintf(out, "Choose model [1] or enter a model ID (%s): ", fallback)
	choice := readInput(reader, "1")
	if index, err := strconv.Atoi(choice); err == nil && index >= 1 && index <= len(models) {
		return models[index-1].ID
	}
	if strings.TrimSpace(choice) != "" {
		return strings.TrimSpace(choice)
	}
	for _, item := range models {
		if item.ID == fallback {
			return fallback
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return fallback
}

func hostedProviderName(baseURL string) string {
	if strings.Contains(baseURL, "openrouter") {
		return "openrouter"
	}
	return "groq"
}

func hostedModelBadge(info model.HostedModelInfo) string {
	if info.Free {
		return "FREE"
	}
	if info.FreeTierEligible {
		return "FREE TIER"
	}
	if info.HasPricing {
		return fmt.Sprintf("$%.3f/$%.3f per 1M", info.PromptPrice, info.CompletionPrice)
	}
	return "pricing unavailable"
}

func validEnvironmentVariableName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func readInputPrompt(reader *bufio.Reader, out io.Writer, prompt, fallback string) string {
	_, _ = fmt.Fprint(out, prompt)
	return readInput(reader, fallback)
}

func readCredential(reader *bufio.Reader, out io.Writer, prompt, fallback string) string {
	_, _ = fmt.Fprint(out, prompt)
	if isatty.IsTerminal(os.Stdin.Fd()) {
		value, err := term.ReadPassword(os.Stdin.Fd())
		_, _ = fmt.Fprintln(out)
		if err == nil && strings.TrimSpace(string(value)) != "" {
			return strings.TrimSpace(string(value))
		}
		return fallback
	}
	return readInput(reader, fallback)
}

func writeHostedSetup(projectRoot string, target setupTarget) error {
	cfg := config.Defaults(projectRoot)
	cfg.Runtime.Backend = string(model.BackendExternal)
	cfg.Model.BaseURL = target.baseURL
	cfg.Model.Model = target.model
	cfg.Agent.ToolCalls = "native"
	cfg.Model.Auth = config.ModelAuthConfig{Type: target.authType, TokenEnv: target.tokenEnv, Header: target.header}
	if err := writeSetupFiles(projectRoot, cfg); err != nil {
		return err
	}
	providerKey := strings.TrimSpace(target.apiKey)
	if providerKey == "" {
		providerKey = strings.TrimSpace(os.Getenv(target.tokenEnv))
	}
	if providerKey != "" {
		if err := credentials.Save(projectRoot, target.tokenEnv, providerKey); err != nil {
			return fmt.Errorf("store hosted provider credential securely: %w", err)
		}
		fmt.Printf("Stored %s securely in the operating system credential store.\n", target.tokenEnv)
	} else {
		fmt.Printf("No %s value was available to store; Qodex will use the environment when it is set.\n", target.tokenEnv)
	}
	fmt.Printf("\nConfigured %s with model %s.\n", target.baseURL, target.model)
	fmt.Printf("The credential will be loaded automatically by Qodex.\n")
	return nil
}

func setupModelForProfile(profile setupProfile) string {
	switch profile {
	case setupProfileGPUServer:
		return gpuServerSetupModel
	case setupProfileServer:
		return serverSetupModel
	default:
		return consumerSetupModel
	}
}

// automaticSetupBackend deliberately selects llama.cpp for the built-in GGUF
// catalog. It works across supported managed platforms and is the only
// backend contract compatible with the profile models above. Native Windows
// falls back to external endpoint mode because managed llama.cpp installation
// is not supported there yet.
func automaticSetupBackend() model.Backend {
	if runtimepkg.GOOS == "windows" {
		return model.BackendExternal
	}
	return model.BackendLlamaCpp
}

func detectSetupProfile() setupProfile {
	if override := strings.ToLower(strings.TrimSpace(os.Getenv("QODEX_SETUP_PROFILE"))); override != "" {
		switch override {
		case string(setupProfileGPUServer), "gpu", "gpu_server":
			return setupProfileGPUServer
		case string(setupProfileServer):
			return setupProfileServer
		case string(setupProfileConsumer), "laptop", "desktop":
			return setupProfileConsumer
		}
	}

	server := isServerEnvironment()
	if server && hasGPU() {
		return setupProfileGPUServer
	}
	if server {
		return setupProfileServer
	}
	return setupProfileConsumer
}

func isServerEnvironment() bool {
	for _, key := range []string{"QODEX_SERVER", "KUBERNETES_SERVICE_HOST", "CONTAINER", "container"} {
		if value := strings.ToLower(strings.TrimSpace(os.Getenv(key))); value == "1" || value == "true" || value == "yes" || value != "" && key != "QODEX_SERVER" {
			return true
		}
	}
	if runtimepkg.GOOS == "linux" {
		for _, path := range []string{"/var/lib/cloud/instance", "/etc/cloud/cloud.cfg"} {
			if _, err := os.Stat(path); err == nil {
				return true
			}
		}
		// A headless Linux host with server-class resources is a reasonable
		// server signal, while avoiding classifying ordinary GUI workstations.
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && runtimepkg.NumCPU() >= 16 && systemMemoryBytes() >= 32<<30 {
			return true
		}
	}
	return false
}

func hasGPU() bool {
	for _, command := range []string{"nvidia-smi", "rocm-smi"} {
		if _, err := exec.LookPath(command); err == nil {
			return true
		}
	}
	return false
}

func systemMemoryBytes() int64 {
	if runtimepkg.GOOS == "linux" {
		data, err := os.ReadFile("/proc/meminfo")
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "MemTotal:" {
					kb, _ := strconv.ParseInt(fields[1], 10, 64)
					return kb * 1024
				}
			}
		}
	}
	if runtimepkg.GOOS == "darwin" {
		if output, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
			bytes, _ := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
			return bytes
		}
	}
	return 0
}

func ensureSetupModel(ctx context.Context, registry setupModelSource, modelName string, out, errOut io.Writer) (bool, error) {
	if registry.IsDownloaded(modelName) {
		_, _ = fmt.Fprintf(out, "  ✓ Model already available: %s\n", modelName)
		return true, nil
	}
	_, _ = fmt.Fprintf(out, "  Downloading recommended model: %s\n", modelName)
	registry.SetProgressFunc(downloadProgress())
	if err := registry.Download(ctx, modelName); err != nil {
		printManualModelHelp(errOut, modelName, registry.ModelsDir())
		return false, err
	}
	return true, nil
}

func selectRemoteModel(reader *bufio.Reader, backend model.Backend) string {
	defaultModel := "Qwen/Qwen2.5-Coder-7B-Instruct"
	if backend == model.BackendSGLang {
		defaultModel = "Qwen/Qwen2.5-Coder-7B-Instruct"
	}
	fmt.Printf("Enter a Hugging Face model ID [%s]: ", defaultModel)
	return readInput(reader, defaultModel)
}

func runExternalSetup(projectRoot string, reader *bufio.Reader) error {
	fmt.Println("\nExternal endpoint mode")
	fmt.Println("Qodex will use an existing OpenAI-compatible endpoint instead of managing a local backend.")

	fmt.Print("Endpoint URL [http://127.0.0.1:8080/v1]: ")
	baseURL := readInput(reader, "http://127.0.0.1:8080/v1")
	fmt.Print("Model name [qwen2.5-coder]: ")
	modelName := readInput(reader, "qwen2.5-coder")

	fmt.Println("\nCreating Configuration")
	cfg := config.Defaults(projectRoot)
	cfg.Runtime.Backend = string(model.BackendExternal)
	cfg.Model.BaseURL = strings.TrimRight(baseURL, "/")
	cfg.Model.Model = modelName

	if err := writeSetupFiles(projectRoot, cfg); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         Setup Complete!                     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Endpoint:       %s\n", cfg.Model.BaseURL)
	fmt.Printf("  Backend:        %s\n", cfg.Runtime.Backend)
	fmt.Printf("  Model:          %s\n", cfg.Model.Model)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    qodex doctor             Verify endpoint connectivity")
	fmt.Println("    qodex chat               Start the interactive TUI")
	fmt.Println(`    qodex run "hello"         Run a one-shot prompt`)
	fmt.Println()
	return nil
}

func readInput(reader *bufio.Reader, fallback string) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		return fallback
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback
	}
	return line
}

func promptYesNo(reader *bufio.Reader, out io.Writer, prompt string, fallback bool) bool {
	if out != nil {
		if _, err := fmt.Fprint(out, prompt); err != nil {
			return fallback
		}
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return fallback
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return fallback
	}
	return line == "y" || line == "yes"
}

func printManualModelHelp(errOut io.Writer, modelName, modelsDir string) {
	_, _ = fmt.Fprintf(errOut, "Manual download required for %s\n", modelName)
	_, _ = fmt.Fprintf(errOut, "Place the model file in: %s\n", modelsDir)
	_, _ = fmt.Fprintf(errOut, "Or run: qodex models download %s\n", modelName)
	_, _ = fmt.Fprintf(errOut, "Find GGUF models at: https://huggingface.co/models?library=gguf\n")
}

func maybeRedirectUnsupportedManagedBackend(reader *bufio.Reader, backend model.Backend, out io.Writer) model.Backend {
	switch backend {
	case model.BackendLlamaCpp:
		if runtimepkg.GOOS == "windows" {
			_, _ = fmt.Fprintln(out, "\nNative Windows does not support automatic llama.cpp setup yet.")
			_, _ = fmt.Fprintln(out, "WSL2 is recommended for managed local backends.")
			if promptYesNo(reader, out, "Configure an external endpoint instead? [Y/n]: ", true) {
				return model.BackendExternal
			}
		}
	case model.BackendVLLM:
		if !hasPython() {
			_, _ = fmt.Fprintln(out, "\nvLLM requires Python and pip on this machine.")
			if promptYesNo(reader, out, "Configure an external endpoint instead? [Y/n]: ", true) {
				return model.BackendExternal
			}
		}
	case model.BackendSGLang:
		if !hasPython() {
			_, _ = fmt.Fprintln(out, "\nSGLang requires Python and pip on this machine.")
			if promptYesNo(reader, out, "Configure an external endpoint instead? [Y/n]: ", true) {
				return model.BackendExternal
			}
		}
	}
	return backend
}

func hasPython() bool {
	for _, candidate := range []string{"python3", "python"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return true
		}
	}
	return false
}

func writeSetupFiles(projectRoot string, cfg config.Config) error {
	qodexDir := filepath.Join(projectRoot, ".qodex")
	configPath := filepath.Join(qodexDir, "config.toml")
	skillDir := filepath.Join(qodexDir, "skills", "project")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}

	authContent := ""
	if cfg.Model.Auth.Type != "" && cfg.Model.Auth.Type != "none" {
		authContent = fmt.Sprintf(`
auth = { type = "%s", token_env = "%s", header = "%s" }
`, cfg.Model.Auth.Type, cfg.Model.Auth.TokenEnv, cfg.Model.Auth.Header)
	}

	configContent := fmt.Sprintf(`[model]
provider = "openai-compatible"
base_url = "%s"
model = "%s"
%s

[runtime]
backend = "%s"
context_tokens = 32768
temperature = 0.2
top_p = 0.95

[approval]
write_files = "ask"
run_commands = "ask"
network = "ask"

[store]
path = ".qodex/qodex.db"

[agent]
max_steps = 12
tool_calls = "%s"
`, cfg.Model.BaseURL, cfg.Model.Model, authContent, cfg.Runtime.Backend, cfg.Agent.ToolCalls)

	if err := writeAtomic(configPath, []byte(configContent), 0o666); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("  ✓ Created %s\n", configPath)

	skillContent := `# Project

Use this skill for repository-specific conventions.

- Inspect existing code before editing.
- Prefer narrow searches and focused file reads.
- Run the smallest relevant test command before broader test suites.
- Summarize changed files and verification at the end.
`
	if err := writeAtomic(skillPath, []byte(skillContent), 0o666); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	fmt.Printf("  ✓ Created %s\n", skillPath)
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qodex-write-*")
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
	} else if runtimepkg.GOOS != "windows" {
		return err
	}
	// Windows cannot replace an existing file with RenameFile. The temporary
	// file still prevents a partially-written destination; replace the exact
	// setup target and retry.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpName, path)
}

func selectSetupModel(ctx context.Context, reader *bufio.Reader, registry setupModelSource, out, errOut io.Writer) (string, bool, error) {
	models, err := registry.List()
	if err != nil || len(models) == 0 {
		return "", false, err
	}

	_, _ = fmt.Fprintln(out, "Available models:")
	for i, m := range models {
		downloaded := ""
		if m.Downloaded {
			downloaded = " (downloaded)"
		}
		_, _ = fmt.Fprintf(out, "  %d. %s %s%s\n", i+1, m.Name, m.Size, downloaded)
	}
	_, _ = fmt.Fprint(out, "\nSelect model [1]: ")
	modelChoice := readInput(reader, "1")
	idx := 0
	_, _ = fmt.Sscanf(modelChoice, "%d", &idx)
	if idx <= 0 || idx > len(models) {
		idx = 1
	}

	modelName := models[idx-1].Name
	if models[idx-1].Downloaded || registry.IsDownloaded(modelName) {
		return modelName, true, nil
	}

	_, _ = fmt.Fprintf(out, "Model %s is not downloaded.\n", modelName)
	if !promptYesNo(reader, out, "Download now? [Y/n]: ", true) {
		printManualModelHelp(errOut, modelName, registry.ModelsDir())
		return modelName, false, nil
	}
	registry.SetProgressFunc(downloadProgress())
	if err := registry.Download(ctx, modelName); err != nil {
		return modelName, false, err
	}
	return modelName, true, nil
}

func ensureConfigExists(projectRoot string) bool {
	if _, err := os.Stat(filepath.Join(config.UserConfigDir(), "config.toml")); err == nil {
		return true
	}
	configPath := filepath.Join(projectRoot, ".qodex", "config.toml")
	_, err := os.Stat(configPath)
	return err == nil
}

func promptRunSetup(projectRoot string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("No Qodex configuration found.")
		fmt.Println("Run 'qodex init' to create one or 'qodex setup' for the interactive wizard.")
		return fmt.Errorf("setup required")
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Println()
	fmt.Println("No Qodex configuration found.")
	fmt.Println("You need to set up Qodex before using this command.")
	fmt.Print("Run the setup wizard now? [Y/n]: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "n" || line == "no" {
		fmt.Println()
		fmt.Println("Run 'qodex setup' when you're ready to configure.")
		return fmt.Errorf("setup required")
	}
	return runSetup(projectRoot)
}

func wrapModelError(err error) error {
	if err == nil {
		return nil
	}
	errStr := err.Error()
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "dial tcp") {
		return fmt.Errorf("%w; make sure your model server is running; run 'qodex doctor' to check connectivity or 'qodex setup' to reconfigure", err)
	}
	if _, ok := err.(net.Error); ok {
		return fmt.Errorf("%w; network error — check your model endpoint; run 'qodex doctor' for diagnostics", err)
	}
	return err
}

func getInstallRoot() string {
	return config.UserConfigDir()
}
