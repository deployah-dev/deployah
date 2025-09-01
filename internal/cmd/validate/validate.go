package validate

import (
	"fmt"

	"github.com/deployah-dev/deployah/internal/logging"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/spf13/cobra"
)

// New creates the validate sub-command for the CLI.
func New() *cobra.Command {
	validateCommand := &cobra.Command{
		Use:   "validate <environment>",
		Short: "Validate a Deployah manifest for a specific environment",
		Long:  `Validate a Deployah manifest for a specific environment against the JSON schema`,
		Example: `
# Validate a manifest for a specific environment using default manifest path (./deployah.yaml)
deployah validate production

# Validate a manifest for a specific environment with an explicit manifest path
deployah validate staging -f ./path/to/deployah.yaml
`,
		RunE: runValidate,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("you must specify exactly one environment (e.g., 'deployah validate production'). Received %d argument(s): %v", len(args), args)
			}
			return nil
		},
	}

	return validateCommand
}

func runValidate(cmd *cobra.Command, args []string) error {
	logger := logging.GetLogger(cmd)

	// Get runtime and extract manifest path
	rt := runtime.FromRuntime(cmd.Context())
	if rt == nil {
		return fmt.Errorf("runtime not initialized")
	}

	environment := args[0]

	m, err := rt.Manifest(cmd.Context(), environment)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	logger.Info("Project found", "project", m.Project)
	logger.Info("Manifest validated successfully")

	return nil
}
