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
	manifestPath, err := cmd.Flags().GetString("file")
	if err != nil {
		return fmt.Errorf("failed to get manifest path: %w", err)
	}

	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	manifest, err := manifest.Load(manifestPath, environment)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	yamlManifest, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest to YAML: %w", err)
	}
	fmt.Println(string(yamlManifest))
	fmt.Println("--------------------------------")

	values, err := helm.MapManifestToChartValues(manifest)
	if err != nil {
		return fmt.Errorf("failed to map manifest to chart values: %w", err)
	}

	yamlData, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("failed to marshal values to YAML: %w", err)
	}

	fmt.Println(string(yamlData))

	return nil
}
