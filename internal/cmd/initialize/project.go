package initialize

import (
	"fmt"
	"slices"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
)

func collectProjectName(c *nabat.Context, config *ProjectConfig) error {
	return c.Form(
		nabat.WithFormTitle(StepProjectName),
		nabat.WithFormField(&config.Name, "Project Name",
			"What is the name of your project? Use lowercase letters, numbers, and dashes only.",
			nabat.WithHint("my-awesome-project"),
			nabat.WithValidate(spec.ValidateProjectName),
		),
	)
}

func validateEnvironmentNameUnique(name string, existing []string) error {
	if err := spec.ValidateEnvName(name); err != nil {
		return fmt.Errorf("failed to validate environment name: %w", err)
	}
	if contains(existing, name) {
		return fmt.Errorf("environment '%s' already exists", name)
	}
	return nil
}

func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}
