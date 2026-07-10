package initialize

import (
	"errors"
	"fmt"
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
)

// resolveEnvironments implements the flag > prompt > default resolution
// order for the project's environments.
func resolveEnvironments(c *nabat.Context, config *ProjectConfig, flagEnvironments []string, useDefaults bool) error {
	if len(flagEnvironments) > 0 {
		// --environments values are validated immediately and used as-is,
		// skipping the prompt entirely.
		names := make([]string, 0, len(flagEnvironments))
		for _, raw := range flagEnvironments {
			name := strings.TrimSpace(raw)
			if err := validateEnvironmentNameUnique(name, names); err != nil {
				return fmt.Errorf("--environments: %w", err)
			}
			names = append(names, name)
		}
		config.EnvironmentNames = names
		return nil
	}

	if useDefaults {
		// --defaults, or no TTY to prompt on.
		config.EnvironmentNames = []string{DefaultEnvironmentName}
		return nil
	}

	return collectEnvironments(c, config)
}

// collectEnvironments asks whether to set up the local environment, then
// collects any other environment names from one comma-separated input.
// When the local environment is declined, the input requires at least one
// name; the rule is enforced inline so no warning outlives the step.
func collectEnvironments(c *nabat.Context, config *ProjectConfig) error {
	// A bound form field is used instead of c.Confirm: the ad-hoc Confirm
	// only applies WithDefault as its non-interactive fallback, so it
	// always starts on "No"; a form field starts on the target's value.
	useLocal := true
	err := c.Form(
		nabat.WithFormGroup(
			nabat.WithFormField(&useLocal,
				StepEnvironments+" — Set up a local environment?",
				"Creates a kind cluster config; matches 'deployah cluster up'",
				nabat.WithAffirmative("Yes"),
				nabat.WithNegative("No"),
			),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to collect local environment choice: %w", err)
	}

	names := []string{}
	prompt := "Other environments — comma-separated names, leave empty to skip. "
	if useLocal {
		names = append(names, DefaultEnvironmentName)
	} else {
		prompt = "Environments — comma-separated names, at least one is required. "
	}

	otherInput, err := c.Input(
		prompt+"A name like 'review' also matches 'review/pr-123' at deploy time via prefix matching.",
		nabat.WithHint("staging, production"),
		nabat.WithDefault(""),
		// Inline so a bad answer re-prompts instead of aborting the wizard.
		nabat.WithValidate(func(input string) error {
			parsed, parseErr := parseEnvironmentNames(input, names)
			if parseErr != nil {
				return parseErr
			}
			if len(parsed) == 0 {
				return errors.New("at least one environment is required")
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to collect environment names: %w", err)
	}

	names, err = parseEnvironmentNames(otherInput, names)
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
		if base, cut := strings.CutSuffix(name, "/*"); cut {
			return nil, fmt.Errorf("invalid environment name %q: the \"/*\" suffix is not supported; a plain name like %q already matches %q at deploy time",
				name, base, base+"/pr-123")
		}
		if err := validateEnvironmentNameUnique(name, names); err != nil {
			return nil, fmt.Errorf("invalid environment name %q: %w", name, err)
		}
		names = append(names, name)
	}

	return names, nil
}

func collectEnvironmentVariables(c *nabat.Context) (map[string]string, error) {
	variables := make(map[string]string)
	continueAdding := true

	for continueAdding {
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
			nabat.WithFormGroup(
				nabat.WithFormField(&continueAdding, "Add another variable?", "",
					nabat.WithAffirmative("Yes"),
					nabat.WithNegative("No, I'm done"),
				),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to collect variable details: %w", err)
		}

		if varName != "" {
			variables[varName] = varValue
		}
	}

	return variables, nil
}
