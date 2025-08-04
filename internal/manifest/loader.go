// Package manifest provides functions for parsing and manipulating manifest files.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"

	"sigs.k8s.io/yaml"
)

const (
	DefaultManifestPath = ".deployah.yaml"
)

// sanitizeEnvName removes path separators and wildcards from environment names
// to prevent them from interfering with file path construction.
func sanitizeEnvName(name string) string {
	// Remove wildcard suffix patterns like "/*"
	sanitized := strings.TrimSuffix(name, "/*")

	// Remove any remaining path separators or wildcards
	sanitized = strings.ReplaceAll(sanitized, "/", "")
	sanitized = strings.ReplaceAll(sanitized, "\\", "")
	sanitized = strings.ReplaceAll(sanitized, "*", "")
	sanitized = strings.ReplaceAll(sanitized, "?", "")

	return sanitized
}

// ResolveEnvironment returns the environment by name, or the default if name is empty.
// Returns an error if not found or if multiple environments are defined but none specified.
func ResolveEnvironment(environments []Environment, desiredEnvironment string) (*Environment, error) {
	// Helper function to format environment names for error messages
	formatEnvNames := func(envs []Environment) string {
		names := make([]string, len(envs))
		for i, env := range envs {
			names[i] = fmt.Sprintf("%q", env.Name)
		}
		return strings.Join(names, ", ")
	}

	// If no environments are defined, and no environment is specified, use the default environment.
	if len(environments) == 0 && desiredEnvironment == "" {
		return &Environment{
			Name:       "default",
			EnvFile:    DefaultEnvFile,
			ConfigFile: DefaultConfigFile,
		}, nil
	}

	// If only one environment is defined, and no environment is specified, use it.
	if len(environments) == 1 && desiredEnvironment == "" {
		return &environments[0], nil
	}

	// If multiple environments are defined, and no environment is specified, return an error.
	if len(environments) > 1 && desiredEnvironment == "" {
		return nil, fmt.Errorf(
			"multiple environments found but none specified: %s",
			formatEnvNames(environments),
		)
	}

	// Find the specified environment
	for i, env := range environments {
		if env.Name == desiredEnvironment {
			return &environments[i], nil
		}
	}

	return nil, fmt.Errorf(
		"environment %q not found, available environments: %s",
		desiredEnvironment,
		formatEnvNames(environments),
	)
}

// resolveEnvFile determines which env file to use for the given environment, following Deployah's resolution order.
// Returns the path, whether it was explicitly set, and an error if explicitly set but missing.
func resolveEnvFile(env *Environment) (string, bool, error) {
	if env.EnvFile != "" {
		if fileExists(env.EnvFile) {
			return env.EnvFile, true, nil
		}
		return "", true, fmt.Errorf("explicit envFile %q does not exist", env.EnvFile)
	}

	// Sanitize the environment name to prevent path separators and wildcards
	// from interfering with file path construction
	sanitizedName := sanitizeEnvName(env.Name)

	candidates := []string{
		fmt.Sprintf(".env.%s", sanitizedName),
		filepath.Join(".deployah", fmt.Sprintf(".env.%s", sanitizedName)),
		".env",
		filepath.Join(".deployah", ".env"),
	}
	for _, path := range candidates {
		if fileExists(path) {
			return path, false, nil
		}
	}
	return "", false, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Save writes the manifest to a YAML file at the specified path.
func Save(manifest *Manifest, path string) error {
	if path == "" {
		path = DefaultManifestPath
	}

	// Marshal the manifest to YAML
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest to YAML: %w", err)
	}

	// Create the directory if it doesn't exist
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write the file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest to %s: %w", path, err)
	}

	return nil
}

// Load reads and parses the manifest YAML file at the given path, resolves the environment
// (using the provided envName or default resolution rules), and substitutes variables
// according to precedence (environment definition < env file < OS environment).
// Returns the fully parsed Manifest struct or an error.
func Load(path string, envName string) (*Manifest, error) {
	if path == "" {
		path = DefaultManifestPath
	}

	// Read the manifest file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Unmarshal into map[string]any for validation.
	var manifestObj map[string]any
	if err := yaml.Unmarshal(data, &manifestObj); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	// Validate API version.
	version, err := ValidateAPIVersion(manifestObj)
	if err != nil {
		return nil, err
	}

	// Validate the environments against the schema
	if err := ValidateEnvironments(manifestObj, version); err != nil {
		return nil, fmt.Errorf("environments validation failed: %w", err)
	}

	var tmp struct {
		Environments []Environment `yaml:"environments"`
	}
	if err := yaml.Unmarshal(data, &tmp); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	// Select the correct environment
	env, err := ResolveEnvironment(tmp.Environments, envName)
	if err != nil {
		return nil, fmt.Errorf("failed to select environment: %w", err)
	}

	log.Infof("Selected environment: %s", env.Name)

	envFilePath, explicitlySet, err := resolveEnvFile(env)
	if err != nil {
		return nil, err
	}
	if envFilePath != "" {
		if explicitlySet {
			log.Infof("Using explicily set env file: %s", envFilePath)
		} else {
			log.Infof("Using resolved env file: %s", envFilePath)
		}
	} else {
		log.Infof("No env file found for environment '%s', proceeding without env file.", env.Name)
	}

	// Set the resolved env file path for substitution
	env.EnvFile = envFilePath

	// Substitute variables in the manifest YAML
	substituted, err := SubstituteVariables(data, env)
	if err != nil {
		return nil, fmt.Errorf("failed to substitute variables: %w", err)
	}

	// Unmarshal substituted manifest for final validation
	var substitutedObj map[string]any
	if err := yaml.Unmarshal([]byte(substituted), &substitutedObj); err != nil {
		return nil, fmt.Errorf("failed to parse substituted manifest YAML: %w", err)
	}

	// Validate the manifest against the schema
	if err := ValidateManifest(substitutedObj, version); err != nil {
		return nil, fmt.Errorf("manifest validation failed: %w", err)
	}

	// Unmarshal into the Manifest struct
	var finalManifest Manifest
	if err := yaml.Unmarshal([]byte(substituted), &finalManifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest YAML: %w", err)
	}

	FillManifestWithDefaults(&finalManifest, version)

	return &finalManifest, nil
}
