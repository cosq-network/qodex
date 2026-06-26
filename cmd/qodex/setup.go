package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benoybose/qodex/internal/model"
	"github.com/spf13/cobra"
)

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time setup wizard",
		Long: `Walks through configuring Qodex: model endpoint, model name,
connectivity testing, and creating project configuration.`,
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

	// Step 1: Model endpoint
	fmt.Println("Step 1: Model Endpoint")
	fmt.Println("Enter the URL of your OpenAI-compatible model server.")
	fmt.Println("Common options:")
	fmt.Println("  http://127.0.0.1:8080/v1   (llama.cpp, default)")
	fmt.Println("  http://127.0.0.1:8000/v1   (vLLM)")
	fmt.Println("  http://127.0.0.1:8000/v1   (SGLang)")
	fmt.Print("\nEndpoint URL [http://127.0.0.1:8080/v1]: ")
	baseURL := readInput(reader, "http://127.0.0.1:8080/v1")
	baseURL = strings.TrimRight(baseURL, "/")

	// Validate URL
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		fmt.Printf("Invalid URL: %s. Using default.\n", err)
		baseURL = "http://127.0.0.1:8080/v1"
	}

	// Step 2: Model name
	fmt.Println("\nStep 2: Model Name")
	fmt.Println("Enter the model name your server is serving.")
	fmt.Print("Model name [qwen2.5-coder]: ")
	modelName := readInput(reader, "qwen2.5-coder")

	// Step 3: Test connectivity
	fmt.Println("\nStep 3: Testing Connectivity")
	client := model.NewClient(baseURL, modelName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connected := false
	if err := client.Check(ctx); err == nil {
		fmt.Println("  ✓ Connected to model endpoint")
		connected = true
	} else {
		fmt.Printf("  ✗ Could not connect: %s\n", err)

		// Offer options on failure
		for {
			fmt.Println()
			fmt.Println("  Options:")
			fmt.Println("    1. Enter a different URL")
			fmt.Println("    2. Continue anyway (you can fix this later)")
			fmt.Println("    3. Show setup instructions for llama.cpp")
			fmt.Print("  Choose [1-3]: ")
			choice := strings.TrimSpace(readInput(reader, "2"))

			switch choice {
			case "1":
				fmt.Print("  Endpoint URL: ")
				baseURL = strings.TrimRight(readInput(reader, baseURL), "/")
				if _, err := url.ParseRequestURI(baseURL); err != nil {
					fmt.Printf("  Invalid URL: %s. Keeping previous.\n", err)
					continue
				}
				client = model.NewClient(baseURL, modelName)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err = client.Check(ctx)
				cancel()
				if err == nil {
					fmt.Println("  ✓ Connected!")
					connected = true
					break
				}
				fmt.Printf("  ✗ Still could not connect: %s\n", err)

			case "2":
				fmt.Println("  Continuing without connectivity. Run 'qodex doctor' later to check.")
				connected = false
				break

			case "3":
				printLLamaInstructions()
				fmt.Print("\n  Press Enter to continue...")
				_, _ = reader.ReadString('\n')

			default:
				fmt.Println("  Invalid choice.")
			}

			if connected || choice == "2" {
				break
			}
		}
	}

	// If model endpoint includes /v1, detect backend
	backend := "llama.cpp"
	if connected {
		backend = detectBackend(baseURL)
	}

	// Step 4: Create configuration
	fmt.Println("\nStep 4: Creating Configuration")

	qodexDir := filepath.Join(projectRoot, ".qodex")
	configPath := filepath.Join(qodexDir, "config.toml")
	skillDir := filepath.Join(qodexDir, "skills", "project")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}

	// Write config.toml
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
`, baseURL, modelName, backend)

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("  ✓ Created %s\n", configPath)

	// Write starter skill
	skillContent := `# Project

Use this skill for repository-specific conventions.

- Inspect existing code before editing.
- Prefer narrow searches and focused file reads.
- Run the smallest relevant test command before broader test suites.
- Summarize changed files and verification at the end.
`
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	fmt.Printf("  ✓ Created %s\n", skillPath)

	// Summary
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║         Setup Complete!                     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Model endpoint: %s\n", baseURL)
	fmt.Printf("  Model name:     %s\n", modelName)
	fmt.Printf("  Backend:        %s\n", backend)
	fmt.Printf("  Connected:      %v\n", connected)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    qodex doctor             Check connectivity")
	fmt.Println("    qodex chat               Start the interactive TUI")
	fmt.Println(`    qodex run "hello"         Run a one-shot prompt`)
	fmt.Println("    qodex review             Review uncommitted changes")
	fmt.Println("    qodex config set ...     Adjust configuration")
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

func detectBackend(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "llama.cpp"
	}
	if port := u.Port(); port == "8080" || port == "8081" {
		return "llama.cpp"
	}
	if port := u.Port(); port == "8000" {
		// Could be vLLM or SGLang, check /health
		return "vllm"
	}
	return "llama.cpp"
}

func printLLamaInstructions() {
	fmt.Println()
	fmt.Println("  ── llama.cpp Setup ──")
	fmt.Println()
	fmt.Println("  1. Download a Qwen Coder GGUF model:")
	fmt.Println("     https://huggingface.co/Qwen")
	fmt.Println()
	fmt.Println("  2. Start the llama.cpp server:")
	fmt.Println("     llama-server -m qwen2.5-coder-7b-q4_k_m.gguf \\")
	fmt.Println("       --host 127.0.0.1 --port 8080")
	fmt.Println()
	fmt.Println("  3. Verify it's running:")
	fmt.Println("     curl http://127.0.0.1:8080/v1/models")
	fmt.Println()
	fmt.Println("  4. Run 'qodex setup' again or configure manually:")
	fmt.Println("     qodex config set model.base_url http://127.0.0.1:8080/v1")
	fmt.Println("     qodex config set model.model qwen2.5-coder")
	fmt.Println()
	fmt.Println("  ────────────────────")
}

// ensureConfigExists checks whether a global or project-level config file exists.
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
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
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
	// Check if it's a net error
	if _, ok := err.(net.Error); ok {
		return fmt.Errorf("%w\n\nNetwork error — check your model endpoint.\nRun 'qodex doctor' for diagnostics.", err)
	}
	return err
}
