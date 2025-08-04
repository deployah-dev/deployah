package cmd

import (
	"fmt"

	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/spf13/cobra"
)

// NewValidateCommand creates the validate sub-command for the CLI.
func NewValidateCommand() *cobra.Command {
	validateCommand := &cobra.Command{
		Use:   "validate [flags]",
		Short: "Validate a Deployah manifest against the JSON schema",
		RunE:  runValidate,
	}

	validateCommand.Flags().StringP("file", "f", ".deployah.yaml", "Path to the manifest file (YAML or JSON)")
	validateCommand.Flags().StringP("environment", "e", "", "Environment to validate against")

	return validateCommand
}

func runValidate(cmd *cobra.Command, args []string) error {
	logger := GetLogger(cmd)

	manifestPath, err := cmd.Flags().GetString("file")
	if err != nil {
		return fmt.Errorf("failed to get manifest file: %w", err)
	}

	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	m, err := manifest.Load(manifestPath, environment)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	logger.Info("Project found", "project", m.Project)
	logger.Info("Manifest validated successfully")

	return nil
}
