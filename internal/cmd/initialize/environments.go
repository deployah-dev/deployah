package initialize

import (
	"fmt"
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/spec"
)

func collectEnvironments(c *nabat.Context, config *ProjectConfig) error {
	var selectedEnvironments []string
	continueAdding := true

	for continueAdding {
		var envName string
		var addAnother bool

		err := c.Form(
			nabat.WithFormTitle(StepEnvironments),
			nabat.WithFormField(&envName, "Environment Name",
				"Enter the name of an environment (e.g., development, staging, production, review/*)",
				nabat.WithHint("dev"),
				nabat.WithValidate(func(s string) error {
					return validateEnvironmentNameUnique(s, selectedEnvironments)
				}),
			),
			nabat.WithFormField(&addAnother, "Add another environment?", "",
				nabat.WithAffirmative("Yes, add another"),
				nabat.WithNegative("No, I'm done"),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to collect environment name: %w", err)
		}

		if envName != "" {
			selectedEnvironments = append(selectedEnvironments, envName)
		}

		continueAdding = addAnother
	}

	for _, envName := range selectedEnvironments {
		env, err := collectEnvironmentDetails(c, envName)
		if err != nil {
			return fmt.Errorf("failed to collect details for environment %s: %w", envName, err)
		}
		config.Environments[envName] = env
	}

	return nil
}

func collectEnvironmentDetails(c *nabat.Context, envName string) (spec.Environment, error) {
	env := spec.Environment{}

	var addVariables bool
	err := c.Form(
		nabat.WithFormGroup(
			nabat.WithGroupTitle(fmt.Sprintf("Environment Details for %s", envName)),
			nabat.WithFormField(&env.EnvFile, fmt.Sprintf("Environment File for %s", envName),
				"Path to the environment file (optional, defaults to .env.{name})",
				nabat.WithHint(fmt.Sprintf(".env.%s", strings.TrimSuffix(envName, "/*"))),
			),
			nabat.WithFormField(&env.ConfigFile, fmt.Sprintf("Config File for %s", envName),
				"Path to the configuration file (optional, defaults to config.{name}.yaml)",
				nabat.WithHint(fmt.Sprintf("config.%s.yaml", strings.TrimSuffix(envName, "/*"))),
			),
		),
		nabat.WithFormGroup(
			nabat.WithFormField(&addVariables, fmt.Sprintf("Add custom variables for %s?", envName),
				"You can add environment-specific variables that will be used as template variables.",
				nabat.WithAffirmative("Yes, add variables"),
				nabat.WithNegative("No, skip variables"),
			),
		),
	)
	if err != nil {
		return env, fmt.Errorf("failed to collect environment details: %w", err)
	}

	if addVariables {
		variables, varErr := collectEnvironmentVariables(c)
		if varErr != nil {
			return env, fmt.Errorf("failed to collect variables for environment %s: %w", envName, varErr)
		}
		env.Variables = variables
	}

	return env, nil
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

		if varName != "" && varValue != "" {
			variables[varName] = varValue
		}
	}

	return variables, nil
}
