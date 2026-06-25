package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/benoybose/locha/internal/agent"
	"github.com/benoybose/locha/internal/config"
	"github.com/benoybose/locha/internal/model"
	"github.com/benoybose/locha/internal/skills"
	"github.com/benoybose/locha/internal/store"
	"github.com/benoybose/locha/internal/tools"
	"github.com/benoybose/locha/internal/tui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var cfgPath string
	var yes bool

	cmd := &cobra.Command{
		Use:   "locha",
		Short: "Local-first coding agent for llama.cpp and Qwen Coder",
	}
	cmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path")
	cmd.PersistentFlags().BoolVarP(&yes, "yes", "y", false, "auto-approve write and shell tools")

	cmd.AddCommand(initCmd())
	cmd.AddCommand(configCmd(&cfgPath))
	cmd.AddCommand(runCmd(&cfgPath, &yes))
	cmd.AddCommand(chatCmd(&cfgPath, &yes))
	cmd.AddCommand(doctorCmd(&cfgPath))
	cmd.AddCommand(skillsCmd())
	cmd.AddCommand(sessionsCmd(&cfgPath, &yes))
	return cmd
}

func configCmd(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and update Locha configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List effective configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			values := cfg.Values()
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Printf("%s=%s\n", key, values[key])
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get KEY",
		Short: "Show one effective configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			value, ok := cfg.Get(args[0])
			if !ok {
				return fmt.Errorf("unknown config key: %s", args[0])
			}
			fmt.Println(value)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set one project-local configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := config.SetProjectValue(cwd, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("Updated %s\n", config.ProjectConfigPath(cwd))
			return nil
		},
	})
	return cmd
}

func initCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create project-local Locha configuration and starter skill",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			lochaDir := filepath.Join(cwd, ".locha")
			configPath := filepath.Join(lochaDir, "config.toml")
			skillDir := filepath.Join(lochaDir, "skills", "project")
			skillPath := filepath.Join(skillDir, "SKILL.md")

			if err := os.MkdirAll(skillDir, 0o755); err != nil {
				return err
			}
			if err := writeStarterFile(configPath, starterConfig(), force); err != nil {
				return err
			}
			if err := writeStarterFile(skillPath, starterSkill(), force); err != nil {
				return err
			}
			fmt.Printf("Created %s\n", configPath)
			fmt.Printf("Created %s\n", skillPath)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing Locha files")
	return cmd
}

func runCmd(cfgPath *string, yes *bool) *cobra.Command {
	var sessionID int64
	cmd := &cobra.Command{
		Use:   "run PROMPT",
		Short: "Run a one-shot agent prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := buildRuntime(*cfgPath, *yes, false, sessionID)
			if err != nil {
				return err
			}
			defer rt.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
			defer cancel()

			result, err := rt.Agent.Run(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Println(result)
			return nil
		},
	}
	cmd.Flags().Int64Var(&sessionID, "session", 0, "resume an existing session by ID")
	return cmd
}

func writeStarterFile(path, content string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Printf("Exists %s\n", path)
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func starterConfig() string {
	return `[model]
provider = "openai-compatible"
base_url = "http://127.0.0.1:8080/v1"
model = "qwen2.5-coder"

[runtime]
backend = "llama.cpp"
context_tokens = 32768
temperature = 0.2
top_p = 0.95

[approval]
write_files = "ask"
run_commands = "ask"
network = "ask"

[store]
path = ".locha/locha.db"

[agent]
max_steps = 12
`
}

func starterSkill() string {
	return `# Project

Use this skill for repository-specific conventions.

- Inspect existing code before editing.
- Prefer narrow searches and focused file reads.
- Run the smallest relevant test command before broader test suites.
- Summarize changed files and verification at the end.
`
}

func chatCmd(cfgPath *string, yes *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start the terminal chat UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := buildRuntime(*cfgPath, *yes, true, 0)
			if err != nil {
				return err
			}
			defer rt.Close()

			var model tui.Model
			if rt.AutoApprove {
				model = tui.NewAutoApproved(rt.Agent)
			} else {
				model = tui.New(rt.Agent)
			}
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
}

func doctorCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration and local model connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			fmt.Printf("Project root: %s\n", cfg.ProjectRoot)
			fmt.Printf("Model endpoint: %s\n", cfg.Model.BaseURL)
			fmt.Printf("Model name: %s\n", cfg.Model.Model)
			fmt.Printf("Runtime backend: %s\n", cfg.Runtime.Backend)

			client := model.NewClient(cfg.Model.BaseURL, cfg.Model.Model)
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			if err := client.Check(ctx); err != nil {
				return fmt.Errorf("model endpoint check failed: %w", err)
			}
			fmt.Println("Model endpoint: ok")
			return nil
		},
	}
}

func skillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Inspect available skills",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			found, err := skills.Discover(cwd)
			if err != nil {
				return err
			}
			for _, skill := range found {
				fmt.Printf("%s\t%s\n", skill.Name, skill.Path)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show NAME",
		Short: "Show a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			found, err := skills.Discover(cwd)
			if err != nil {
				return err
			}
			for _, skill := range found {
				if skill.Name == args[0] {
					fmt.Println(skill.Content)
					return nil
				}
			}
			return fmt.Errorf("skill not found: %s", args[0])
		},
	})
	return cmd
}

func sessionsCmd(cfgPath *string, yes *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Inspect stored sessions",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List recent sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			db, err := store.Open(cfg.Store.Path)
			if err != nil {
				return err
			}
			defer db.Close()
			sessions, err := db.ListSessions(cmd.Context())
			if err != nil {
				return err
			}
			for _, s := range sessions {
				fmt.Printf("%d\t%s\t%s\n", s.ID, s.UpdatedAt.Format(time.RFC3339), s.Title)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "resume ID",
		Short: "Resume a session in the terminal chat UI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid session id: %w", err)
			}
			rt, err := buildRuntime(*cfgPath, *yes, true, id)
			if err != nil {
				return err
			}
			defer rt.Close()
			if _, err := rt.Store.GetSession(cmd.Context(), id); err != nil {
				return fmt.Errorf("session not found: %d", id)
			}
			messages, err := rt.Store.ListMessages(cmd.Context(), id)
			if err != nil {
				return err
			}
			var model tui.Model
			if rt.AutoApprove {
				model = tui.NewWithHistoryAutoApproved(rt.Agent, messages)
			} else {
				model = tui.NewWithHistory(rt.Agent, messages)
			}
			p := tea.NewProgram(model, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "export ID",
		Short: "Export session data as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid session id: %w", err)
			}
			cfg, err := config.Load(*cfgPath)
			if err != nil {
				return err
			}
			db, err := store.Open(cfg.Store.Path)
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := db.ExportSession(cmd.Context(), id)
			if err != nil {
				return err
			}
			raw, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		},
	})
	return cmd
}

type runtime struct {
	Agent       *agent.Agent
	Store       *store.Store
	AutoApprove bool
}

func (r *runtime) Close() {
	if r.Store != nil {
		_ = r.Store.Close()
	}
}

func buildRuntime(cfgPath string, yes bool, tuiMode bool, sessionID int64) (*runtime, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	db, err := store.Open(cfg.Store.Path)
	if err != nil {
		return nil, err
	}
	skillSet, err := skills.Discover(cfg.ProjectRoot)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	approver := agent.ApproverFunc(func(action agent.ApprovalRequest) bool {
		if yes || cfg.Approval.AutoApprove {
			return true
		}
		if tuiMode {
			return false
		}
		fmt.Printf("\nApprove %s?\n%s\n[y/N]: ", action.Kind, action.Summary)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		return line == "y" || line == "yes"
	})

	client := model.NewClient(cfg.Model.BaseURL, cfg.Model.Model)
	registry := tools.NewRegistry(cfg.ProjectRoot)
	agt := agent.New(agent.Options{
		Config:    cfg,
		Client:    client,
		Tools:     registry,
		Store:     db,
		Skills:    skillSet,
		Approver:  approver,
		MaxSteps:  cfg.Agent.MaxSteps,
		SessionID: sessionID,
	})
	if tuiMode {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		caps := client.DetectCapabilities(ctx)
		cancel()
		if caps.Streaming {
			agt.SetStreaming(true)
		}
	}
	return &runtime{Agent: agt, Store: db, AutoApprove: yes || cfg.Approval.AutoApprove}, nil
}
