package initialize

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
)

// projectNameMinLength mirrors the manifest schema's minLength for
// "project" (see internal/spec/schema/*/manifest.json properties.project),
// used to reject a sanitized directory name that is too short to be valid
// before it reaches schema validation.
const projectNameMinLength = 3

// defaultProjectNameFallback is used when the current directory name does
// not sanitize into a valid project name (e.g. too short, or made only of
// characters outside [a-z0-9-]).
const defaultProjectNameFallback = "my-project"

// resolveProjectName implements the flag > prompt > default resolution
// order for the project name.
func resolveProjectName(c *nabat.Context, config *ProjectConfig, flagProject string, useDefaults bool) error {
	if flagProject != "" {
		// --project is validated immediately and used as-is, skipping the
		// prompt entirely.
		if err := spec.ValidateProjectName(flagProject); err != nil {
			return fmt.Errorf("--project: %w", err)
		}
		config.Name = flagProject
		return nil
	}

	if useDefaults {
		// --defaults, or no TTY to prompt on: fall back to the sanitized
		// current directory name.
		config.Name = defaultProjectName()
		return nil
	}

	return collectProjectName(c, config)
}

func collectProjectName(c *nabat.Context, config *ProjectConfig) error {
	name, err := c.Input(
		StepProjectName+" — Project Name",
		nabat.WithHint("my-awesome-project"),
		nabat.WithDefault(config.Name),
		nabat.WithValidate(spec.ValidateProjectName),
	)
	if err != nil {
		return err
	}
	config.Name = name
	return nil
}

// defaultProjectName returns the sanitized current working directory name,
// falling back to defaultProjectNameFallback when the directory can't be
// read or its name doesn't sanitize into a valid project name.
func defaultProjectName() string {
	dir, err := os.Getwd()
	if err != nil {
		return defaultProjectNameFallback
	}

	name := spec.SanitizeProjectName(filepath.Base(dir))
	if len(name) < projectNameMinLength {
		return defaultProjectNameFallback
	}
	if err = spec.ValidateProjectName(name); err != nil {
		return defaultProjectNameFallback
	}
	return name
}

func validateEnvironmentNameUnique(name string, existing []string) error {
	if err := spec.ValidateEnvName(name); err != nil {
		return fmt.Errorf("failed to validate environment name: %w", err)
	}
	if slices.Contains(existing, name) {
		return fmt.Errorf("environment '%s' already exists", name)
	}
	return nil
}
