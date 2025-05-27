package manifest

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"dario.cat/mergo"
	"github.com/fluxcd/pkg/envsubst"
	"github.com/joho/godotenv"
)

const (
	// DPY_VAR_ prefix is used for variables that are used for template rendering
	DEPLOYAH_VARIABLE_PREFIX = "DPY_VAR_"
)

// parseEnvFile parses .env files.
//
// If explicitlySet is true, the function will return an error if the file does not exist.
// If explicitlySet is false, the function will return nil if the file does not exist.
func parseEnvFile(path string, explicitlySet bool) (map[string]string, error) {
	// Read the .env file.
	data, err := os.ReadFile(path)
	if err != nil {
		if explicitlySet {
			return nil, fmt.Errorf("failed to read %s file: %w", path, err)
		}
		return nil, nil
	}

	// Parse the .env file.
	vars, err := godotenv.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s file: %w", path, err)
	}

	return vars, nil
}

func parseOSVariables() (map[string]string, error) {
	env := os.Environ()
	buf := bytes.NewBufferString(strings.Join(env, "\n"))
	buf.WriteString("\n")

	vars, err := godotenv.Parse(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OS environment variables: %w", err)
	}

	return vars, nil
}

// filterVariables separates variables into two maps based on their prefix.
// - deployahVars: variables prefixed with DPY_VAR_ used for template rendering
// - envVars: regular environment variables passed to containers
//
// The DPY_VAR_ prefix is stripped from the keys in the returned deployahVars map.
func filterVariables(vars map[string]string) (map[string]string, map[string]string) {
	deployahVars := make(map[string]string)
	envVars := make(map[string]string)

	for key, value := range vars {
		if strings.HasPrefix(key, DEPLOYAH_VARIABLE_PREFIX) {
			deployahVars[strings.TrimPrefix(key, DEPLOYAH_VARIABLE_PREFIX)] = value
		} else {
			envVars[key] = value
		}
	}

	return deployahVars, envVars
}

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
	deployahVarsFromFile, err := parseEnvFile(env.EnvFile, env.EnvFile != "")
	if err != nil {
		return nil, err
	}

	// Filter variables, deployahVars (medium priority)
	envVarsFromFile, _ := filterVariables(deployahVarsFromFile)
	err = mergo.Merge(&variables, envVarsFromFile, mergo.WithOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to merge env file variables: %w", err)
	}

	// Load OS environment variables (highest priority)
	osEnvVars, err := parseOSVariables()
	if err != nil {
		return nil, err
	}

	// Filter variables, deployahVars (highest priority)
	deployahVarsFromOS, _ := filterVariables(osEnvVars)

	// Merge variables (highest priority)
	err = mergo.Merge(&variables, deployahVarsFromOS, mergo.WithOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to merge OS environment variables: %w", err)
	}

	content, err := envsubst.Eval(string(data), func(s string) (string, bool) {
		return variables[s], true
	})

	if err != nil {
		return nil, fmt.Errorf("failed to substitute variables: %w", err)
	}

	return []byte(content), nil
}
