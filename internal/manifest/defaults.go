// Package manifest provides functions for parsing and manipulating manifest files.
// This package includes functionality for applying default values from JSON schemas
// to manifest structures, with support for caching and type conversion.
//
// The package uses github.com/go-viper/mapstructure for robust type conversion
// between map[string]any (from JSON/YAML) and strongly-typed Go structs, which
// provides better error handling, type safety, and maintainability compared to
// manual reflection-based conversion.
//
// # Default Value Application
//
// The package supports sophisticated default value application including:
//   - Schema-based defaults from JSON Schema definitions
//   - Pattern-based defaults for dynamic component names
//   - Environment-specific placeholder substitution
//   - Resource preset resolution
//
// # Usage Examples
//
// Basic manifest creation with defaults:
//
//	manifest, err := CreateManifestWithDefaults("my-project", "v1-alpha.1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Applying defaults to existing manifest:
//
//	manifest := &Manifest{
//		ApiVersion: "v1-alpha.1",
//		Project:    "my-project",
//		Components: map[string]Component{
//			"web": {Image: "nginx:latest"},
//		},
//	}
//	err := FillManifestWithDefaults(manifest, "v1-alpha.1")
//	// web component now has default role="service", kind="stateless", port=8080
//
// Extracting schema defaults:
//
//	defaults, err := GetDefaultValues("v1-alpha.1", schema.SchemaTypeManifest)
//	// defaults["components.[^[a-zA-Z0-9_-]+$].role"] = "service"
//	// defaults["components.[^[a-zA-Z0-9_-]+$].port"] = 8080
package manifest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"dario.cat/mergo"
	"github.com/deployah-dev/deployah/internal/manifest/schema"
	"github.com/go-viper/mapstructure/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cast"
)

// DefaultValues represents the default values extracted from a JSON schema.
// The map keys are dot-notation paths to fields, and values are the default values to apply.
//
// Examples of keys:
//   - "components.[^[a-zA-Z0-9_-]+$].role" -> "service" (pattern-based component default)
//   - "components.web.port" -> 8080 (specific component default)
//   - "environments.[0].envFile" -> ".env.{name}" (environment template with placeholder)
//   - "environments.production.configFile" -> "config.production.yaml" (specific environment)
type DefaultValues map[string]any

// Constants for path construction and validation.
// These constants define the structure and patterns used for navigating
// and applying defaults to nested manifest structures.
const (
	// Path prefixes for different manifest sections
	componentsPrefix   = "components."   // Prefix for component paths: "components.web.port"
	environmentsPrefix = "environments." // Prefix for environment paths: "environments.prod.envFile"

	// Placeholder substitution
	// Example: ".env.{name}" becomes ".env.production" for environment "production"
	placeholderName = "{name}" // Placeholder replaced with actual names

	// Computed lengths for efficient string operations
	componentsPrefixLength   = len(componentsPrefix)   // 11 - avoids repeated len() calls
	environmentsPrefixLength = len(environmentsPrefix) // 13 - avoids repeated len() calls

	// Templates for constructing schema paths
	arrayItemIndexTemplate = "[0]" // Template for first array item in schema paths
	envFileSuffix          = "/*"  // Suffix to remove from environment names during cleanup
)

// Global caches for schema compilation and pattern extraction.
// These caches improve performance by avoiding repeated schema compilation
// and pattern extraction operations.
//
// Cache keys follow the format: "{version}-{schemaType}"
// Example: "v1-alpha.1-manifest", "v1-alpha.1-environments"
var (
	// compiledSchemaCache stores compiled JSON schemas with their raw data
	// Key format: "v1-alpha.1-manifest" -> schemaInfo{compiled, rawData}
	compiledSchemaCache = make(map[string]*schemaInfo)

	// patternCache stores extracted component name patterns from schemas
	// Key format: "v1-alpha.1" -> "^[a-zA-Z0-9_-]+$"
	patternCache = make(map[string]string)

	// schemaMutex protects concurrent access to the caches
	schemaMutex sync.RWMutex
)

// schemaInfo holds both compiled schema and raw data for efficient access.
// This dual storage allows us to use the compiled schema for validation
// while using the raw data for extracting default values and patterns.
//
// Example usage:
//
//	info.compiled.Validate(data) // Use compiled schema for validation
//	extractDefaults(info.rawData) // Use raw data for default extraction
type schemaInfo struct {
	compiled *jsonschema.Schema // Compiled schema for validation and references
	rawData  map[string]any     // Raw schema data for default value extraction
}

