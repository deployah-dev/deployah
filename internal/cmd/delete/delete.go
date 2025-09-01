// Package delete provides the CLI commands for the Deployah application.
package delete

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/log"
	"github.com/deployah-dev/deployah/internal/cli"
	"github.com/deployah-dev/deployah/internal/logging"
	"github.com/spf13/cobra"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// New creates the delete sub-command for the CLI.
// It returns a new Cobra command for deleting Deployah projects.
func New() *cobra.Command {
	deleteCommand := &cobra.Command{
		Use:     "delete <project> <environment>",
		Aliases: []string{"uninstall", "remove"},
		Short:   "Delete a deployed project in an environment",
		Long:    `Delete (uninstall) a deployed project in an environment from the Kubernetes cluster.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("project and environment are required, eg. 'deployah delete project1 production'")
			}
			return nil
		},
		RunE: runDelete,
	}

	deleteCommand.Flags().Bool("force", false, "Force deletion without confirmation")
	deleteCommand.Flags().Bool("dry-run", false, "Simulate the deletion without actually removing the project")
	deleteCommand.Flags().Bool("show-resources", false, "Show detailed resources that would be deleted (implies --dry-run)")

	return deleteCommand
}

// runDelete is the main function for the delete command.
// It deletes a Helm project in an environment from the Kubernetes cluster.
func runDelete(cmd *cobra.Command, args []string) error {
	logger := logging.GetLogger(cmd)

	project := args[0]
	environment := args[1]

	logger.Infof("Starting delete process for project '%s' in environment '%s'", project, environment)

	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	showResources, _ := cmd.Flags().GetBool("show-resources")

	// show-resources implies dry-run
	if showResources {
		dryRun = true
	}

	// Initialize Helm client from runtime
	helmClient, err := cli.GetHelmClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to initialize helm client: %w", err)
	}

	// Check if release exists before proceeding
	logger.Infof("Checking project '%s' in environment '%s' status", project, environment)
	release, err := helmClient.GetRelease(cmd.Context(), project, environment)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			logger.Warnf("Project '%s' in environment '%s' not found", project, environment)
			if !force {
				return fmt.Errorf("project '%s' in environment '%s' not found - use --force to ignore", project, environment)
			}
			logger.Infof("Continuing with --force flag despite missing project '%s' in environment '%s'", project, environment)
		} else {
			return fmt.Errorf("failed to check project status: %w", err)
		}
	} else {
		logger.Infof("Project '%s' in environment '%s' found, proceeding with deletion", project, environment)
	}

	// Enhanced dry-run output
	if dryRun {
		return showDryRunOutput(project, environment, release, showResources, logger)
	}

	// Show confirmation unless force flag is used
	if !force {
		var confirmed bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Delete project '%s' in environment '%s'?", project, environment)).
					Description(fmt.Sprintf("This will permanently remove project '%s' in environment '%s' and all its resources from the cluster.", project, environment)).
					Affirmative("Yes, delete it").
					Negative("No, cancel").
					Value(&confirmed),
			),
		)

		if err := confirmForm.Run(); err != nil {
			logger.Debugf("failed to get confirmation: %v", err)
			return fmt.Errorf("failed to get confirmation")
		}

		if !confirmed {
			logger.Infof("Delete operation for project '%s' in environment '%s' cancelled by user", project, environment)
			return nil
		}
	}

	err = spinner.New().
		Title(fmt.Sprintf("Deleting project '%s' in environment '%s'...", project, environment)).
		Context(cmd.Context()).
		ActionWithErr(func(ctx context.Context) error {
			return helmClient.DeleteRelease(ctx, project, environment)
		}).
		Run()
	if err != nil {
		return fmt.Errorf("failed to delete release: %w", err)
	}

	logger.Infof("Project '%s' in environment '%s' deleted successfully", project, environment)

	return nil
}

// showDryRunOutput displays detailed information about what would be deleted
func showDryRunOutput(project, environment string, release *v1.Release, showResources bool, logger *log.Logger) error {
	if release == nil {
		logger.Infof("🔍 DRY RUN: Project '%s' in environment '%s' not found - nothing to delete", project, environment)
		return nil
	}

	// Extract release information
	releaseName := release.Name
	namespace := release.Namespace
	status := "unknown"
	revision := 0
	lastDeployed := "unknown"

	if release.Info != nil {
		status = release.Info.Status.String()
		if !release.Info.LastDeployed.IsZero() {
			lastDeployed = release.Info.LastDeployed.Format("2006-01-02 15:04:05 MST")
		}
	}
	if release.Version > 0 {
		revision = int(release.Version)
	}

	// Display basic release information
	logger.Infof("🔍 DRY RUN: Would delete the following release:")
	logger.Infof("  📦 Project: %s", project)
	logger.Infof("  🌍 Environment: %s", environment)
	logger.Infof("  🏷️  Release Name: %s", releaseName)
	logger.Infof("  📂 Namespace: %s", namespace)
	logger.Infof("  📊 Status: %s", status)
	logger.Infof("  🔢 Revision: %d", revision)
	logger.Infof("  📅 Last Deployed: %s", lastDeployed)

	if release.Info != nil && release.Info.Description != "" {
		logger.Infof("  📝 Description: %s", release.Info.Description)
	}

	// Show manifest resources if requested or if it's available
	if showResources && release.Manifest != "" {
		logger.Infof("\n📋 Resources that would be deleted:")
		if err := displayResourcesSummary(release.Manifest, logger); err != nil {
			logger.Warnf("Could not parse manifest resources: %v", err)
		}
	}

	// Show configuration if available
	if len(release.Config) > 0 {
		logger.Infof("\n⚙️  Configuration values would be lost:")
		displayConfigSummary(release.Config, logger)
	}

	// Show notes if available
	if release.Info != nil && release.Info.Notes != "" {
		logger.Infof("\n📜 Release notes:")
		logger.Infof("%s", release.Info.Notes)
	}

	// Show warning for data loss
	logger.Infof("\n⚠️  WARNING: This operation will:")
	logger.Infof("  • Permanently delete all Kubernetes resources")
	logger.Infof("  • Remove the Helm release history")
	logger.Infof("  • Delete any persistent data (PVCs, ConfigMaps, Secrets)")
	logger.Infof("  • Cannot be undone without backups")

	logger.Infof("\n💡 To perform the actual deletion, run without --dry-run:")
	logger.Infof("  deployah delete %s %s", project, environment)

	return nil
}

// displayResourcesSummary parses and displays a summary of Kubernetes resources
func displayResourcesSummary(manifest string, logger *log.Logger) error {
	if manifest == "" {
		logger.Infof("  (No manifest data available)")
		return nil
	}

	// Simple manifest parsing to count resource types
	resourceCounts := make(map[string]int)
	lines := strings.Split(manifest, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "kind:") {
			kind := strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
			if kind != "" {
				resourceCounts[kind]++
			}
		}
	}

	if len(resourceCounts) == 0 {
		logger.Infof("  (Could not parse resource information)")
		return nil
	}

	for kind, count := range resourceCounts {
		if count == 1 {
			logger.Infof("  • %s", kind)
		} else {
			logger.Infof("  • %s (%d instances)", kind, count)
		}
	}

	return nil
}

// displayConfigSummary shows a summary of the configuration values
func displayConfigSummary(config map[string]any, logger *log.Logger) {
	if len(config) == 0 {
		logger.Infof("  (No configuration values)")
		return
	}

	count := 0
	for key, value := range config {
		if count >= 5 { // Limit to first 5 keys to avoid spam
			logger.Infof("  ... and %d more configuration keys", len(config)-5)
			break
		}

		// Show a preview of the value
		valueStr := fmt.Sprintf("%v", value)
		if len(valueStr) > 50 {
			valueStr = valueStr[:47] + "..."
		}

		logger.Infof("  • %s: %s", key, valueStr)
		count++
	}
}
