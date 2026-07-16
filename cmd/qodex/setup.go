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
	"strings"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

type setupModelSource interface {
	List() ([]model.ModelInfo, error)
	Download(context.Context, string) error
	ModelsDir() string
	IsDownloaded(string) bool
}

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time setup wizard",
		Long: `Walks through configuring Qodex:
1. Choose backend (llama.cpp, vLLM, or SGLang)
2. Download/install the backend automatically
3. Download a model if needed
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
	reader := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         Qodex Setup Wizard                  ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	// Step 1: Choose backend
	fmt.Println("Step 1: Choose Backend")
	fmt.Println("Select the model backend to use:")
	fmt.Println("  1. llama.cpp (recommended for Apple Silicon/Linux)")
	fmt.Println("  2. vLLM (for HuggingFace models with Python)")
	fmt.Println("  3. SGLang (high-performance inference)")
	fmt.Println("  4. External OpenAI-compatible endpoint")
	fmt.Print("\nChoice [1]: ")
	choice := readInput(reader, "1")

	var backend model.Backend
	switch choice {
	case "1":
		backend = model.BackendLlamaCpp
	case "2":
		backend = model.BackendVLLM
	case "3":
		backend = model.BackendSGLang
	case "4":
		backend = model.BackendExternal
	default:
		backend = model.BackendLlamaCpp
	}

	if fallback := maybeRedirectUnsupportedManagedBackend(reader, backend, os.Stdout); fallback == model.BackendExternal {
		backend = fallback
	}

	if backend == model.BackendExternal {
		return runExternalSetup(projectRoot, reader)
	}

	// Step 2: Install backend
	fmt.Println("\nStep 2: Install Backend")
	installRoot := getInstallRoot()
	mgr := model.NewManager(backend, installRoot, "qwen2.5-coder-7b-q4_k_m.gguf", 0)

	fmt.Printf("Installing %s...\n", backend)
	if err := mgr.Install(context.Background()); err != nil {
		fmt.Printf("  ✗ Install failed: %s\n", err)
		fmt.Println("  Continuing with manual setup...")
	} else {
		fmt.Printf("  ✓ %s installed\n", backend)
	}

	// Step 3: Choose/download model
	fmt.Println("\nStep 3: Choose Model")
	registry := model.NewModelRegistry(installRoot)
	modelName, modelReady, err := selectSetupModel(context.Background(), reader, registry, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Printf("  ✗ Model setup failed: %s\n", err)
		fmt.Println("  Continuing with manual model setup...")
	}
	if modelName == "" {
		modelName = readInput(reader, "qwen2.5-coder-7b-q4_k_m.gguf")
		modelReady = registry.IsDownloaded(modelName)
		if !modelReady {
			printManualModelHelp(os.Stderr, modelName, registry.ModelsDir())
		}
	}
	mgr = model.NewManager(backend, installRoot, modelName, 0)

	// Step 4: Start server
	fmt.Println("\nStep 4: Start Model Server")
	if !modelReady {
		fmt.Println("Skipping automatic start because no local model is available yet.")
		fmt.Println("Download the selected model with 'qodex models download <model-name>' and then run 'qodex serve start'.")
	} else {
		fmt.Println("Starting model server...")
		if err := mgr.EnsureRunning(context.Background()); err != nil {
			fmt.Printf("  ✗ Failed to start: %s\n", err)
			fmt.Println("  You can start it manually later with: qodex serve start")
		} else {
			fmt.Println("  ✓ Model server running")
		}
	}

	// Step 5: Create config
	fmt.Println("\nStep 5: Creating Configuration")
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
		fmt.Fprint(out, prompt)
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
	fmt.Fprintf(errOut, "Manual download required for %s\n", modelName)
	fmt.Fprintf(errOut, "Place the model file in: %s\n", modelsDir)
	fmt.Fprintf(errOut, "Or run: qodex models download %s\n", modelName)
	fmt.Fprintf(errOut, "Find GGUF models at: https://huggingface.co/models?library=gguf\n")
}

func maybeRedirectUnsupportedManagedBackend(reader *bufio.Reader, backend model.Backend, out io.Writer) model.Backend {
	switch backend {
	case model.BackendLlamaCpp:
		if runtimepkg.GOOS == "windows" {
			fmt.Fprintln(out, "\nNative Windows does not support automatic llama.cpp setup yet.")
			fmt.Fprintln(out, "WSL2 is recommended for managed local backends.")
			if promptYesNo(reader, out, "Configure an external endpoint instead? [Y/n]: ", true) {
				return model.BackendExternal
			}
		}
	case model.BackendVLLM:
		if _, err := exec.LookPath("pip"); err != nil {
			fmt.Fprintln(out, "\nvLLM requires Python and pip on this machine.")
			if promptYesNo(reader, out, "Configure an external endpoint instead? [Y/n]: ", true) {
				return model.BackendExternal
			}
		}
	case model.BackendSGLang:
		if _, err := exec.LookPath("pip"); err != nil {
			fmt.Fprintln(out, "\nSGLang requires Python and pip on this machine.")
			if promptYesNo(reader, out, "Configure an external endpoint instead? [Y/n]: ", true) {
				return model.BackendExternal
			}
		}
	}
	return backend
}

func writeSetupFiles(projectRoot string, cfg config.Config) error {
	qodexDir := filepath.Join(projectRoot, ".qodex")
	configPath := filepath.Join(qodexDir, "config.toml")
	skillDir := filepath.Join(qodexDir, "skills", "project")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}

	configContent := fmt.Sprintf(`[model]
