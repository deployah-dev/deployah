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
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/renameio/v2"
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

// normalizeComponents removes expose blocks written as `expose: false`, so
// the rest of the code only ever sees a nil or active Expose.
func normalizeComponents(s *Spec) {
	for name, comp := range s.Components {
		if comp.Expose != nil && comp.Expose.disabled {
			comp.Expose = nil
			s.Components[name] = comp
		}
	}
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

// environmentRegistry returns the sorted environment names that may be
// deployed to, plus a label naming the file that owns them. The platform
// config owns the registry when present; the spec map is the fallback.
func environmentRegistry(environments map[string]Environment, platform *PlatformConfig) ([]string, string) {
	if platform != nil {
		return slices.Sorted(maps.Keys(platform.Environments)), "the platform file"
	}
	return slices.Sorted(maps.Keys(environments)), "the spec"
}

// DeclaredEnvironments returns the sorted registry of environment names that
// may be deployed to. The platform config owns the registry when present;
// otherwise the spec's environments map is used.
func DeclaredEnvironments(environments map[string]Environment, platform *PlatformConfig) []string {
	names, _ := environmentRegistry(environments, platform)
	return names
}

// ResolveEnvironment selects the target environment and returns its name and
// developer-owned overrides. Which names are valid (the registry) is owned by
// the platform config when one exists; the spec's environments map supplies
// optional per-environment overrides (envFile, variables) and acts as the
// registry only when there is no platform config.
//
// When desiredEnvironment is empty:
//   - Empty registry: returns a synthetic default environment.
//   - One registry entry: selects it automatically.
//   - Two or more: returns an error listing the registry names.
//
// When desiredEnvironment is set it must match the registry via
// [matchEnvKey]; with an empty registry any name is accepted as-is.
func ResolveEnvironment(environments map[string]Environment, platform *PlatformConfig, desiredEnvironment string) (string, *Environment, error) {
	registry, source := environmentRegistry(environments, platform)

	name := desiredEnvironment
	if desiredEnvironment == "" {
		switch len(registry) {
		case 0:
			// No registry: callers still need a name for release identity
			// and runtime resolution. "default" is the synthetic name when
			// the spec and platform omit environments.
			return "default", &Environment{}, nil
		case 1:
			name = registry[0]
		default:
			return "", nil, fmt.Errorf(
				"multiple environments found in %s but none specified: %s",
				source, joinStrings(registry),
			)
		}
	} else {
		if err := ValidateRequestedEnv(desiredEnvironment); err != nil {
			return "", nil, err
		}
		if len(registry) > 0 {
			matched, ok := matchEnvKey(desiredEnvironment, registry)
			if !ok {
				return "", nil, fmt.Errorf(
					"environment %q not found in %s, available environments: %s",
					desiredEnvironment, source, joinStrings(registry),
				)
			}
			name = matched
		}
	}

	// The spec entry is an optional override; absent means zero value.
	if matched, ok := matchEnvKey(name, slices.Collect(maps.Keys(environments))); ok {
		cp := environments[matched]
		return matched, &cp, nil
	}
	return name, &Environment{}, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// PrescanSubstitutionReport inspects the raw (pre-envsubst) spec for
// ${VAR} tokens in expose.subdomain fields and returns a SubstitutionReport.
// [Load] computes the same report from the raw YAML map. Call this after
// [ParseManifest] when a typed pre-substitution spec is already in hand.
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

// prescanSubstitutionReportFromRaw records ${VAR} tokens in
// components.*.expose.subdomain from the pre-substitution YAML map.
// It is observational: malformed entries are skipped, not rejected.
// expose: false and expose: true are skipped, matching
// [normalizeComponents] / [PrescanSubstitutionReport].
func prescanSubstitutionReportFromRaw(specObj map[string]any) SubstitutionReport {
	report := SubstitutionReport{DynamicSubdomains: make(map[string]bool)}
	if specObj == nil {
		return report
	}
	components, ok := specObj["components"].(map[string]any)
	if !ok {
		return report
	}
	for name, raw := range components {
		comp, isComp := raw.(map[string]any)
		if !isComp {
			continue
		}
		expose, isExpose := comp["expose"].(map[string]any)
		if !isExpose {
			continue
		}
		subdomain, isString := expose["subdomain"].(string)
		if !isString {
			continue
		}
		if varPattern.MatchString(subdomain) {
			report.DynamicSubdomains[name] = true
		}
	}
	return report
}

func loadFail(err error) (*Spec, SubstitutionReport, error) {
	return nil, SubstitutionReport{}, err
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

	// Atomic write-then-rename: a crash or interrupt never leaves a
	// truncated or partially written file behind.
	if err = renameio.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write spec to %s: %w", path, err)
	}

	return nil
}

// LoadOption configures optional behavior of [Load].
type LoadOption func(*loadOptions)

type loadOptions struct {
	allowHookCycleForDisplay bool
}

// AllowHookCycleForDisplay returns a [LoadOption] that passes true to
// [ValidateSpecTasks] so [ResolveForDisplay] can record a hook cycle as a
// warning. Other task validation still runs. Deploy, plan, validate, and
// run must not pass this option.
func AllowHookCycleForDisplay() LoadOption {
	return func(o *loadOptions) {
		o.allowHookCycleForDisplay = true
	}
}

// Load reads and parses the spec YAML file at the given path, resolves the
// environment (using desiredEnv or default resolution rules), substitutes
// variables according to precedence, validates the spec, and applies defaults.
//
// The returned [SubstitutionReport] is computed from the same
// pre-substitution YAML map that Load already read. On success,
// DynamicSubdomains is an initialized map (empty when no component used a
// ${VAR} token in expose.subdomain). On any failure, Load returns a nil
// spec, a zero [SubstitutionReport], and the error.
//
// platform supplies the environment registry for [ResolveEnvironment]; pass
// nil when no platform file exists. This function performs the load pipeline
// without platform resolution; for [ResolvedSpec] see [Resolve].
//
// opts may include [AllowHookCycleForDisplay] for inspect-only commands.
func Load(ctx context.Context, path, desiredEnv string, platform *PlatformConfig, opts ...LoadOption) (*Spec, SubstitutionReport, error) {
	options := loadOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	if path == "" {
		path = DefaultSpecPath
	}

	data, err := os.ReadFile(path) // #nosec G304 -- spec path from CLI or default
	if err != nil {
		return loadFail(fmt.Errorf("failed to read spec: %w", err))
	}

	var specObj map[string]any
	if err = yaml.Unmarshal(data, &specObj); err != nil {
		return loadFail(fmt.Errorf("failed to parse spec YAML: %w", err))
	}

	substReport := prescanSubstitutionReportFromRaw(specObj)

	version, err := ValidateAPIVersion(specObj)
	if err != nil {
		return loadFail(fmt.Errorf("failed to validate API version: %w", err))
	}

	if err = ValidateEnvironments(specObj, version); err != nil {
		return loadFail(fmt.Errorf("environments validation failed: %w", err))
	}

	// Parse the environments section to resolve the target environment.
	var tmp struct {
		Environments map[string]Environment `yaml:"environments"`
	}
	if err = yaml.Unmarshal(data, &tmp); err != nil {
		return loadFail(fmt.Errorf("failed to parse spec YAML: %w", err))
	}

	envName, env, err := ResolveEnvironment(tmp.Environments, platform, desiredEnv)
	if err != nil {
		return loadFail(fmt.Errorf("failed to select environment: %w", err))
	}

	slog.InfoContext(ctx, "selected environment", "environment", envName)

	substituted, err := SubstituteVariables(data, env)
	if err != nil {
		return loadFail(fmt.Errorf("failed to substitute variables: %w", err))
	}

	var substitutedObj map[string]any
	if err = yaml.Unmarshal(substituted, &substitutedObj); err != nil {
		return loadFail(fmt.Errorf("failed to parse substituted spec YAML: %w", err))
	}

	if err = ValidateSpec(substitutedObj, version); err != nil {
		return loadFail(fmt.Errorf("spec validation failed: %w", err))
	}

	var finalSpec Spec
	if err = yaml.Unmarshal(substituted, &finalSpec); err != nil {
		return loadFail(fmt.Errorf("failed to unmarshal spec: %w", err))
	}
	normalizeComponents(&finalSpec)

	if err = ValidateSpecComponents(&finalSpec); err != nil {
		return loadFail(fmt.Errorf("validation failed: %w", err))
	}

	if err = ValidateSpecTasks(&finalSpec, options.allowHookCycleForDisplay); err != nil {
		return loadFail(fmt.Errorf("validation failed: %w", err))
	}

	if err = FillSpecWithDefaults(&finalSpec, version); err != nil {
		return loadFail(fmt.Errorf("failed to apply defaults: %w", err))
	}

	finalSpec.SpecDir = filepath.Dir(path)
	return &finalSpec, substReport, nil
}

// ParseManifest reads and partially validates the spec YAML file: validates
// the API version and environments section, then unmarshals the raw struct
// without applying envsubst or defaults. It returns the raw spec and version.
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
	normalizeComponents(&rawSpec)
	rawSpec.SpecDir = filepath.Dir(path)

	return &rawSpec, version, nil
}
