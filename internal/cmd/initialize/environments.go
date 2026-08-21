package initialize

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
)

// collectEnvironments collects the environments for the project.
func collectEnvironments(c *nabat.Context, config *ProjectConfig) error {
	useLocal, err := c.Confirm(
		promptLocalKind,
		nabat.WithAffirmative("Yes"),
		nabat.WithNegative("No"),
		nabat.WithPrefill(true),
	)
	if err != nil {
		return fmt.Errorf("failed to collect local environment choice: %w", err)
	}

	otherInput, err := c.Input(
		promptOtherEnvs,
		nabat.WithHint("staging, production"),
		nabat.WithDefault(""),
		nabat.WithValidate(func(input string) error {
			return validateOtherEnvironmentInput(input, useLocal)
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect other environments: %w", err)
	}

	return applyEnvironmentAnswers(config, useLocal, otherInput)
}

// validateOtherEnvironmentInput validates the input for other environments.
func validateOtherEnvironmentInput(input string, useLocal bool) error {
	var base []string
	if useLocal {
		base = []string{DefaultEnvironmentName}
	}
	parsed, err := parseEnvironmentNames(input, base)
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		return errors.New("at least one environment is required")
	}
	return nil
}

// applyEnvironmentAnswers applies the environment answers to the project config.
func applyEnvironmentAnswers(config *ProjectConfig, useLocal bool, otherInput string) error {
	var names []string
	if useLocal {
		names = []string{DefaultEnvironmentName}
	}
	names, err := parseEnvironmentNames(otherInput, names)
	if err != nil {
		return err
	}
	config.EnvironmentNames = names
	return nil
}

// parseEnvironmentNames appends the validated, deduplicated names from a
// comma-separated input to names.
func parseEnvironmentNames(input string, names []string) ([]string, error) {
	for raw := range strings.SplitSeq(input, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if err := spec.ValidateEnvName(name); err != nil {
			return nil, err
		}
		if slices.Contains(names, name) {
			return nil, fmt.Errorf("environment '%s' already exists", name)
		}
		names = append(names, name)
	}

	return names, nil
}

// collectEnvironmentVariables collects the environment variables for the project.
func collectEnvironmentVariables(c *nabat.Context) (map[string]string, error) {
	variables := make(map[string]string)

	for {
		var varName, varValue string
		err := c.Form(
			nabat.WithFormGroup(
				nabat.WithGroupTitle("Environment Variable"),
				nabat.WithFormField(&varName, "Variable Name",
					"Environment variable name (uppercase with underscores)",
					nabat.WithHint("APP_ENV"),
					nabat.WithValidate(spec.ValidateEnvVarName),
				),
				nabat.WithFormField(&varValue, "Variable Value",
					"Value for the environment variable",
					nabat.WithHint("production"),
				),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to collect variable details: %w", err)
		}
		if varName != "" {
			variables[varName] = varValue
		}

		addAnother, confirmErr := c.Confirm(
			"Add another variable?",
			nabat.WithAffirmative("Yes"),
			nabat.WithNegative("No, I'm done"),
			nabat.WithDefault(false),
		)
		if confirmErr != nil {
			return nil, fmt.Errorf("failed to confirm another variable: %w", confirmErr)
		}
		if !addAnother {
			return variables, nil
		}
	}
}
