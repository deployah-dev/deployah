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
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/google/renameio/v2"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/spec/schema"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// SupportedPlatformVersions lists platform schema versions that are
// compatible with the current manifest API.
var SupportedPlatformVersions = []string{"platform/v1-alpha.1"}

// CurrentPlatformVersion is the platform apiVersion written by scaffold
// helpers (init, cluster up). It is always the last entry in
// SupportedPlatformVersions. Bump SupportedPlatformVersions first, then this
// constant follows automatically at compile time.
const CurrentPlatformVersion = "platform/v1-alpha.1"

// LoadPlatform reads and validates the platform configuration file at path.
// The file is never subject to envsubst. LoadPlatform performs:
//  1. YAML parse into a raw map for schema validation
//  2. Schema validation against the embedded platform schema
//  3. Internal-consistency checks (TLS mode fields, domain references)
//  4. Unmarshal into [PlatformConfig]
//
// On success it returns the parsed platform config. On error it returns a
// nil config and a descriptive error.
func LoadPlatform(path string) (*PlatformConfig, error) {
	if path == "" {
		return nil, fmt.Errorf("platform file path must not be empty")
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path resolved by session before call
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("platform file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read platform file %s: %w", path, err)
	}

	var raw map[string]any
	if err = yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse platform YAML: %w", err)
	}

	// Extract and validate apiVersion.
	apiVersionVal, ok := raw["apiVersion"]
	if !ok {
		return nil, fmt.Errorf("platform file is missing 'apiVersion' field")
	}
	apiVersion, ok := apiVersionVal.(string)
	if !ok || apiVersion == "" {
		return nil, fmt.Errorf("platform file 'apiVersion' must be a non-empty string")
	}

	platformVersion := strings.TrimPrefix(apiVersion, "platform/")
	if platformVersion == apiVersion {
		return nil, fmt.Errorf("platform file apiVersion must start with 'platform/' (got %q)", apiVersion)
	}

	// Validate against JSON schema.
	schemaBytes, err := schema.GetPlatformSchema(platformVersion)
	if err != nil {
		return nil, fmt.Errorf("unsupported platform schema version %q: %w", apiVersion, err)
	}

	if err = validatePlatformYAML(raw, schemaBytes, apiVersion); err != nil {
		return nil, fmt.Errorf("platform file validation failed: %w", err)
	}

	// Unmarshal into typed struct.
	var platform PlatformConfig
	if err = yaml.Unmarshal(data, &platform); err != nil {
		return nil, fmt.Errorf("failed to unmarshal platform config: %w", err)
	}

	// Internal-consistency checks.
	if err = validatePlatformConsistency(&platform); err != nil {
		return nil, fmt.Errorf("platform file internal consistency error: %w", err)
	}

	return &platform, nil
}

// validatePlatformYAML validates the raw platform YAML map against the schema.
func validatePlatformYAML(raw map[string]any, schemaBytes []byte, version string) error {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	schemaID := "platform-schema-" + version + ".json"
	jsonSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return fmt.Errorf("invalid platform schema JSON for version %q: %w", version, err)
	}

	if err = compiler.AddResource(schemaID, jsonSchema); err != nil {
		return fmt.Errorf("failed to add platform schema: %w", err)
	}

	compiled, err := compiler.Compile(schemaID)
	if err != nil {
		return fmt.Errorf("failed to compile platform schema: %w", err)
	}

	if err = compiled.Validate(raw); err != nil {
		return fmt.Errorf("platform schema validation failed for %q: %w", version, err)
	}
	return nil
}

// validatePlatformConsistency checks internal consistency of the platform
// config: TLS mode fields must be set correctly and domain baseDomains must
// be non-empty.
func validatePlatformConsistency(p *PlatformConfig) error {
	for envKey, env := range p.Environments {
		var defaults []string
		for domainKey, domain := range env.Domains {
			if domain.Default {
				defaults = append(defaults, domainKey)
			}
			if domain.BaseDomain == "" {
				return fmt.Errorf("environments.%s.domains.%s.baseDomain must not be empty",
					envKey, domainKey)
			}
			if domain.TLS != nil {
				if err := validatePlatformTLS(domain.TLS, envKey, domainKey); err != nil {
					return err
				}
			}
		}
		if len(defaults) > 1 {
			slices.Sort(defaults)
			return fmt.Errorf("environments.%s.domains: at most one domain may set default: true, got %s",
				envKey, strings.Join(defaults, ", "))
		}
	}
	return nil
}

// validatePlatformTLS checks that TLS mode fields are consistent.
func validatePlatformTLS(tls *PlatformTLS, envKey, domainKey string) error {
	prefix := fmt.Sprintf("environments.%s.domains.%s.tls", envKey, domainKey)
	switch tls.Mode {
	case TLSModeSelfSigned:
		return nil
	case TLSModeSecretName:
		if tls.SecretName == "" {
			return fmt.Errorf("%s: secretName is required when mode is secretName", prefix)
		}
	case TLSModeCertManager:
		if tls.Issuer == "" {
			return fmt.Errorf("%s: issuer is required when mode is certManager", prefix)
		}
	default:
		return fmt.Errorf("%s: unsupported mode %q", prefix, tls.Mode)
	}
	return nil
}

// IsSupportedPlatformVersion reports whether the given platform apiVersion
// (e.g. "platform/v1-alpha.1") is supported by the current version of
// Deployah.
func IsSupportedPlatformVersion(apiVersion string) bool {
	return slices.Contains(SupportedPlatformVersions, apiVersion)
}

// ScaffoldPlatformFile writes a deployah.platform.yaml at path registering
// the given environment names. "local" gets a full entry (kind-deployah
// context, nip.io domain, self-signed TLS); every other name gets an empty
// entry with no context, meaning deploys to it follow the kubeconfig
// current-context until one is set.
func ScaffoldPlatformFile(path, ingressIP string, envNames []string) (created bool, err error) {
	if path == "" {
		path = DefaultPlatformPath
	}

	// Never overwrite an existing file; the caller prints a hint instead.
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	}

	// The platform schema requires at least one environment entry
	// (minProperties: 1).
	if len(envNames) == 0 {
		return false, nil
	}

	envs := make(map[string]PlatformEnvironment, len(envNames))
	for _, name := range envNames {
		if name == "local" {
			envs[name] = LocalPlatformEnvironment(ingressIP)
			continue
		}
		envs[name] = PlatformEnvironment{}
	}

	platform := PlatformConfig{
		APIVersion:   CurrentPlatformVersion,
		Environments: envs,
	}

	data, marshalErr := yaml.Marshal(&platform)
	if marshalErr != nil {
		return false, fmt.Errorf("failed to marshal platform config: %w", marshalErr)
	}

	if writeErr := renameio.WriteFile(path, data, 0o600); writeErr != nil {
		return false, fmt.Errorf("failed to write platform file %s: %w", path, writeErr)
	}
	return true, nil
}

// MissingPlatformEnvironments returns the subset of envNames that have no
// entry in platform.Environments, sorted for stable output. platform may be
// nil, which reports every name as missing.
func MissingPlatformEnvironments(platform *PlatformConfig, envNames []string) []string {
	var missing []string
	for _, name := range envNames {
		if platform != nil {
			if _, ok := platform.Environments[name]; ok {
				continue
			}
		}
		missing = append(missing, name)
	}
	slices.Sort(missing)
	return missing
}
