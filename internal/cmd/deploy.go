package cmd

import (
	"fmt"

	"github.com/deployah-dev/deployah/internal/helm"
	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// NewDeployCommand creates the deploy sub-command for the CLI.
func NewDeployCommand() *cobra.Command {
	deployCommand := &cobra.Command{
		Use:   "deploy [flags]",
		Short: "Deploy a project to a Kubernetes cluster on a given environment",
		Long:  `Deploy a project to a Kubernetes cluster on a given environment.`,
		RunE:  runDeploy,
	}

	deployCommand.Flags().StringP("file", "f", ".deployah.yaml", "Path to the manifest file (YAML or JSON)")
	deployCommand.Flags().StringP("environment", "e", "", "Environment to deploy to")

	return deployCommand
}

func runDeploy(cmd *cobra.Command, args []string) error {
	logger := GetLogger(cmd)

	logger.Info("Starting deployment process", "cmd", "deploy")

	manifestPath, err := cmd.Flags().GetString("file")
	if err != nil {
		logger.Error("Failed to get manifest path", "err", err)
		return fmt.Errorf("failed to get manifest path: %w", err)
	}

	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		logger.Error("Failed to get environment", "err", err)
		return fmt.Errorf("failed to get environment: %w", err)
	}

	logger.Info("Loading manifest", "file", manifestPath, "env", environment)

	manifest, err := manifest.Load(manifestPath, environment)
	if err != nil {
		logger.Error("Failed to load manifest", "file", manifestPath, "env", environment, "err", err)
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	logger.Info("Manifest loaded successfully", "file", manifestPath, "env", environment)

	yamlManifest, err := yaml.Marshal(manifest)
	if err != nil {
		logger.Error("Failed to marshal manifest to YAML", "err", err)
		return fmt.Errorf("failed to marshal manifest to YAML: %w", err)
	}

	logger.Debug("Manifest YAML", "yaml", string(yamlManifest))
	logger.Info("Manifest processed", "separator", "--------------------------------")

	logger.Info("Mapping manifest to chart values")
	values, err := helm.MapManifestToChartValues(manifest)
	if err != nil {
		logger.Error("Failed to map manifest to chart values", "err", err)
		return fmt.Errorf("failed to map manifest to chart values: %w", err)
	}

	yamlData, err := yaml.Marshal(values)
	if err != nil {
		logger.Error("Failed to marshal values to YAML", "err", err)
		return fmt.Errorf("failed to marshal values to YAML: %w", err)
	}

	logger.Debug("Chart values YAML", "yaml", string(yamlData))
	logger.Info("Deployment process completed successfully")

	return nil
}
