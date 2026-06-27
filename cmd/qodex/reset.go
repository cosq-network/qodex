package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func resetCmd() *cobra.Command {
	var force, all bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove all Qodex state, configuration, and cached data",
		Long: `Removes Qodex data from this system.

Removes the project-local .qodex directory (config, database, skills).
Use --all to also remove the global Qodex user directory.

Session history in the local database will be permanently lost.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(force, all)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&all, "all", false, "also remove global Qodex user directory")
	return cmd
}

func runReset(force, all bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	projectDir := filepath.Join(cwd, ".qodex")

	// Check what exists
	hasProject := dirExists(projectDir)
	hasGlobal := false
	var globalDir string
	if all {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		globalDir = filepath.Join(home, ".config", "qodex")
		hasGlobal = dirExists(globalDir)
	}

	if !hasProject && !hasGlobal {
		fmt.Println("Nothing to reset — no Qodex data found.")
		return nil
	}

	// Confirmation prompt
	if !force {
		fmt.Println("This will permanently remove the following Qodex data:")
		if hasProject {
			fmt.Printf("  • %s/\n", projectDir)
			fmt.Println("    (project config, database with session history, skills)")
		}
		if hasGlobal {
			fmt.Printf("  • %s/\n", globalDir)
			fmt.Println("    (global config, skills)")
		}
		fmt.Println()
		fmt.Print("Are you sure? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "y" && line != "yes" {
			fmt.Println("Reset cancelled.")
			return nil
		}
	}

	if hasProject {
		if err := os.RemoveAll(projectDir); err != nil {
			return fmt.Errorf("remove %s: %w", projectDir, err)
		}
		fmt.Printf("Removed %s/\n", projectDir)
	}

	if hasGlobal {
		if err := os.RemoveAll(globalDir); err != nil {
			return fmt.Errorf("remove %s: %w", globalDir, err)
		}
		fmt.Printf("Removed %s/\n", globalDir)
	}

	fmt.Println("Qodex has been reset.")
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
