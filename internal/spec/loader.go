package spec

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// varPattern matches ${VAR} placeholder tokens in YAML scalar values.
var varPattern = regexp.MustCompile(`\$\{[^}]+\}`)

// SubstitutionReport records which component fields were produced by envsubst
// variable expansion. Consumers (e.g. the resolver) use this to distinguish
// user-supplied literals from dynamically expanded values.
type SubstitutionReport struct {
	// DynamicSubdomains maps component names to true when the component's
	// expose.subdomain field contained a ${VAR} token before substitution.
	DynamicSubdomains map[string]bool
}

// sanitizeEnvName removes path separators and wildcards from environment names
// to prevent them from interfering with env-file path construction.
// This function is for file path construction only; use [matchEnvKey] for
// environment key matching.
func sanitizeEnvName(name string) string {
	sanitized := strings.TrimSuffix(name, "/*")
	sanitized = strings.ReplaceAll(sanitized, "/", "")
	sanitized = strings.ReplaceAll(sanitized, "\\", "")
	sanitized = strings.ReplaceAll(sanitized, "*", "")
	sanitized = strings.ReplaceAll(sanitized, "?", "")
	return sanitized
}

// ResolveEnvironment returns the matched map key and environment for the
// desired name. It uses [matchEnvKey] for exact-then-prefix lookup.
//
// When desiredEnvironment is empty:
//   - Zero environments: returns a synthetic default environment.
//   - One environment: returns it automatically.
//   - Two or more: returns an error listing available names.
func ResolveEnvironment(environments map[string]Environment, desiredEnvironment string) (string, *Environment, error) {
	if len(environments) == 0 && desiredEnvironment == "" {
		env := &Environment{
			EnvFile:    DefaultEnvFile,
			ConfigFile: DefaultConfigFile,
		}
		return "default", env, nil
	}

	if len(environments) == 1 && desiredEnvironment == "" {
		for k, v := range environments {
			cp := v
			return k, &cp, nil
		}
	}

	if len(environments) > 1 && desiredEnvironment == "" {
		names := make([]string, 0, len(environments))
		for k := range environments {
			names = append(names, k)
		}
		slices.Sort(names)
		quoted := make([]string, 0, len(names))
		for _, n := range names {
			quoted = append(quoted, fmt.Sprintf("%q", n))
		}
		return "", nil, fmt.Errorf(
			"multiple environments found but none specified: %s",
			strings.Join(quoted, ", "),
		)
	}

	keys := make([]string, 0, len(environments))
	for k := range environments {
		keys = append(keys, k)
	}

	if matched, ok := matchEnvKey(desiredEnvironment, keys); ok {
		cp := environments[matched]
		return matched, &cp, nil
	}

	slices.Sort(keys)
	quoted := make([]string, 0, len(keys))
	for _, k := range keys {
		quoted = append(quoted, fmt.Sprintf("%q", k))
	}
	return "", nil, fmt.Errorf(
		"environment %q not found, available environments: %s",
		desiredEnvironment,
		strings.Join(quoted, ", "),
	)
}

// resolveEnvFile determines which env file to use for the given environment,
// following Deployah's resolution order. Returns the path, whether it was
// explicitly set, and an error if explicitly set but missing.
func resolveEnvFile(env *Environment, envName string) (string, bool, error) {
	if env.EnvFile != "" {
		if fileExists(env.EnvFile) {
			return env.EnvFile, true, nil
		}
		return "", true, fmt.Errorf("explicit envFile %q does not exist", env.EnvFile)
	}

	sanitizedName := sanitizeEnvName(envName)

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

// PrescanSubstitutionReport inspects the raw (pre-envsubst) spec for
// ${VAR} tokens in expose.subdomain fields and returns a SubstitutionReport.
// Call it after [ParseManifest] and before envsubst so the resolver can
// distinguish static from dynamic subdomains (the wildcard static-subdomain
// warning does not fire for dynamically expanded values).
func PrescanSubstitutionReport(rawSpec *Spec) SubstitutionReport {
	report := SubstitutionReport{DynamicSubdomains: make(map[string]bool)}
	for name, comp := range rawSpec.Components {
		if comp.Expose != nil && comp.Expose.Subdomain != nil {
			if varPattern.MatchString(*comp.Expose.Subdomain) {
				report.DynamicSubdomains[name] = true
			}
		}
	}
	return report
}

// Save writes the spec to a YAML file at the specified path.
func Save(spec *Spec, path string) error {
	if path == "" {
		path = DefaultSpecPath
	}

	data, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal spec to YAML: %w", err)
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err = os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	if err = os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write spec to %s: %w", path, err)
	}

	return nil
}