provider = "openai-compatible"
base_url = "%s"
model = "%s"

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
`, cfg.Model.BaseURL, cfg.Model.Model, cfg.Runtime.Backend)

	if err := os.WriteFile(configPath, []byte(configContent), 0o666); err != nil {
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
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o666); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	fmt.Printf("  ✓ Created %s\n", skillPath)
	return nil
}

func selectSetupModel(ctx context.Context, reader *bufio.Reader, registry setupModelSource, out, errOut io.Writer) (string, bool, error) {
	models, err := registry.List()
	if err != nil || len(models) == 0 {
		return "", false, err
	}

	fmt.Fprintln(out, "Available models:")
	for i, m := range models {
		downloaded := ""
		if m.Downloaded {
			downloaded = " (downloaded)"
		}
		fmt.Fprintf(out, "  %d. %s %s%s\n", i+1, m.Name, m.Size, downloaded)
	}
	fmt.Fprint(out, "\nSelect model [1]: ")
	modelChoice := readInput(reader, "1")
	idx := 0
	fmt.Sscanf(modelChoice, "%d", &idx)
	if idx <= 0 || idx > len(models) {
		idx = 1
	}

	modelName := models[idx-1].Name
	if models[idx-1].Downloaded || registry.IsDownloaded(modelName) {
		return modelName, true, nil
	}

	fmt.Fprintf(out, "Model %s is not downloaded.\n", modelName)
	if !promptYesNo(reader, out, "Download now? [Y/n]: ", true) {
		printManualModelHelp(errOut, modelName, registry.ModelsDir())
		return modelName, false, nil
	}
	if err := registry.Download(ctx, modelName); err != nil {
		return modelName, false, err
	}
	return modelName, true, nil
}

func ensureConfigExists(projectRoot string) bool {
	home, err := os.UserHomeDir()
	if err == nil {
		if _, err := os.Stat(filepath.Join(home, ".config", "qodex", "config.toml")); err == nil {
			return true
		}
	}
	configPath := filepath.Join(projectRoot, ".qodex", "config.toml")
	_, err = os.Stat(configPath)
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
		return fmt.Errorf("%w\n\nMake sure your model server is running.\nRun 'qodex doctor' to check connectivity or 'qodex setup' to reconfigure.", err)
	}
	if _, ok := err.(net.Error); ok {
		return fmt.Errorf("%w\n\nNetwork error — check your model endpoint.\nRun 'qodex doctor' for diagnostics.", err)
	}
	return err
}

func getInstallRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".qodex"
	}
	return filepath.Join(home, ".config", "qodex")
}