// getCachedSchemaInfo retrieves cached schema info or loads and compiles it.
// This function implements a thread-safe cache for compiled JSON schemas to improve performance.
//
// Parameters:
//   - version: Schema version (e.g., "v1-alpha.1")
//   - schemaType: Type of schema (schema.SchemaTypeManifest or schema.SchemaTypeEnvironments)
//
// Returns:
//   - *schemaInfo: Contains both compiled schema and raw data
//   - error: If schema loading, parsing, or compilation fails
//
// Example:
//
//	info, err := getCachedSchemaInfo("v1-alpha.1", schema.SchemaTypeManifest)
//	if err != nil {
//		return nil, fmt.Errorf("failed to get schema: %w", err)
//	}
//	// info.compiled can be used for validation
//	// info.rawData can be used for extracting defaults
func getCachedSchemaInfo(version string, schemaType schema.SchemaType) (*schemaInfo, error) {
	cacheKey := fmt.Sprintf("%s-%s", version, schemaType)

	// Try to get from cache first
	schemaMutex.RLock()
	if cached, exists := compiledSchemaCache[cacheKey]; exists {
		schemaMutex.RUnlock()
		return cached, nil
	}
	schemaMutex.RUnlock()

	// Load schema if not cached
	var schemaLoader func(string) ([]byte, error)
	switch schemaType {
	case schema.SchemaTypeManifest:
		schemaLoader = schema.GetManifestSchema
	case schema.SchemaTypeEnvironments:
		schemaLoader = schema.GetEnvironmentsSchema
	default:
		return nil, fmt.Errorf("unsupported schema type: %s", schemaType)
	}

	schemaBytes, err := schemaLoader(version)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s schema version %q: %w", schemaType, version, err)
	}

	// Parse schema JSON
	var schemaObj map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaObj); err != nil {
		return nil, fmt.Errorf("failed to parse %s schema JSON for version %q: %w", schemaType, version, err)
	}

	// Compile schema using jsonschema library
	compiler := jsonschema.NewCompiler()
	schemaURL := fmt.Sprintf("internal://%s-%s.json", schemaType, version)

	if err := compiler.AddResource(schemaURL, schemaObj); err != nil {
		return nil, fmt.Errorf("failed to add schema resource for %s version %q: %w", schemaType, version, err)
	}

	compiledSchema, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to compile %s schema for version %q: %w", schemaType, version, err)
	}

	// Create schemaInfo with both compiled and raw data
	result := &schemaInfo{compiled: compiledSchema, rawData: schemaObj}

	// Cache the schema info
	schemaMutex.Lock()
	compiledSchemaCache[cacheKey] = result
	schemaMutex.Unlock()

	return result, nil
}

// ClearSchemaCache clears all cached schemas and patterns.
// This function is primarily useful for testing to ensure clean state
// between test runs and for memory management in long-running applications.
//
// Example usage in tests:
//
//	func TestSchemaDefaults(t *testing.T) {
//		defer ClearSchemaCache() // Clean up after test
//		// ... test code that modifies schemas
//	}
func ClearSchemaCache() {
	schemaMutex.Lock()
	defer schemaMutex.Unlock()
	compiledSchemaCache = make(map[string]*schemaInfo)
	patternCache = make(map[string]string)
}

// extractDefaultsFromSchemaInfo recursively extracts default values from schema info.
// This function walks through the schema structure and builds a map of paths to default values.
//
// Parameters:
//   - schemaInfo: Contains compiled schema and raw schema data
//   - path: Current path in the schema (empty string for root)
//
// Returns:
//   - DefaultValues: Map of dot-notation paths to default values
//
// Example output:
//
//	{
//		"components.[^[a-zA-Z0-9_-]+$].role": "service",
//		"components.[^[a-zA-Z0-9_-]+$].port": 8080,
//		"environments.[0].envFile": ".env.{name}",
//	}
func extractDefaultsFromSchemaInfo(schemaInfo *schemaInfo, path string) DefaultValues {
	defaults := make(DefaultValues)

	// Use the raw schema data for extracting defaults
	if schemaInfo.rawData != nil {
		// Extract defaults from the schema recursively
		extractDefaultsFromSchemaData(schemaInfo.rawData, path, defaults)
	}

	return defaults
}

