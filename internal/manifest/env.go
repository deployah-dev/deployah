package manifest

import (
	"fmt"
	"os"
	"strings"
)

// parseEnvFile parses .env files.
//
// If explicitlySet is true, the function will return an error if the file does not exist.
// If explicitlySet is false, the function will return nil if the file does not exist.
func parseEnvFile(path string, explicitlySet bool) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Use os.IsNotExist to check for file existence (Go best practice)
		if os.IsNotExist(err) {
			if explicitlySet {
				return nil, fmt.Errorf("failed to read %s file: %w", path, err)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read %s file: %w", path, err)
	}

	vars := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			vars[key] = val
		}
	}

	return vars, nil
}

// parseOSVariables parses the current OS environment variables directly from os.Environ().
func parseOSVariables() (map[string]string, error) {
	env := os.Environ()
	vars := make(map[string]string)
	for _, e := range env {
		if idx := strings.Index(e, "="); idx != -1 {
			key := e[:idx]
			val := e[idx+1:]
			vars[key] = val
		}
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
