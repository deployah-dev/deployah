package spec

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"deployah.dev/deployah/internal/spec/schema"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// SentinelSubstituteRaw replaces ${VAR} tokens in raw YAML bytes with
// format-valid sentinel values so JSON schema format assertions still catch
// literal typos in fields like subdomain or hostname. Only scalar string
// values that consist entirely of a single ${VAR} expression are replaced;
// mixed strings (e.g., "prefix-${VAR}") are left as-is because they are
// likely already valid enough for schema validation.
//
// This is intentionally a simple text-level approach (not a YAML AST walk)
// because sentinel substitution is only needed for offline validate mode where
// no env context is available. It is not used during normal deploy flows.
func SentinelSubstituteRaw(data []byte) []byte {
	// Replace bare ${VAR} tokens with the sentinel "placeholder".
	// We only replace tokens that appear as a standalone YAML scalar value
	// (after a colon-space or as a list item), not inside longer strings.
	return varPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		return []byte("placeholder")
	})
}

// validateJSONAgainstSchema is a helper that validates JSON data against a
// JSON schema. schemaLoader returns the schema bytes for a given version.
func validateYAMLAgainstSchema(
	obj map[string]any,
	version string,
	schemaLoader func(string) ([]byte, error),
	schemaType schema.SchemaType, // e.g. schema.SchemaTypeManifest or schema.SchemaTypeEnvironments
) error {
	// Load schema
	schemaBytes, err := schemaLoader(version)
	if err != nil {
		return fmt.Errorf("failed to get %s schema version %q: %w", schemaType, version, err)
	}

	// Compile schema with format assertions enabled
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	schemaID := filepath.Join(version, schemaType.String()+".json")
	jsonSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return fmt.Errorf("invalid %s schema JSON for version %q: %w", schemaType, version, err)
	}

	if err = compiler.AddResource(schemaID, jsonSchema); err != nil {
		return fmt.Errorf("failed to load %s schema version %q: %w", schemaType, version, err)
	}

	compiled, err := compiler.Compile(schemaID)
	if err != nil {
		return fmt.Errorf("failed to compile %s schema version %q: %w", schemaType, version, err)
	}

	// Validate the spec
	if err = compiled.Validate(obj); err != nil {
		return fmt.Errorf("%s validation failed for schema version %q: %w", schemaType, version, err)
	}
	return nil
}

// ValidateSpec validates spec YAML against the provided JSON schema.
// version should be the version of the schema (e.g., "v1-alpha.2").
// This is a strict validation: unknown fields are not allowed.
func ValidateSpec(specObj map[string]any, version string) error {
	return validateYAMLAgainstSchema(
		specObj,
		version,
		schema.GetManifestSchema,
		schema.SchemaTypeManifest,
	)
}

// ValidateEnvironments validates environments YAML against the provided JSON
// schema file.
// version should be the version of the schema (e.g., "v1-alpha.2").
// This is a strict validation: unknown fields are not allowed.
func ValidateEnvironments(specObj map[string]any, version string) error {
	return validateYAMLAgainstSchema(
		specObj,
		version,
		schema.GetEnvironmentsSchema,
		schema.SchemaTypeEnvironments,
	)
}

// ValidateAPIVersion checks the spec apiVersion field for presence, type,
// and validity.
// Returns the apiVersion string if valid, or an error otherwise.
func ValidateAPIVersion(specObj map[string]any) (string, error) {
	validVersions, err := schema.GetValidManifestVersions()
	if err != nil {
		return "", fmt.Errorf("failed to get valid spec versions: %w", err)
	}

	apiVersionVal, ok := specObj["apiVersion"]
	if !ok {
		return "", fmt.Errorf("spec is missing 'apiVersion' field")
	}

	apiVersionStr, ok := apiVersionVal.(string)
	if !ok || apiVersionStr == "" {
		return "", fmt.Errorf("'apiVersion' field must be a non-empty string")
	}

	if !slices.Contains(validVersions, apiVersionStr) {
		return "", fmt.Errorf("unsupported spec schema version: %s (valid: %v)", apiVersionStr, validVersions)
	}

	return apiVersionStr, nil
}

// ValidateComponentResources validates a component's resource configuration.
// A component can have either:
// 1. Explicit resources (resources field with actual values)
// 2. Resource preset (resourcePreset field)
// 3. Neither (will use defaults)
// It cannot have both resources and resourcePreset, or an empty resources
// object.
func ValidateComponentResources(component Component) error {
	hasResources := component.Resources.ResourcesSet()
	hasPreset := component.ResourcePreset != ""

	if hasResources && hasPreset {
		return fmt.Errorf("component cannot have both 'resources' and 'resourcePreset' fields")
	}

	// Check if resources object is present but empty (resources: {} or
	// zero quantities). Pointers let us detect an explicitly set block.
	if !hasResources && component.Resources.ResourcesPresent() {
		return fmt.Errorf("component cannot have empty 'resources' object - either specify actual resource values or remove the resources field entirely")
	}

	// Both empty is allowed (will use defaults)
	// Either one is allowed
	return nil
}