// extractDefaultsFromSchemaData is a helper function that recursively processes schema data.
// It traverses JSON Schema structures to find "default" properties and builds path-to-value mappings.
//
// Parameters:
//   - schemaData: Raw schema data (typically map[string]any from JSON)
//   - path: Current dot-notation path (e.g., "components.web")
//   - defaults: Map to populate with discovered defaults (modified in-place)
//
// Handles these JSON Schema structures:
//   - "default" properties: Direct default values
//   - "properties": Object property definitions
//   - "patternProperties": Dynamic property patterns (e.g., component names)
//   - "items": Array item schemas
//
// Example schema processing:
//
//	// Input schema: {"properties": {"port": {"default": 8080}}}
//	// Input path: "components.web"
//	// Result: defaults["components.web.port"] = 8080
func extractDefaultsFromSchemaData(schemaData any, path string, defaults DefaultValues) {
	schemaMap, ok := schemaData.(map[string]any)
	if !ok {
		return
	}

	// Check if this schema object has a default value
	if defaultVal, exists := schemaMap["default"]; exists {
		defaults[path] = defaultVal
	}

	// Handle properties (object schemas)
	// Example: {"properties": {"port": {"default": 8080}}}
	// Creates: "components.web.port" -> 8080
	if properties, exists := schemaMap["properties"].(map[string]any); exists {
		for propName, propSchema := range properties {
			propPath := path
			if propPath != "" {
				propPath += "."
			}
			propPath += propName

			// Recursively extract defaults from this property
			extractDefaultsFromSchemaData(propSchema, propPath, defaults)
		}
	}

	// Handle patternProperties (for dynamic property names like components)
	// Example: {"patternProperties": {"^[a-zA-Z0-9_-]+$": {"properties": {"role": {"default": "service"}}}}}
	// Creates: "components.[^[a-zA-Z0-9_-]+$].role" -> "service"
	// This allows any component name matching the pattern to inherit the default
	if patternProps, exists := schemaMap["patternProperties"].(map[string]any); exists {
		for pattern, propSchema := range patternProps {
			// For pattern properties, we use the pattern as a key indicator
			propPath := path
			if propPath != "" {
				propPath += "."
			}
			propPath += "[" + pattern + "]"

			// Recursively extract defaults from this property
			extractDefaultsFromSchemaData(propSchema, propPath, defaults)
		}
	}

	// Handle items (array schemas)
	// Example: {"items": {"properties": {"envFile": {"default": ".env.{name}"}}}}
	// Creates: "environments.[0].envFile" -> ".env.{name}"
	// The [0] indicates this applies to array items (any index)
	if items, exists := schemaMap["items"]; exists {
		itemsPath := path
		if itemsPath != "" {
			itemsPath += "."
		}
		itemsPath += "[0]" // Use [0] to indicate first array item

		// Recursively extract defaults from array items
		extractDefaultsFromSchemaData(items, itemsPath, defaults)
	}
}

// GetDefaultValues extracts all default values from a JSON schema.
// This is the main entry point for retrieving schema-based defaults for a specific version and type.
//
// Parameters:
//   - version: Schema version identifier (e.g., "v1-alpha.1")
//   - schemaType: Type of schema to process (manifest or environments)
//
// Returns:
//   - DefaultValues: Map of paths to default values extracted from the schema
//   - error: If schema loading or processing fails
//
// Example usage:
//
//	defaults, err := GetDefaultValues("v1-alpha.1", schema.SchemaTypeManifest)
//	if err != nil {
//		return err
//	}
//	// defaults now contains:
//	// "components.[^[a-zA-Z0-9_-]+$].role" -> "service"
//	// "components.[^[a-zA-Z0-9_-]+$].port" -> 8080
//	// etc.
func GetDefaultValues(version string, schemaType schema.SchemaType) (DefaultValues, error) {
	// Get cached or compile schema
	schemaInfo, err := getCachedSchemaInfo(version, schemaType)
	if err != nil {
		return nil, err
	}

	// Extract defaults from schema info
	defaults := extractDefaultsFromSchemaInfo(schemaInfo, "")

	return defaults, nil
}

// resolveResourcePresets resolves resource presets into actual resource values.
// This function converts high-level resource presets (like "small", "medium") into
// concrete CPU and memory specifications.
//
// Note: Currently only applies "requests" values from presets to maintain backward compatibility.
// The "limits" values from ResourcePresetMappings are not used in this implementation.
// This is by design to keep the Resources struct simple, but users should be aware
// that preset limits are not automatically applied.
//
// Parameters:
//   - manifest: Manifest to process (modified in-place)
//
// Returns:
//   - error: If preset resolution fails
//
// Example transformation:
//
//	// Before:
//	component := Component{
//		ResourcePreset: "small",
//		Resources: Resources{}, // empty
//	}
//	// After resolveResourcePresets:
//	component.Resources = Resources{
//		CPU:    "500m",
//		Memory: "512Mi",
//		EphemeralStorage: "50Mi",
//	}
func resolveResourcePresets(manifest *Manifest) error {
	for componentName, component := range manifest.Components {
		// Only resolve if ResourcePreset is set and Resources are empty
		if component.ResourcePreset != "" && isZeroValue(component.Resources) {
			if presetResources, exists := ResourcePresetMappings[component.ResourcePreset]; exists {
				// Use the requests values from the preset as the base resources
				// This maintains backward compatibility with the current Helm logic
				component.Resources = Resources{
					CPU:              presetResources["requests"].CPU,
					Memory:           presetResources["requests"].Memory,
					EphemeralStorage: presetResources["requests"].EphemeralStorage,
				}
				manifest.Components[componentName] = component
			}
		}
	}
	return nil
}

