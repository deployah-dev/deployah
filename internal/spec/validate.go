package spec

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"deployah.dev/deployah/internal/spec/schema"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

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
// version should be the version of the schema (e.g., "v1-alpha.1").
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
// version should be the version of the schema (e.g., "v1-alpha.1").
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
	hasResources := (component.Resources.CPU != nil && *component.Resources.CPU != "") ||
		(component.Resources.Memory != nil && *component.Resources.Memory != "") ||
		(component.Resources.EphemeralStorage != nil && *component.Resources.EphemeralStorage != "")
	hasPreset := component.ResourcePreset != ""

	if hasResources && hasPreset {
		return fmt.Errorf("component cannot have both 'resources' and 'resourcePreset' fields")
	}

	// Check if resources object is present but empty (resources: {})
	// This happens when someone explicitly writes "resources: {}" in YAML
	// With pointers, we can detect if the Resources struct was explicitly set
	if !hasResources && (component.Resources.CPU != nil || component.Resources.Memory != nil || component.Resources.EphemeralStorage != nil) {
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
	}

	if len(errs) > 0 {
		return fmt.Errorf("component validation failed: %w", errors.Join(errs...))
	}

	return nil
}
