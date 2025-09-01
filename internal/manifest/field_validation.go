// Package manifest provides functions for parsing and manipulating manifest files.
package manifest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"github.com/deployah-dev/deployah/internal/manifest/schema"
)

// fieldValidators holds compiled regex patterns extracted from the JSON schema
type fieldValidators struct {
	projectNamePattern   *regexp.Regexp
	componentNamePattern *regexp.Regexp
	envNamePattern       *regexp.Regexp
	envVarNamePattern    *regexp.Regexp
}

var (
	validators *fieldValidators
	once       sync.Once
)

// initValidators extracts regex patterns from the JSON schema and compiles them
func initValidators() error {
	// Get the latest schema to extract patterns from
	schemaBytes, err := schema.GetLatestManifestSchema()
	if err != nil {
		return fmt.Errorf("failed to get latest manifest schema: %w", err)
	}

	var schemaData map[string]interface{}
	if err := json.Unmarshal(schemaBytes, &schemaData); err != nil {
		return fmt.Errorf("failed to parse manifest schema: %w", err)
	}

	// Extract project name pattern
	projectPattern, err := extractPattern(schemaData, []string{"properties", "project", "pattern"})
	if err != nil {
		return fmt.Errorf("failed to extract project name pattern: %w", err)
	}

	// Extract component name pattern
	componentPattern, err := extractPattern(schemaData, []string{"properties", "components", "propertyNames", "pattern"})
	if err != nil {
		return fmt.Errorf("failed to extract component name pattern: %w", err)
	}

	// Extract environment name pattern
	envPattern, err := extractPattern(schemaData, []string{"properties", "environments", "items", "properties", "name", "pattern"})
	if err != nil {
		return fmt.Errorf("failed to extract environment name pattern: %w", err)
	}

	// Extract environment variable name pattern
	envVarPattern, err := extractPattern(schemaData, []string{"properties", "environments", "items", "properties", "variables", "propertyNames", "pattern"})
	if err != nil {
		return fmt.Errorf("failed to extract environment variable name pattern: %w", err)
	}

	validators = &fieldValidators{
		projectNamePattern:   regexp.MustCompile(projectPattern),
		componentNamePattern: regexp.MustCompile(componentPattern),
		envNamePattern:       regexp.MustCompile(envPattern),
		envVarNamePattern:    regexp.MustCompile(envVarPattern),
	}

	return nil
}

// extractPattern navigates through nested map structure to extract a pattern string
func extractPattern(data map[string]interface{}, path []string) (string, error) {
	current := data
	for i, key := range path {
		value, exists := current[key]
		if !exists {
			return "", fmt.Errorf("key '%s' not found at path %v", key, path[:i+1])
		}

		if i == len(path)-1 {
			// Last element should be the pattern string
			pattern, ok := value.(string)
			if !ok {
				return "", fmt.Errorf("pattern at path %v is not a string", path)
			}
			return pattern, nil
		}

		// Continue navigating
		nextMap, ok := value.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("value at path %v is not a map", path[:i+1])
		}
		current = nextMap
	}

	return "", fmt.Errorf("empty path provided")
}

// getValidators ensures validators are initialized and returns them
func getValidators() (*fieldValidators, error) {
	var err error
	once.Do(func() {
		err = initValidators()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validators: %w", err)
	}
	return validators, nil
}

// ValidateProjectName validates a project name against the JSON schema pattern
func ValidateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	v, err := getValidators()
	if err != nil {
		return fmt.Errorf("failed to initialize validators: %w", err)
	}

	if !v.projectNamePattern.MatchString(name) {
		return fmt.Errorf("project name '%s' is invalid: must be lowercase alphanumeric characters or dashes (-) separated and cannot start or end with a dash (-)", name)
	}
	return nil
}

// ValidateComponentName validates a component name against the JSON schema pattern
func ValidateComponentName(name string) error {
	if name == "" {
		return fmt.Errorf("component name cannot be empty")
	}

	v, err := getValidators()
	if err != nil {
		return fmt.Errorf("failed to initialize validators: %w", err)
	}

	if !v.componentNamePattern.MatchString(name) {
		return fmt.Errorf("component name '%s' is invalid: must be lowercase letters, numbers, and dashes only", name)
	}
	return nil
}

// ValidateEnvName validates an environment name against the JSON schema pattern
func ValidateEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("environment name cannot be empty")
	}

	v, err := getValidators()
	if err != nil {
		return fmt.Errorf("failed to initialize validators: %w", err)
	}

	if !v.envNamePattern.MatchString(name) {
		return fmt.Errorf("environment name '%s' is invalid: must match pattern ^[a-z0-9]+(?:-[a-z0-9]+)*(?:/\\*)?$", name)
	}
	return nil
}

// ValidateEnvVarName validates an environment variable name against the JSON schema pattern
func ValidateEnvVarName(name string) error {
	if name == "" {
		return fmt.Errorf("variable name cannot be empty")
	}

	v, err := getValidators()
	if err != nil {
		return fmt.Errorf("failed to initialize validators: %w", err)
	}

	if !v.envVarNamePattern.MatchString(name) {
		return fmt.Errorf("variable name '%s' is invalid: must be uppercase letters, numbers, and underscores only", name)
	}
	return nil
}

// ValidateHostname validates a hostname against the JSON schema pattern
func ValidateHostname(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname cannot be empty")
	}

	// Hostname pattern: optionally starts with *. followed by domain parts
	// Each domain part contains alphanumeric characters and hyphens
	// Ends with a TLD of at least 2 characters
	hostnamePattern := `^(\*\.)?([a-zA-Z0-9-]+\.)+[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(hostnamePattern, hostname)
	if err != nil {
		return fmt.Errorf("failed to validate hostname: %w", err)
	}

	if !matched {
		return fmt.Errorf("hostname '%s' is invalid: must be a valid hostname (e.g., 'api.example.com' or '*.example.com')", hostname)
	}

	return nil
}

// ValidatePort validates that the port number is a valid number between 1024 and 65535
func ValidatePort(portStr string) error {
	if portStr == "" {
		return fmt.Errorf("port cannot be empty")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("port '%s' is invalid: must be a valid number", portStr)
	}
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port %d is invalid: must be between 1024 and 65535", port)
	}
	return nil
}