// FillManifestWithDefaults fills a Manifest struct with default values from JSON schemas.
// This function applies defaults from both manifest and environment schemas, handles resource
// preset resolution, and supports environment-specific placeholder substitution.
//
// The function processes defaults in this order:
//  1. Apply manifest schema defaults to components
//  2. Resolve resource presets to concrete values
//  3. Merge manifest and environment defaults
//  4. Apply merged defaults to environments with placeholder substitution
//
// Parameters:
//   - manifest: Manifest to fill with defaults (modified in-place)
//   - version: Schema version to use for default extraction
//
// Returns:
//   - error: If schema loading, default extraction, or application fails
//
// Example:
//
//	manifest := &Manifest{
//		ApiVersion: "v1-alpha.1",
//		Project:    "my-app",
//		Components: map[string]Component{
//			"web": {Image: "nginx:latest"}, // Only image specified
//		},
//		Environments: []Environment{
//			{Name: "production"}, // Only name specified
//		},
//	}
//	err := FillManifestWithDefaults(manifest, "v1-alpha.1")
//	// After filling:
//	// manifest.Components["web"].Role = "service"
//	// manifest.Components["web"].Port = 8080
//	// manifest.Environments[0].EnvFile = ".env.production"
func FillManifestWithDefaults(manifest *Manifest, version string) error {
	if manifest == nil {
		return fmt.Errorf("manifest cannot be nil")
	}

	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	manifestDefaults, err := GetDefaultValues(version, schema.SchemaTypeManifest)
	if err != nil {
		return fmt.Errorf("failed to get default values: %w", err)
	}

	envDefaults, err := GetDefaultValues(version, schema.SchemaTypeEnvironments)
	if err != nil {
		return fmt.Errorf("failed to get environment defaults: %w", err)
	}

	// Apply defaults to components
	if manifest.Components == nil {
		manifest.Components = make(map[string]Component)
	}

	for componentName, component := range manifest.Components {
		if err := applyDefaultsRecursively(&component, manifestDefaults, "components."+componentName, version); err != nil {
			return fmt.Errorf("failed to apply defaults to component %s: %w", componentName, err)
		}
		manifest.Components[componentName] = component
	}

	// Resolve resource presets after applying schema defaults
	if err := resolveResourcePresets(manifest); err != nil {
		return fmt.Errorf("failed to resolve resource presets: %w", err)
	}

	// Merge manifest and environment defaults for environments
	mergedDefaults := make(DefaultValues)
	// Copy manifest defaults first
	if err := mergo.Merge(&mergedDefaults, manifestDefaults); err != nil {
		return fmt.Errorf("failed to merge manifest defaults: %w", err)
	}
	// Merge environment defaults with override to allow env defaults to take precedence
	if err := mergo.Merge(&mergedDefaults, envDefaults, mergo.WithOverride); err != nil {
		return fmt.Errorf("failed to merge environment defaults: %w", err)
	}

	// Apply environment defaults
	for i := range manifest.Environments {
		if err := applyDefaultsRecursively(&manifest.Environments[i], mergedDefaults, "environments."+manifest.Environments[i].Name, version); err != nil {
			return fmt.Errorf("failed to apply defaults to environment %s: %w", manifest.Environments[i].Name, err)
		}
	}

	return nil
}

// substitutePlaceholders replaces placeholders in string values with actual environment names.
// This function enables environment-specific configuration by replacing {name} placeholders
// with the actual environment name, after cleaning any suffix patterns.
//
// Parameters:
//   - value: Value to process (typically a string with placeholders)
//   - envName: Environment name to substitute (may include suffixes like "/*")
//
// Returns:
//   - any: Processed value with placeholders replaced
//
// Examples:
//
//	substitutePlaceholders(".env.{name}", "production") -> ".env.production"
//	substitutePlaceholders("config.{name}.yaml", "staging/*") -> "config.staging.yaml"
//	substitutePlaceholders(42, "production") -> 42 (non-strings unchanged)
func substitutePlaceholders(value any, envName string) any {
	cleanEnvName := cleanEnvironmentName(envName)
	str := cast.ToString(value)
	if str != "" {
		return strings.ReplaceAll(str, placeholderName, cleanEnvName)
	}
	return value
}

