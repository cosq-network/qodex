package main

import (
	"fmt"
	"path/filepath"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
	"github.com/spf13/cobra"
)

func serveCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve <start|stop|status>",
		Short: "Manage model server lifecycle",
		Long: `Manage local model server instances.

Qodex can start, stop, and check the status of model servers.
Servers run as background processes managed by Qodex.

Run 'qodex setup' to configure the model backend.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}

			installRoot := getInstallRoot()
			mgr := model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, port)
			if port == 0 {
				if state, err := mgr.LoadState(); err == nil && state.Port > 0 {
					mgr = model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, state.Port)
				}
			}
			if cfg.Runtime.Backend == string(model.BackendExternal) {
				switch args[0] {
				case "status":
					fmt.Println("Status: external endpoint mode")
					fmt.Printf("Endpoint: %s\n", cfg.Model.BaseURL)
					fmt.Printf("Model: %s\n", cfg.Model.Model)
					return nil
				default:
					return fmt.Errorf("managed backend commands are unavailable in external endpoint mode")
				}
			}

			switch args[0] {
			case "start":
				return mgr.EnsureRunning(cmd.Context())
			case "stop":
				return mgr.Stop()
			case "status":
				diag := mgr.Diagnostics(cmd.Context())
				if diag.Server.Running {
					fmt.Printf("Status: running\n")
					fmt.Printf("PID: %d\n", diag.Server.PID)
					fmt.Printf("Port: %d\n", diag.Server.Port)
					fmt.Printf("Model: %s\n", diag.Server.Model)
				} else {
					fmt.Printf("Status: not running\n")
					fmt.Printf("Port: %d\n", diag.Server.Port)
					if diag.Server.Error != "" {
						fmt.Printf("Error: %s\n", diag.Server.Error)
					}
				}
				fmt.Printf("Backend: %s\n", diag.Backend)
				if diag.BackendInstalled {
					fmt.Printf("Backend install: ok")
					if diag.BinaryPath != "" {
						fmt.Printf(" (%s)", diag.BinaryPath)
					}
					fmt.Println()
				} else {
					fmt.Println("Backend install: missing")
				}
				if diag.ModelPresent {
					fmt.Printf("Model file: %s\n", diag.ModelPath)
				} else {
					fmt.Printf("Model file: missing (expected %s under %s)\n", diag.ModelName, filepath.Join(installRoot, "models"))
				}
				return nil
			default:
				return fmt.Errorf("unknown action: %s (use start, stop, or status)", args[0])
			}
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 0, "server port (default: 8080 for llama.cpp, 8000 for vLLM/SGLang)")
	return cmd
}

func upCmd() *cobra.Command {
	var port int
	return &cobra.Command{
		Use:   "up",
		Short: "Ensure the managed backend is installed, configured, and running",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			if cfg.Runtime.Backend == string(model.BackendExternal) {
				return fmt.Errorf("up is unavailable in external endpoint mode")
			}
			installRoot := getInstallRoot()
			mgr := model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, port)
			if port == 0 {
				if state, err := mgr.LoadState(); err == nil && state.Port > 0 {
					mgr = model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, state.Port)
				}
			}
			return mgr.EnsureRunning(cmd.Context())
		},
	}
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the managed backend if it is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			if cfg.Runtime.Backend == string(model.BackendExternal) {
				return fmt.Errorf("down is unavailable in external endpoint mode")
			}
			installRoot := getInstallRoot()
			mgr := model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, 0)
			if state, err := mgr.LoadState(); err == nil && state.Port > 0 {
				mgr = model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, state.Port)
			}
			return mgr.Stop()
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show managed backend status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				return err
			}
			if cfg.Runtime.Backend == string(model.BackendExternal) {
				fmt.Println("Status: external endpoint mode")
				fmt.Printf("Endpoint: %s\n", cfg.Model.BaseURL)
				fmt.Printf("Model: %s\n", cfg.Model.Model)
				return nil
			}
			installRoot := getInstallRoot()
			mgr := model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, 0)
			if state, err := mgr.LoadState(); err == nil && state.Port > 0 {
				mgr = model.NewManager(model.Backend(cfg.Runtime.Backend), installRoot, cfg.Model.Model, state.Port)
			}
			diag := mgr.Diagnostics(cmd.Context())
			if diag.Server.Running {
				fmt.Printf("Status: running\nPID: %d\nPort: %d\nModel: %s\n", diag.Server.PID, diag.Server.Port, diag.Server.Model)
			} else {
				fmt.Printf("Status: not running\nPort: %d\n", diag.Server.Port)
				if diag.Server.Error != "" {
					fmt.Printf("Error: %s\n", diag.Server.Error)
				}
			}
			return nil
		},
	}
}

func modelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models <list|download> [model-name]",
		Short: "Manage downloaded models",
		Long: `List available models or download a model.

Models are stored in the Qodex data directory (default ~/.config/qodex/models/).`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			installRoot := getInstallRoot()
			registry := model.NewModelRegistry(installRoot)

			switch args[0] {
			case "list":
				models, err := registry.List()
				if err != nil {
					return fmt.Errorf("list models: %w", err)
				}
				for _, m := range models {
					downloaded := "not downloaded"
					if m.Downloaded {
						downloaded = "downloaded"
					}
					fmt.Printf("  %-45s %s\n", m.Name, downloaded)
				}
				return nil
			case "download":
				if len(args) < 2 {
					return fmt.Errorf("model name required for download")
				}
				fmt.Printf("Downloading %s...\n", args[1])
				return registry.Download(cmd.Context(), args[1])
			default:
				return fmt.Errorf("unknown action: %s (use list or download)", args[0])
			}
		},
	}
}