// Load reads and parses the spec YAML file at the given path, resolves the
// environment (using desiredEnv or default resolution rules), substitutes
// variables according to precedence, validates the spec, and applies defaults.
//
// This function performs the full load pipeline without platform resolution.
// For the full pipeline including platform config and [ResolvedSpec], use
// [Session.ResolvedSpec].
func Load(ctx context.Context, path, desiredEnv string) (*Spec, error) {
	if path == "" {
		path = DefaultSpecPath
	}

	data, err := os.ReadFile(path) // #nosec G304 -- spec path from CLI or default
	if err != nil {
		return nil, fmt.Errorf("failed to read spec: %w", err)
	}

	var specObj map[string]any
	if err = yaml.Unmarshal(data, &specObj); err != nil {
		return nil, fmt.Errorf("failed to parse spec YAML: %w", err)
	}

	version, err := ValidateAPIVersion(specObj)
	if err != nil {
		return nil, fmt.Errorf("failed to validate API version: %w", err)
	}

	if err = ValidateEnvironments(specObj, version); err != nil {
		return nil, fmt.Errorf("environments validation failed: %w", err)
	}

	// Parse the environments section to resolve the target environment.
	var tmp struct {
		Environments map[string]Environment `yaml:"environments"`
	}
	if err = yaml.Unmarshal(data, &tmp); err != nil {
		return nil, fmt.Errorf("failed to parse spec YAML: %w", err)
	}

	envName, env, err := ResolveEnvironment(tmp.Environments, desiredEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to select environment: %w", err)
	}

	slog.InfoContext(ctx, "selected environment", "environment", envName)

	envFilePath, explicitlySet, err := resolveEnvFile(env, envName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve environment file: %w", err)
	}
	if envFilePath != "" {
		if explicitlySet {
			slog.InfoContext(ctx, "using explicitly set env file", "envFile", envFilePath)
		} else {
			slog.InfoContext(ctx, "using resolved env file", "envFile", envFilePath)
		}
	} else {
		slog.InfoContext(ctx, "no env file found for environment", "environment", envName)
	}

	env.EnvFile = envFilePath

	substituted, err := SubstituteVariables(data, env)
	if err != nil {
		return nil, fmt.Errorf("failed to substitute variables: %w", err)
	}

	var substitutedObj map[string]any
	if err = yaml.Unmarshal(substituted, &substitutedObj); err != nil {
		return nil, fmt.Errorf("failed to parse substituted spec YAML: %w", err)
	}

	if err = ValidateSpec(substitutedObj, version); err != nil {
		return nil, fmt.Errorf("spec validation failed: %w", err)
	}

	var finalSpec Spec
	if err = yaml.Unmarshal(substituted, &finalSpec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}

	if err = ValidateSpecComponents(&finalSpec); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	if err = FillSpecWithDefaults(&finalSpec, version); err != nil {
		return nil, fmt.Errorf("failed to apply defaults: %w", err)
	}

	return &finalSpec, nil
}

// ParseManifest reads and partially validates the spec YAML file: validates
// the API version and environments section, then unmarshals the raw struct
// without applying envsubst or defaults. It returns the raw spec and version.
// Used by [Session.ResolvedSpec] as the first step of the full pipeline.
func ParseManifest(path string) (*Spec, string, error) {
	if path == "" {
		path = DefaultSpecPath
	}

	data, err := os.ReadFile(path) // #nosec G304 -- spec path from CLI or default
	if err != nil {
		return nil, "", fmt.Errorf("failed to read spec: %w", err)
	}

	var specObj map[string]any
	if err = yaml.Unmarshal(data, &specObj); err != nil {
		return nil, "", fmt.Errorf("failed to parse spec YAML: %w", err)
	}

	version, err := ValidateAPIVersion(specObj)
	if err != nil {
		return nil, "", fmt.Errorf("failed to validate API version: %w", err)
	}

	if err = ValidateEnvironments(specObj, version); err != nil {
		return nil, "", fmt.Errorf("environments validation failed: %w", err)
	}

	var rawSpec Spec
	if err = yaml.Unmarshal(data, &rawSpec); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal spec: %w", err)
	}
	rawSpec.APIVersion = version

	return &rawSpec, version, nil
}