// applyDefaultsToField applies default values to a single struct field.
// This function checks if a field needs defaults (is zero-value and settable) and attempts
// to find appropriate defaults using multiple fallback strategies.
//
// Parameters:
//   - field: Reflection value of the field to set
//   - fieldType: Reflection type information for the field
//   - defaults: Map of available default values
//   - currentPath: Current dot-notation path (e.g., "components.web")
//   - version: Schema version for pattern extraction
//   - envName: Environment name for placeholder substitution
//
// Returns:
//   - error: If field setting fails
//
// Default resolution strategy:
//  1. Try direct path: "components.web.port" -> 8080
//  2. Try environment index: "environments.[0].envFile" -> ".env.{name}"
//  3. Try component pattern: "components.[^pattern$].port" -> 8080
//
// Example:
//
//	// Field: web component's Port field (currently 0)
//	// Path: "components.web"
//	// Result: Sets Port to 8080 from schema defaults
func applyDefaultsToField(field reflect.Value, fieldType reflect.StructField, defaults DefaultValues, currentPath string, version string, envName string) error {
	if !field.CanSet() || !isZeroValue(field.Interface()) {
		return nil // Skip non-settable or non-zero fields
	}

	fieldName := getJSONFieldName(fieldType)
	defaultPath := buildFieldPath(currentPath, fieldName)

	// Try direct path first
	if defaultVal, exists := defaults[defaultPath]; exists {
		return applyFieldValue(field, defaultVal, envName, fieldName, currentPath)
	}

	// Try alternative paths based on context
	if err := tryAlternativeDefaultPaths(field, fieldName, currentPath, defaults, version, envName); err != nil {
		return fmt.Errorf("failed to apply alternative defaults for field %s at path %s: %w", fieldName, currentPath, err)
	}

	return nil
}

// applyFieldValue applies a default value to a field with placeholder substitution.
// This function processes the default value for environment-specific placeholders
// and then sets the field using type-safe conversion.
//
// Parameters:
//   - field: Reflection value to set
//   - defaultVal: Default value to apply
//   - envName: Environment name for placeholder substitution
//   - fieldName: Name of the field being set (for error messages)
//   - currentPath: Current path for error context
//
// Returns:
//   - error: If value setting fails
//
// Example:
//
//	// defaultVal: ".env.{name}"
//	// envName: "production"
//	// Result: Field set to ".env.production"
func applyFieldValue(field reflect.Value, defaultVal any, envName, fieldName, currentPath string) error {
	processedVal := substitutePlaceholders(defaultVal, envName)
	if err := setFieldValue(field, processedVal); err != nil {
		return fmt.Errorf("failed to set field %s at path %s: %w", fieldName, currentPath, err)
	}
	return nil
}

// tryAlternativeDefaultPaths tries alternative paths for finding defaults
func tryAlternativeDefaultPaths(field reflect.Value, fieldName, currentPath string, defaults DefaultValues, version, envName string) error {
	// Try index-based default for environments
	if isEnvironmentPath(currentPath) {
		idxPath := environmentsPrefix + arrayItemIndexTemplate + "." + fieldName
		if defaultVal, exists := defaults[idxPath]; exists {
			return applyFieldValue(field, defaultVal, envName, fieldName, currentPath)
		}
	}

	// Try pattern property path for components
	if isComponentPath(currentPath) {
		return tryComponentPatternPath(field, fieldName, currentPath, defaults, version, envName)
	}

	return nil
}

// tryComponentPatternPath tries to apply defaults using component pattern paths
func tryComponentPatternPath(field reflect.Value, fieldName, currentPath string, defaults DefaultValues, version, envName string) error {
	// Extract the field path after "components."
	fieldPath := currentPath[componentsPrefixLength:]
	parts := strings.Split(fieldPath, ".")
	if len(parts) == 0 {
		return nil
	}

	restPath := strings.Join(parts[1:], ".")
	pattern := extractComponentPattern(version)
	if pattern == "" {
		return nil
	}

	patternPath := buildComponentPatternPath(pattern, restPath, fieldName)
	if defaultVal, exists := defaults[patternPath]; exists {
		if err := setFieldValue(field, defaultVal); err != nil {
			return fmt.Errorf("failed to set component field %s at path %s: %w", fieldName, currentPath, err)
		}
	}

	return nil
}

// processStructField handles recursive processing of struct fields
func processStructField(field reflect.Value, fieldType reflect.StructField, defaults DefaultValues, currentPath string, version string) error {
	fieldName := getJSONFieldName(fieldType)
	newPath := buildFieldPath(currentPath, fieldName)

	switch field.Kind() {
	case reflect.Ptr:
		// Handle pointer fields (like *Autoscaling)
		if !field.IsNil() {
			if err := applyDefaultsRecursively(field.Interface(), defaults, newPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to pointer field %s at path %s: %w", fieldName, currentPath, err)
			}
		}
	case reflect.Struct:
		if err := applyDefaultsRecursively(field.Addr().Interface(), defaults, newPath, version); err != nil {
			return fmt.Errorf("failed to apply defaults to struct field %s at path %s: %w", fieldName, currentPath, err)
		}
	case reflect.Map:
		if err := applyDefaultsToMap(field, defaults, newPath); err != nil {
			return fmt.Errorf("failed to apply defaults to map field %s at path %s: %w", fieldName, currentPath, err)
		}
	case reflect.Slice:
		if err := applyDefaultsToSlice(field, defaults, newPath); err != nil {
			return fmt.Errorf("failed to apply defaults to slice field %s at path %s: %w", fieldName, currentPath, err)
		}
	}

	return nil
}

