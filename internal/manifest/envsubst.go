// Package manifest provides functions for parsing and manipulating manifest files.
package manifest

import (
	"fmt"

	"dario.cat/mergo"
	"github.com/fluxcd/pkg/envsubst"
)

// SubstituteVariables substitutes variables in the manifest data using the environment variables.
// The variables are substituted in the following order:
// 1. Variables from the environment definition (lowest priority)
// 2. Variables from the env file (medium priority)
// 3. Variables from the OS environment variables (highest priority)
// The variables are substituted using the envsubst package.
func SubstituteVariables(data []byte, env *Environment) ([]byte, error) {
	variables := make(map[string]string)

	// Merge variables from the environment definition (lowest priority)
	err := mergo.Merge(&variables, env.Variables)
	if err != nil {
		return nil, fmt.Errorf("failed to merge environment definition variables: %w", err)
	}

	// Load env file (medium priority), if explicitly set
	varsFromFile, err := parseEnvFile(env.EnvFile, env.EnvFile != "")
	if err != nil {
		return nil, fmt.Errorf("failed to parse environment file: %w", err)
	}

	// Filter variables, deployahVars (medium priority)
	deployahVarsFromFile, _ := filterVariables(varsFromFile)
	err = mergo.Merge(&variables, deployahVarsFromFile, mergo.WithOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to merge env file variables: %w", err)
	}

	// Load OS environment variables (highest priority)
	osEnvVars, err := parseOSVariables()
	if err != nil {
		return nil, fmt.Errorf("failed to parse OS environment variables: %w", err)
	}

	// Filter variables, deployahVars (highest priority)
	deployahVarsFromOS, _ := filterVariables(osEnvVars)

	// Merge variables (highest priority)
	err = mergo.Merge(&variables, deployahVarsFromOS, mergo.WithOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to merge OS environment variables: %w", err)
	}

	content, err := envsubst.Eval(string(data), func(s string) (string, bool) {
		if v, ok := variables[s]; ok {
			return v, true
		}
		return "", false
	})

	if err != nil {
		return nil, fmt.Errorf("failed to substitute variables: %w", err)
	}

	return []byte(content), nil
}
