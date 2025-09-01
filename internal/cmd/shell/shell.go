package shell

import (
	"fmt"
	"strings"

	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/spf13/cobra"
)

// New creates the shell sub-command for the CLI.
func New() *cobra.Command {
	shellCommand := &cobra.Command{
		Use:   "shell <project>",
		Short: "Connect to a shell in a container",
		Long: "Connect to an interactive shell in a container of a deployed project.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("project name is required")
			}

			if strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("project name cannot be empty")
			}

			return nil
		},
		RunE: runShell,
		Example: `
# Connect to default component and container
deployah shell myproject

# Connect to specific component
deployah shell myproject --component=api

# Connect to specific container
deployah shell myproject --container=app

# Use specific shell
deployah shell myproject --shell=zsh

# Execute specific command instead of shell
deployah shell myproject --command="ls -la"

# Set working directory
deployah shell myproject --workdir=/app/src

# Connect to specific environment
deployah shell myproject --environment=prod`,
	}

	// Add flags
	shellCommand.Flags().String("component", "", "Component name (e.g., api, web, worker)")
	shellCommand.Flags().String("container", "", "Container name (if pod has multiple containers)")
	shellCommand.Flags().String("shell", "", "Preferred shell (bash, zsh, sh, ash, dash, fish)")
	shellCommand.Flags().String("command", "", "Command to execute (default: shell)")
	shellCommand.Flags().String("workdir", "", "Working directory in container")
	shellCommand.Flags().String("environment", "", "Filter by environment name (e.g., dev, staging, prod)")
	shellCommand.Flags().Bool("no-tty", false, "Disable TTY allocation")
	shellCommand.Flags().Bool("stdin", true, "Keep STDIN open")
	shellCommand.Flags().Bool("stdout", true, "Redirect STDOUT")
	shellCommand.Flags().Bool("stderr", true, "Redirect STDERR")

	return shellCommand
}

func runShell(cmd *cobra.Command, args []string) error {
	// Extract all flag values with proper error handling
	component, err := cmd.Flags().GetString("component")
	if err != nil {
		return fmt.Errorf("failed to get component flag: %w", err)
	}
	container, err := cmd.Flags().GetString("container")
	if err != nil {
		return fmt.Errorf("failed to get container flag: %w", err)
	}
	shell, err := cmd.Flags().GetString("shell")
	if err != nil {
		return fmt.Errorf("failed to get shell flag: %w", err)
	}
	command, err := cmd.Flags().GetString("command")
	if err != nil {
		return fmt.Errorf("failed to get command flag: %w", err)
	}
	workdir, err := cmd.Flags().GetString("workdir")
	if err != nil {
		return fmt.Errorf("failed to get workdir flag: %w", err)
	}
	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		return fmt.Errorf("failed to get environment flag: %w", err)
	}

	projectName := args[0]

	// Initialize runtime and get manifest
	rt := runtime.FromRuntime(cmd.Context())
	if rt == nil {
		return fmt.Errorf("runtime not initialized")
	}

	// Create shell executor
	executor, err := NewShellExecutor(rt, cmd)
	if err != nil {
		return fmt.Errorf("failed to create shell executor: %w", err)
	}

	// Execute shell command
	return executor.Execute(ExecuteOptions{
		ProjectName: projectName,
		Component:   component,
		Container:   container,
		Shell:       shell,
		Command:     command,
		WorkDir:     workdir,
		Environment: environment,
	})
}