// applyDefaultsRecursively applies default values to a struct recursively
func applyDefaultsRecursively(obj any, defaults DefaultValues, currentPath string, version string) error {
	if obj == nil {
		return fmt.Errorf("cannot apply defaults to nil object at path %s", currentPath)
	}

	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("object must be a pointer to apply defaults at path %s", currentPath)
	}

	val = val.Elem()
	typ := val.Type()

	// Extract environment name if this is an environment path
	envName := extractEnvironmentName(currentPath)

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// Apply defaults to zero-value fields
		if err := applyDefaultsToField(field, fieldType, defaults, currentPath, version, envName); err != nil {
			return err
		}

		// Always process struct fields recursively
		if err := processStructField(field, fieldType, defaults, currentPath, version); err != nil {
			return err
		}
	}

	return nil
}

// isComponentPath checks if the current path is a component path.
// Component paths start with "components." and are used to identify
// when component-specific default resolution should be applied.
//
// Examples:
//
//	isComponentPath("components.web") -> true
//	isComponentPath("components.web.port") -> true
//	isComponentPath("environments.prod") -> false
//	isComponentPath("project") -> false
func isComponentPath(path string) bool {
	return len(path) > componentsPrefixLength && path[:componentsPrefixLength] == componentsPrefix
}

// isEnvironmentPath checks if the current path is an environment path.
// Environment paths start with "environments." and are used to identify
// when environment-specific default resolution should be applied.
//
// Examples:
//
//	isEnvironmentPath("environments.production") -> true
//	isEnvironmentPath("environments.staging.envFile") -> true
//	isEnvironmentPath("components.web") -> false
func isEnvironmentPath(path string) bool {
	return strings.HasPrefix(path, environmentsPrefix)
}

// buildFieldPath constructs a field path by combining current path and field name.
// This utility function handles the dot-notation path building used throughout
// the default application process.
//
// Parameters:
//   - currentPath: Current path context (may be empty for root)
//   - fieldName: Name of the field to append
//
// Returns:
//   - string: Combined path using dot notation
//
// Examples:
//
//	buildFieldPath("components.web", "port") -> "components.web.port"
//	buildFieldPath("", "project") -> "project"
//	buildFieldPath("environments.prod", "envFile") -> "environments.prod.envFile"
func buildFieldPath(currentPath, fieldName string) string {
	if currentPath == "" {
		return fieldName
	}
	return fmt.Sprintf("%s.%s", currentPath, fieldName)
}