// ValidateComponentAutoscaling validates a component's autoscaling
// configuration.
func ValidateComponentAutoscaling(component Component) error {
	if component.Autoscaling == nil || !component.Autoscaling.Enabled {
		return nil // Autoscaling not enabled, no validation needed
	}

	if component.Autoscaling.MinReplicas > component.Autoscaling.MaxReplicas {
		return fmt.Errorf("minReplicas cannot be greater than maxReplicas")
	}

	for _, m := range component.Autoscaling.Metrics {
		switch m.Type {
		case MetricTypeCPU, MetricTypeMemory:
			// known types -- translated to targetCPU / targetMemory in the Helm layer
		default:
			return fmt.Errorf("unsupported metric type %q: only %q and %q are supported",
				m.Type, MetricTypeCPU, MetricTypeMemory)
		}
	}

	return nil
}

// ValidateComponentHealth validates the health check configuration of a
// component. Health checks are only supported for role: service components.
func ValidateComponentHealth(component Component) error {
	if component.Health == nil {
		return nil
	}

	// Role may still be "" here if this runs before defaults are filled in;
	// the schema defaults it to service, so treat empty the same way.
	if !component.Role.IsService() && component.Role != "" {
		return fmt.Errorf("health checks are only supported for role: service components")
	}

	// No port check here: only service roles reach this point, and the
	// schema defaults their port to 8080 after validation.
	if component.Health.Ready != nil && !component.Health.Ready.Disabled {
		if component.Health.Ready.Path != "" && component.Health.Ready.Path[0] != '/' {
			return fmt.Errorf("health.ready.path must start with /")
		}
	}

	if component.Health.Alive != nil && !component.Health.Alive.Disabled {
		if component.Health.Alive.Path != "" && component.Health.Alive.Path[0] != '/' {
			return fmt.Errorf("health.alive.path must start with /")
		}

		if component.Health.Alive.Interval != "" {
			intervalSec, err := ParseDuration(component.Health.Alive.Interval)
			if err != nil {
				return fmt.Errorf("health.alive.interval: %w", err)
			}
			if intervalSec <= 0 {
				return fmt.Errorf("health.alive.interval must be a positive duration")
			}
		}

		if component.Health.Alive.RestartAfter != "" {
			restartSec, err := ParseDuration(component.Health.Alive.RestartAfter)
			if err != nil {
				return fmt.Errorf("health.alive.restartAfter: %w", err)
			}
			if restartSec <= 0 {
				return fmt.Errorf("health.alive.restartAfter must be a positive duration")
			}

			// Validate that restartAfter >= interval so failureThreshold >= 1.
			intervalStr := component.Health.Alive.Interval
			if intervalStr == "" {
				intervalStr = DefaultLivenessInterval
			}
			intervalSec, err := ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("health.alive.interval: %w", err)
			}
			if restartSec < intervalSec {
				return fmt.Errorf("health.alive.restartAfter (%s) must be greater than or equal to health.alive.interval (%s)",
					component.Health.Alive.RestartAfter, intervalStr)
			}
		}
	}

	return nil
}

// ValidateSpecComponents validates all components in a spec.
func ValidateSpecComponents(spec *Spec) error {
	var errs []error

	for name, component := range spec.Components {
		if err := ValidateComponentResources(component); err != nil {
			errs = append(errs, fmt.Errorf("component %s: %w", name, err))
		}
		if err := ValidateComponentAutoscaling(component); err != nil {
			errs = append(errs, fmt.Errorf("component %s: %w", name, err))
		}
		if err := ValidateComponentHealth(component); err != nil {
			errs = append(errs, fmt.Errorf("component %s: %w", name, err))
		}
		if err := ValidateComponentExpose(component); err != nil {
			errs = append(errs, fmt.Errorf("component %s: %w", name, err))
		}
		if err := ValidateComponentEnvironmentFilter(component); err != nil {
			errs = append(errs, fmt.Errorf("component %s: %w", name, err))
		}
		if err := ValidateComponentProfiles(component); err != nil {
			errs = append(errs, fmt.Errorf("component %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("component validation failed: %w", errors.Join(errs...))
	}

	return nil
}

// ValidateComponentProfiles checks that profile names in the component are
// non-empty strings. Platform lookup happens during resolve.
func ValidateComponentProfiles(component Component) error {
	for i, name := range component.Profiles {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("profiles[%d]: profile name must not be empty", i)
		}
	}
	return nil
}

// ValidateComponentExpose rejects an expose block combining apex with a
// subdomain.
func ValidateComponentExpose(component Component) error {
	if component.Expose == nil {
		return nil
	}
	if component.Expose.Apex && component.Expose.Subdomain != nil {
		return fmt.Errorf("expose: apex and subdomain are mutually exclusive")
	}
	return nil
}

// ValidateComponentEnvironmentFilter rejects unsupported "/*" suffixes in a
// component's environments filter: matching is prefix-based, so a plain
// name already covers its wildcard instances.
func ValidateComponentEnvironmentFilter(component Component) error {
	for _, entry := range component.Environments {
		if base, cut := strings.CutSuffix(entry, "/*"); cut {
			return fmt.Errorf(
				"environments filter entry %q: the \"/*\" suffix is not supported; use %q, which already matches names like %q",
				entry, base, base+"/pr-123")
		}
	}
	return nil
}
