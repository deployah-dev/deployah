// Package manifest provides functions for parsing and manipulating manifest files.
package manifest

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/deployah-dev/deployah/internal/manifest/schema"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// validateJSONAgainstSchema is a helper that validates JSON data against a JSON schema.
// schemaLoader is a function that returns the schema bytes for a given version.
func validateYAMLAgainstSchema(
	obj map[string]any,
	version string,
	schemaLoader func(string) ([]byte, error),
	schemaType schema.SchemaType, // e.g. schema.SchemaTypeManifest or schema.SchemaTypeEnvironments
) error {
	// var obj map[string]interface{}
	// if err := yaml.Unmarshal(yamlBytes, &obj); err != nil {
	// return fmt.Errorf("failed to convert manifest to JSON: %w", err)
	// }

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

	if err := compiler.AddResource(schemaID, jsonSchema); err != nil {
		return fmt.Errorf("failed to load %s schema version %q: %w", schemaType, version, err)
	}

	compiled, err := compiler.Compile(schemaID)
	if err != nil {
		return fmt.Errorf("failed to compile %s schema version %q: %w", schemaType, version, err)
	}

	// Validate the manifest
	if err := compiled.Validate(obj); err != nil {
		return fmt.Errorf("%s validation failed for schema version %q: %w", schemaType, version, err)
	}
	return nil
}

// Validate validates the manifest YAML against the provided JSON schema file.
// version should be the version of the schema (e.g., "v1-alpha.1").
// This is a strict validation: unknown fields are not allowed.
func ValidateManifest(manifestObj map[string]any, version string) error {
	return validateYAMLAgainstSchema(
		manifestObj,
		version,
		schema.GetManifestSchema,
		schema.SchemaTypeManifest,
	)
}

// ValidateEnvironments validates the environments YAML against the provided JSON schema file.
// version should be the version of the schema (e.g., "v1-alpha.1").
// This is a strict validation: unknown fields are not allowed.
func ValidateEnvironments(manifestObj map[string]any, version string) error {
	return validateYAMLAgainstSchema(
		manifestObj,
		version,
		schema.GetEnvironmentsSchema,
		schema.SchemaTypeEnvironments,
	)
}

// ValidateAPIVersion checks the manifest's apiVersion field for presence, type, and validity.
// Returns the apiVersion string if valid, or an error otherwise.
func ValidateAPIVersion(manifestObj map[string]any) (string, error) {
	validVersions, err := schema.GetValidManifestVersions()
	if err != nil {
		return "", fmt.Errorf("failed to get valid manifest versions: %w", err)
	}

	apiVersionVal, ok := manifestObj["apiVersion"]
	if !ok {
		return "", fmt.Errorf("manifest is missing 'apiVersion' field")
	}

	apiVersionStr, ok := apiVersionVal.(string)
	if !ok || apiVersionStr == "" {
		return "", fmt.Errorf("'apiVersion' field must be a non-empty string")
	}

	if !slices.Contains(validVersions, apiVersionStr) {
		return "", fmt.Errorf("unsupported manifest schema version: %s (valid: %v)", apiVersionStr, validVersions)
	}

	return apiVersionStr, nil
}