// extractEnvironmentName extracts the environment name from an environment path.
// This function parses environment paths to get the environment name for
// placeholder substitution.
//
// Parameters:
//   - path: Full path to parse
//
// Returns:
//   - string: Environment name, or empty string if not an environment path
//
// Examples:
//
//	extractEnvironmentName("environments.production.envFile") -> "production"
//	extractEnvironmentName("environments.staging") -> "staging"
//	extractEnvironmentName("components.web") -> "" (not environment path)
func extractEnvironmentName(path string) string {
	if !isEnvironmentPath(path) {
		return ""
	}
	parts := strings.Split(path, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// cleanEnvironmentName removes suffix patterns from environment names.
// This function cleans environment names by removing common suffixes
// that may be added during processing.
//
// Parameters:
//   - envName: Environment name that may contain suffixes
//
// Returns:
//   - string: Cleaned environment name
//
// Examples:
//
//	cleanEnvironmentName("production/*") -> "production"
//	cleanEnvironmentName("staging") -> "staging" (no change)
func cleanEnvironmentName(envName string) string {
	if strings.HasSuffix(envName, envFileSuffix) {
		return strings.TrimSuffix(envName, envFileSuffix)
	}
	return envName
}

// buildComponentPatternPath constructs a pattern-based path for components.
// This function builds schema paths that use pattern properties for dynamic
// component names, enabling defaults to apply to any component name matching the pattern.
//
// Parameters:
//   - pattern: Regular expression pattern from schema (e.g., "^[a-zA-Z0-9_-]+$")
//   - restPath: Path after component name (may be empty for direct fields)
//   - fieldName: Name of the field
//
// Returns:
//   - string: Pattern-based path for schema lookup
//
// Examples:
//
//	buildComponentPatternPath("^[a-zA-Z0-9_-]+$", "", "port")
//	-> "components.[^[a-zA-Z0-9_-]+$].port"
//
//	buildComponentPatternPath("^[a-zA-Z0-9_-]+$", "autoscaling", "enabled")
//	-> "components.[^[a-zA-Z0-9_-]+$].autoscaling.enabled"
func buildComponentPatternPath(pattern, restPath, fieldName string) string {
	if restPath == "" {
		// Direct component field (e.g., role, kind, port)
		return fmt.Sprintf("components.[%s].%s", pattern, fieldName)
	}
	// Nested component field (e.g., autoscaling.enabled)
	return fmt.Sprintf("components.[%s].%s.%s", pattern, restPath, fieldName)
}

// getJSONFieldName extracts the JSON field name from a struct field tag
func getJSONFieldName(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return field.Name
	}
	// Remove omitempty suffix
	if idx := strings.Index(jsonTag, ","); idx != -1 {
		jsonTag = jsonTag[:idx]
	}
	return jsonTag
}

// applyDefaultsToMap applies default values to a map (using reflect.Value)
func applyDefaultsToMap(mapVal reflect.Value, defaults DefaultValues, currentPath string) error {
	if mapVal.Kind() != reflect.Map {
		return nil
	}
	for _, key := range mapVal.MapKeys() {
		value := mapVal.MapIndex(key)
		keyStr := fmt.Sprintf("%v", key.Interface())
		newPath := buildFieldPath(currentPath, keyStr)
		switch value.Kind() {
		case reflect.Map:
			if err := applyDefaultsToMap(value, defaults, newPath); err != nil {
				return fmt.Errorf("failed to apply defaults to nested map at key %s in path %s: %w", keyStr, currentPath, err)
			}
		case reflect.Slice:
			if err := applyDefaultsToSlice(value, defaults, newPath); err != nil {
				return fmt.Errorf("failed to apply defaults to slice at key %s in path %s: %w", keyStr, currentPath, err)
			}
		case reflect.Struct:
			if err := applyDefaultsRecursively(value.Addr().Interface(), defaults, newPath, ""); err != nil {
				return fmt.Errorf("failed to apply defaults to struct at key %s in path %s: %w", keyStr, currentPath, err)
			}
		}
	}
	return nil
}

// applyDefaultsToSlice applies default values to a slice (using reflect.Value)
func applyDefaultsToSlice(sliceVal reflect.Value, defaults DefaultValues, currentPath string) error {
	if sliceVal.Kind() != reflect.Slice {
		return nil
	}
	for i := 0; i < sliceVal.Len(); i++ {
		item := sliceVal.Index(i)
		itemPath := fmt.Sprintf("%s[%d]", currentPath, i)
		switch item.Kind() {
		case reflect.Map:
			if err := applyDefaultsToMap(item, defaults, itemPath); err != nil {
				return fmt.Errorf("failed to apply defaults to map item at index %d in path %s: %w", i, currentPath, err)
			}
		case reflect.Slice:
			if err := applyDefaultsToSlice(item, defaults, itemPath); err != nil {
				return fmt.Errorf("failed to apply defaults to nested slice at index %d in path %s: %w", i, currentPath, err)
			}
		case reflect.Struct:
			if err := applyDefaultsRecursively(item.Addr().Interface(), defaults, itemPath, ""); err != nil {
				return fmt.Errorf("failed to apply defaults to struct item at index %d in path %s: %w", i, currentPath, err)
			}
		}
	}
	return nil
}

// setFieldValue sets a reflect.Value to a value, converting types if needed.
// This function provides robust type conversion between schema values (typically from JSON)
// and Go struct fields, handling both simple types and complex structures.
//
// The function attempts conversion in this order:
//  1. Direct assignment if types are compatible
//  2. Simple type conversion using cast package (string, bool, int, float)
//  3. Complex type conversion using mapstructure for structs, slices, maps
//
// Parameters:
//   - field: Reflection value to set (must be settable)
//   - value: Value to assign (any type from schema)
//
// Returns:
//   - error: If field is not settable or conversion fails
//
// Examples:
//
//	setFieldValue(stringField, "hello") // Direct string assignment
//	setFieldValue(intField, 42.0) // float64 to int conversion
//	setFieldValue(sliceField, []any{"a", "b"}) // []any to []string conversion
func setFieldValue(field reflect.Value, value any) error {
	if !field.CanSet() {
		return fmt.Errorf("field of type %s is not settable", field.Type())
	}

	// Handle direct assignment for simple cases
	val := reflect.ValueOf(value)
	if val.Type().AssignableTo(field.Type()) {
		field.Set(val)
		return nil
	}

	// For basic types, use cast package for simple conversions
	switch field.Kind() {
	case reflect.String:
		field.SetString(cast.ToString(value))
		return nil
	case reflect.Bool:
		field.SetBool(cast.ToBool(value))
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(cast.ToInt64(value))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		field.SetUint(cast.ToUint64(value))
		return nil
	case reflect.Float32, reflect.Float64:
		field.SetFloat(cast.ToFloat64(value))
		return nil
	}

	// For complex types (structs, slices, maps), use mapstructure
	// Create a new instance of the target type
	target := reflect.New(field.Type()).Interface()

	// Use mapstructure directly with the value
	config := &mapstructure.DecoderConfig{
		Result:           target,
		TagName:          "json",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			// Custom hook for string-based enums
			func(f reflect.Type, t reflect.Type, data any) (any, error) {
				if f.Kind() == reflect.String && t.Kind() == reflect.String {
					// Handle custom string types like ComponentRole, ComponentKind, etc.
					if t.String() == "manifest.ComponentRole" ||
						t.String() == "manifest.ComponentKind" ||
						t.String() == "manifest.MetricType" ||
						t.String() == "manifest.ResourcePreset" {
						return data, nil
					}
				}
				return data, nil
			},
		),
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("failed to decode value %v (type %T) to target type %v: %w", value, value, field.Type(), err)
	}

	field.Set(reflect.ValueOf(target).Elem())
	return nil
}

