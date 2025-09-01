package deploy

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh/spinner"
	"github.com/deployah-dev/deployah/internal/cli"
	"github.com/deployah-dev/deployah/internal/logging"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/spf13/cobra"
)

// New creates the deploy sub-command for the CLI.
func New() *cobra.Command {
	deployCommand := &cobra.Command{
		Use:   "deploy <environment>",
		Short: "Deploy a project to a Kubernetes cluster on a given environment",
		Long:  `Deploy a project to a Kubernetes cluster on a given environment.`,
		Example: `
# Deploy to production using default manifest path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit manifest path
deployah deploy staging -f ./path/to/deployah.yaml

# Deploy to production with a dry run and an explicit manifest path
deployah deploy prod --dry-run -f ./path/to/deployah.yaml
		`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("you must specify exactly one environment (e.g., 'deployah deploy prod'). Received %d argument(s): %v", len(args), args)
			}
			return nil
		},
		RunE: runDeploy,
	}

	deployCommand.Flags().BoolP("dry-run", "d", false, "Perform a dry run (render templates without installing)")

	return deployCommand
}

func runDeploy(cmd *cobra.Command, args []string) error {
	logger := logging.GetLogger(cmd)

	logger.Debug("Starting deployment process")

	environment := args[0]

	// Use runtime to load and memoize manifest and clients
	rt := runtime.FromRuntime(cmd.Context())
	if rt == nil {
		return fmt.Errorf("runtime not initialized")
	}

	manifest, err := rt.Manifest(cmd.Context(), environment)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	logger.Debug("Manifest loaded successfully", "env", environment)

	// Initialize Helm client
	logger.Debug("Initializing Helm client")
	helmClient, err := cli.GetHelmClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to initialize helm client: %w", err)
	}

	// Check if dry-run flag is set
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("failed to get dry-run flag: %w", err)
	}

	if dryRun {
		logger.Info("Performing dry run for project", "project", manifest.Project, "environment", environment)
	} else {
		logger.Debug("Installing release", "project", manifest.Project, "environment", environment)
	}

	// Determine spinner title based on operation type
	var spinnerTitle string
	if dryRun {
		spinnerTitle = fmt.Sprintf("Performing dry run for project '%s' in environment '%s'...", manifest.Project, environment)
	} else {
		spinnerTitle = fmt.Sprintf("Deploying project '%s' to environment '%s'...", manifest.Project, environment)
	}

	err = spinner.New().
		Title(spinnerTitle).
		Context(cmd.Context()).
		ActionWithErr(func(ctx context.Context) error {
			return helmClient.InstallApp(ctx, manifest, environment, dryRun)
		}).
		Run()

	if err != nil {
		if dryRun {
			return fmt.Errorf("deploy dry run failed: %w", err)
		} else {
			return fmt.Errorf("deploy failed: %w", err)
		}
	}

	if dryRun {
		logger.Info("Dry run completed successfully")
	} else {
		logger.Info("Deployment process completed successfully")
	}

	return nil
}
