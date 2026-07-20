// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"deployah.dev/deployah/internal/spec/schema"
)

// fieldValidators holds compiled regex patterns extracted from the JSON schema
type fieldValidators struct {
	projectNamePattern   *regexp.Regexp
	componentNamePattern *regexp.Regexp
	envNamePattern       *regexp.Regexp
	envVarNamePattern    *regexp.Regexp
}

var (
	validators    *fieldValidators
	validatorsErr error
	once          sync.Once
)

// initValidators extracts regex patterns from the JSON schema and compiles them
func initValidators() error {
	// Get the latest schema to extract patterns from
	schemaBytes, err := schema.GetLatestManifestSchema()
	if err != nil {
		return fmt.Errorf("failed to get latest spec schema: %w", err)
	}

	var schemaData map[string]any
	if err = json.Unmarshal(schemaBytes, &schemaData); err != nil {
		return fmt.Errorf("failed to parse spec schema: %w", err)
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

	// Extract environment name pattern. v1-alpha.2 models "environments" as
	// an object keyed by environment name, so the pattern lives on
	// propertyNames rather than on an array item's "name" field.
	envPattern, err := extractPattern(schemaData, []string{"properties", "environments", "propertyNames", "pattern"})
	if err != nil {
		return fmt.Errorf("failed to extract environment name pattern: %w", err)
	}

	// Extract environment variable name pattern. Environment values are
	// defined via the $defs/Environment ref, so navigate there directly
	// rather than through "properties.environments" (which only has
	// propertyNames, not a nested "variables" field).
	envVarPattern, err := extractPattern(schemaData, []string{"$defs", "Environment", "properties", "variables", "propertyNames", "pattern"})
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

// extractPattern navigates a nested map structure to extract a pattern
// string.
func extractPattern(data map[string]any, path []string) (string, error) {
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
		nextMap, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("value at path %v is not a map", path[:i+1])
		}
		current = nextMap
	}

	return "", fmt.Errorf("empty path provided")
}

// getValidators ensures validators are initialized and returns them. The
// initialization error (if any) is cached in validatorsErr so repeated calls
// after a failed first attempt keep returning the error instead of a nil
// validators pointer (sync.Once only runs initValidators once, regardless of
// outcome).
func getValidators() (*fieldValidators, error) {
	once.Do(func() {
		validatorsErr = initValidators()
	})
	if validatorsErr != nil {
		return nil, fmt.Errorf("failed to initialize validators: %w", validatorsErr)
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

// SanitizeProjectName rewrites s to satisfy [ValidateProjectName]'s pattern
// (the result may still be too short; callers should check that separately).
func SanitizeProjectName(s string) string {
	var b strings.Builder
	needDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if needDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			needDash = false
		default:
			// Collapse this and any following invalid run into one dash.
			needDash = true
		}
	}
	return b.String()
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

	// Both the manifest and platform schemas require at least 2 characters.
	if len(name) < 2 {
		return fmt.Errorf("environment name '%s' is invalid: must be at least 2 characters", name)
	}

	v, err := getValidators()
	if err != nil {
		return fmt.Errorf("failed to initialize validators: %w", err)
	}

	if !v.envNamePattern.MatchString(name) {
		return fmt.Errorf("environment name '%s' is invalid: must be lowercase alphanumeric characters or dashes (-) separated and cannot start or end with a dash (-)", name)
	}
	return nil
}

// ValidateEnvVarName validates an environment variable name against the JSON
// schema pattern.
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

// ValidatePort validates that the port is a number between 1024 and 65535.
func ValidatePort(portStr string) error {
	if portStr == "" {
		return fmt.Errorf("port cannot be empty")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("port '%s' is invalid: must be a valid number", portStr)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d is invalid: must be between 1 and 65535", port)
	}
	return nil
}