// isZeroValue checks if a value is the zero value for its type.
// This function determines whether a field should receive default values
// by checking if it contains the Go zero value for its type.
//
// Zero values by type:
//   - Pointers: nil
//   - Strings: empty string ("")
//   - Numbers: 0
//   - Booleans: false
//   - Slices/Maps: empty (length 0)
//   - Structs: all fields are zero values
//
// Parameters:
//   - val: Value to check
//
// Returns:
//   - bool: true if val is a zero value
//
// Examples:
//
//	isZeroValue("") -> true
//	isZeroValue("hello") -> false
//	isZeroValue(0) -> true
//	isZeroValue([]string{}) -> true
//	isZeroValue(struct{Name string}{}) -> true (empty struct)
func isZeroValue(val any) bool {
	if val == nil {
		return true
	}

	v := reflect.ValueOf(val)

	// Handle pointers
	if v.Kind() == reflect.Ptr {
		return v.IsNil()
	}

	// Handle basic types
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Slice:
		return v.Len() == 0
	case reflect.Map:
		return v.Len() == 0
	case reflect.Struct:
		// For structs, check if all fields are zero values
		for i := 0; i < v.NumField(); i++ {
			if !isZeroValue(v.Field(i).Interface()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// CreateManifestWithDefaults creates a new Manifest with default values applied.
// This is a convenience function that creates a minimal manifest structure and
// then applies all relevant defaults from the specified schema version.
//
// Parameters:
//   - projectName: Name of the project
//   - version: Schema version to use for defaults
//
// Returns:
//   - *Manifest: New manifest with defaults applied
//   - error: If creation or default application fails
//
// Example:
//
//	manifest, err := CreateManifestWithDefaults("my-app", "v1-alpha.1")
//	// Returns:
//	// &Manifest{
//	//     ApiVersion: "v1-alpha.1",
//	//     Project:    "my-app",
//	//     Components: map[string]Component{}, // Empty but initialized
//	// }
func CreateManifestWithDefaults(projectName, version string) (*Manifest, error) {
	if projectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	if version == "" {
		return nil, fmt.Errorf("version cannot be empty")
	}

	manifest := &Manifest{
		ApiVersion: version,
		Project:    projectName,
		Components: make(map[string]Component),
	}

	if err := FillManifestWithDefaults(manifest, version); err != nil {
		return nil, fmt.Errorf("failed to fill manifest with defaults: %w", err)
	}

	return manifest, nil
}

// extractComponentPattern extracts the component name pattern from the manifest schema.
// This function retrieves the regular expression pattern used in patternProperties
// for component definitions, enabling dynamic component name matching.
//
// The pattern is cached per version to avoid repeated schema parsing.
//
// Parameters:
//   - version: Schema version to extract pattern from
//
// Returns:
//   - string: Regular expression pattern, or empty string if not found
//
// Example:
//
//	pattern := extractComponentPattern("v1-alpha.1")
//	// Returns: "^[a-zA-Z0-9_-]+$"
//	// This pattern matches component names like "web", "api-server", "worker_1"
func extractComponentPattern(version string) string {
	// Check cache first
	schemaMutex.RLock()
	if cachedPattern, exists := patternCache[version]; exists {
		schemaMutex.RUnlock()
		return cachedPattern
	}
	schemaMutex.RUnlock()

	// Load and cache if not in cache
	schemaInfo, err := getCachedSchemaInfo(version, schema.SchemaTypeManifest)
	if err != nil {
		return ""
	}

	// Get the raw schema to access patternProperties
	if schemaInfo.rawData == nil {
		return ""
	}

	properties, ok := schemaInfo.rawData["properties"].(map[string]any)
	if !ok {
		return ""
	}
	components, ok := properties["components"].(map[string]any)
	if !ok {
		return ""
	}
	patternProps, ok := components["patternProperties"].(map[string]any)
	if !ok {
		return ""
	}
	for pattern := range patternProps {
		schemaMutex.Lock()
		patternCache[version] = pattern
		schemaMutex.Unlock()
		return pattern
	}
	return ""
}
