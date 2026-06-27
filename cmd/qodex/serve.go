package main

import (
	"fmt"

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

			switch args[0] {
			case "start":
				return mgr.EnsureRunning(cmd.Context())
			case "stop":
				return mgr.Stop()
			case "status":
				status, err := mgr.Status(cmd.Context())
				if err != nil {
					return err
				}
				if status.Running {
					fmt.Printf("Status: running\n")
					fmt.Printf("PID: %d\n", status.PID)
					fmt.Printf("Port: %d\n", status.Port)
					fmt.Printf("Model: %s\n", status.Model)
				} else {
					fmt.Printf("Status: not running\n")
					fmt.Printf("Port: %d\n", status.Port)
					if status.Error != "" {
						fmt.Printf("Error: %s\n", status.Error)
					}
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

