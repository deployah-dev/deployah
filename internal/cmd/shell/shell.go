package shell

import (
	"fmt"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/session"
)

// Options holds command-line flags for shell.
type Options struct {
	Project     string `nabat:"project"`
	Component   string `nabat:"component"`
	Container   string `nabat:"container"`
	Shell       string `nabat:"shell"`
	Command     string `nabat:"command"`
	WorkDir     string `nabat:"workdir"`
	Environment string `nabat:"environment"`
}

// Register adds the shell command to app.
func Register(app *nabat.App) {
	app.MustCommand("shell",
		nabat.WithDescription("Connect to a shell in a container"),
		nabat.WithLongDescription("Connect to an interactive shell in a container of a deployed project."),
		nabat.WithArg("project", "", nabat.WithRequired(), nabat.WithUsage("Project name to connect to"), nabat.WithPrompt("Project name", "", nabat.WithHint("e.g. my-app"))),
		nabat.WithFlag("component", "", nabat.WithUsage("Component name (e.g., api, web, worker)")),
		nabat.WithFlag("container", "", nabat.WithUsage("Container name (if pod has multiple containers)")),
		nabat.WithFlag("shell", "", nabat.WithUsage("Preferred shell (bash, zsh, sh, ash, dash, fish)")),
		nabat.WithFlag("command", "", nabat.WithUsage("Command to execute (default: shell)")),
		nabat.WithFlag("workdir", "", nabat.WithUsage("Working directory in container")),
		nabat.WithFlag("environment", "", nabat.WithShort('e'), nabat.WithUsage("Filter by environment name (e.g., dev, staging, prod)")),
		nabat.WithExample(`
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
deployah shell myproject --environment=prod`),
		nabat.WithRun(runShell),
	)
}

func runShell(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	rt := session.FromContext(c)

	cluster, err := rt.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}

	executor, err := NewShellExecutor(cluster, c)
	if err != nil {
		return fmt.Errorf("shell executor: %w", err)
	}

	return executor.Execute(ExecuteOptions{
		ProjectName: opts.Project,
		Component:   opts.Component,
		Container:   opts.Container,
		Shell:       opts.Shell,
		Command:     opts.Command,
		WorkDir:     opts.WorkDir,
		Environment: opts.Environment,
	})
}
