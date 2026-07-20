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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"dario.cat/mergo"
	"github.com/go-viper/mapstructure/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cast"
	"k8s.io/apimachinery/pkg/api/resource"

	"deployah.dev/deployah/internal/spec/schema"
)

// quantityType is treated as an opaque leaf by the defaults walker: its
// unexported fields must not be reflected into.
var quantityType = reflect.TypeFor[resource.Quantity]()

// isOpaqueStruct reports types that should not be walked field-by-field
// when applying schema defaults.
func isOpaqueStruct(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t == quantityType
}

// DefaultValues represents default values extracted from a JSON schema.
// Map keys are dot-notation paths to fields; values are defaults to apply.
//
// Examples of keys:
//   - "components.[^[a-zA-Z0-9_-]+$].role" -> "service"
//     (pattern-based component default)
//   - "components.web.port" -> 8080 (specific component default)
//   - "environments.[0].envFile" -> ".env.{name}"
//     (environment template with placeholder)
//   - "environments.production.configFile" -> "config.production.yaml"
//     (specific environment)
type DefaultValues map[string]any

// componentsPrefixLength avoids repeated len(ComponentsPrefix) calls in
// isComponentPath and tryComponentPatternPath.
const componentsPrefixLength = len(ComponentsPrefix)

// Global caches for schema compilation and pattern extraction.
// These caches improve performance by avoiding repeated schema compilation
// and pattern extraction operations.
//
// Cache keys follow the format: "{version}-{schemaType}"
// Example: "v1-alpha.2-spec", "v1-alpha.2-environments"
var (
	// compiledSchemaCache stores compiled JSON schemas with their raw data
	// Key format: "v1-alpha.2-spec" -> schemaInfo{compiled, rawData}
	compiledSchemaCache = make(map[string]*schemaInfo)

	// patternCache stores extracted component name patterns from schemas
	// Key format: "v1-alpha.2" -> "^[a-zA-Z0-9_-]+$"
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

// getCachedSchemaInfo returns the compiled schema and raw data for version
// and schemaType, compiling and caching it on first use. It is safe for
// concurrent use.
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
	if err = json.Unmarshal(schemaBytes, &schemaObj); err != nil {
		return nil, fmt.Errorf("failed to parse %s schema JSON for version %q: %w", schemaType, version, err)
	}

	// Compile schema using jsonschema library
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	schemaID := fmt.Sprintf("internal://%s-%s.json", schemaType, version)
	jsonSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s schema JSON for version %q: %w", schemaType, version, err)
	}

	if err = compiler.AddResource(schemaID, jsonSchema); err != nil {
		return nil, fmt.Errorf("failed to add schema resource for %s version %q: %w", schemaType, version, err)
	}

	compiledSchema, err := compiler.Compile(schemaID)
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

// ClearSchemaCache clears all cached schemas and patterns. Call it between
// tests that mutate schema caches, or to release memory in long-running
// processes.
func ClearSchemaCache() {
	schemaMutex.Lock()
	defer schemaMutex.Unlock()
	compiledSchemaCache = make(map[string]*schemaInfo)
	patternCache = make(map[string]string)
}

// extractDefaultsFromSchemaInfo walks schemaInfo's raw schema data starting
// at path and returns a map of dot-notation paths to their default values.
func extractDefaultsFromSchemaInfo(schemaInfo *schemaInfo, path string) DefaultValues {
	defaults := make(DefaultValues)

	// Use the raw schema data for extracting defaults
	if schemaInfo.rawData != nil {
		w := &defaultsWalker{root: schemaInfo.rawData, visited: make(map[string]bool)}
		w.walk(schemaInfo.rawData, path, defaults)
	}

	return defaults
}

// defaultsWalker carries the root schema so $ref pointers resolve, and a
// visited set (ref + path) so cyclic definitions terminate.
type defaultsWalker struct {
	root    map[string]any
	visited map[string]bool
}

// resolveRef follows a local "#/a/b" JSON pointer within the root schema.
func (w *defaultsWalker) resolveRef(ref string) (map[string]any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	node := any(w.root)
	for part := range strings.SplitSeq(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		node, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	m, ok := node.(map[string]any)
	return m, ok
}

// walk recursively traverses schemaData looking for "default" properties,
// following "properties", "patternProperties", "additionalProperties", and
// "items" schema keywords, and records each one found in defaults under its
// dot-notation path.
func (w *defaultsWalker) walk(schemaData any, path string, defaults DefaultValues) {
	schemaMap, ok := schemaData.(map[string]any)
	if !ok {
		return
	}

	// Follow local $ref pointers (e.g. "#/$defs/Component") at the same
	// path; the visited set stops cycles.
	if ref, exists := schemaMap["$ref"].(string); exists {
		key := ref + "|" + path
		if !w.visited[key] {
			w.visited[key] = true
			if target, resolved := w.resolveRef(ref); resolved {
				w.walk(target, path, defaults)
			}
		}
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

			w.walk(propSchema, propPath, defaults)
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

			w.walk(propSchema, propPath, defaults)
		}
	}

	// Handle map-typed additionalProperties combined with a propertyNames
	// pattern (the v1-alpha.2 layout for components/environments); the
	// pattern plays the same role as a patternProperties key.
	if addProps, exists := schemaMap["additionalProperties"].(map[string]any); exists {
		pattern := ".*"
		if propNames, hasNames := schemaMap["propertyNames"].(map[string]any); hasNames {
			if p, hasPattern := propNames["pattern"].(string); hasPattern {
				pattern = p
			}
		}
		propPath := path
		if propPath != "" {
			propPath += "."
		}
		propPath += "[" + pattern + "]"

		w.walk(addProps, propPath, defaults)
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

		w.walk(items, itemsPath, defaults)
	}
}

// GetDefaultValues returns all default values declared by the schema for
// version and schemaType, keyed by dot-notation path. It is the main entry
// point for schema-based defaults.
func GetDefaultValues(version string, schemaType schema.SchemaType) (DefaultValues, error) {
	schemaInfo, err := getCachedSchemaInfo(version, schemaType)
	if err != nil {
		return nil, err
	}

	defaults := extractDefaultsFromSchemaInfo(schemaInfo, "")

	return defaults, nil
}

// resolveResourcePresets converts each component's ResourcePreset into
// concrete Resources, then clears ResourcePreset so it does not conflict
// with the now-populated Resources during validation. Components that
// already set Resources are left untouched; components with neither
// Resources nor a preset get [ResourcePresetSmall]. Only the preset's
// "requests" values are applied; "limits" are not used.
func resolveResourcePresets(spec *Spec) error {
	for componentName, component := range spec.Components {
		// Only resolve if ResourcePreset is set and Resources are empty
		if component.ResourcePreset != "" && !component.Resources.ResourcesSet() {
			if presetResources, exists := ResourcePresetMappings[component.ResourcePreset]; exists {
				// Clone quantities so callers do not share mutable pointers from
				// the package-level ResourcePresetMappings table.
				req := presetResources["requests"]
				component.Resources = Resources{
					CPU:              cloneQuantity(req.CPU),
					Memory:           cloneQuantity(req.Memory),
					EphemeralStorage: cloneQuantity(req.EphemeralStorage),
				}
				// Clear the resourcePreset field after converting to resources
				// to avoid validation conflicts
				component.ResourcePreset = ""
				spec.Components[componentName] = component
				continue
			}
		}

		// If neither explicit resources nor a preset is provided, apply a default preset at spec layer
		if component.ResourcePreset == "" && !component.Resources.ResourcesSet() {
			if presetResources, exists := ResourcePresetMappings[ResourcePresetSmall]; exists {
				component.ResourcePreset = ResourcePresetSmall
				req := presetResources["requests"]
				component.Resources = Resources{
					CPU:              cloneQuantity(req.CPU),
					Memory:           cloneQuantity(req.Memory),
					EphemeralStorage: cloneQuantity(req.EphemeralStorage),
				}
				// Clear the resourcePreset field after converting to resources
				// to avoid validation conflicts
				component.ResourcePreset = ""
				spec.Components[componentName] = component
			}
		}
	}
	return nil
}

// FillSpecWithDefaults fills spec with defaults from the JSON schemas for
// version, in this order: apply spec schema defaults to components, resolve
// resource presets to concrete values, merge spec and environment defaults,
// then apply the merged defaults to environments with placeholder
// substitution. spec is updated in place. It returns an error if schema
// loading, default extraction, or application fails.
func FillSpecWithDefaults(spec *Spec, version string) error {
	if spec == nil {
		return fmt.Errorf("spec cannot be nil")
	}

	if version == "" {
		return fmt.Errorf("version cannot be empty")
	}

	specDefaults, err := GetDefaultValues(version, schema.SchemaTypeManifest)
	if err != nil {
		return fmt.Errorf("failed to get default values: %w", err)
	}

	envDefaults, err := GetDefaultValues(version, schema.SchemaTypeEnvironments)
	if err != nil {
		return fmt.Errorf("failed to get environment defaults: %w", err)
	}

	// Apply defaults to components
	if spec.Components == nil {
		spec.Components = make(map[string]Component)
	}

	for componentName, component := range spec.Components {
		if err = applyDefaultsRecursively(&component, specDefaults, "components."+componentName, version); err != nil {
			return fmt.Errorf("failed to apply defaults to component %s: %w", componentName, err)
		}
		spec.Components[componentName] = component
	}

	// Resolve resource presets after applying schema defaults
	if err = resolveResourcePresets(spec); err != nil {
		return fmt.Errorf("failed to resolve resource presets: %w", err)
	}

	// Merge spec and environment defaults for environments
	mergedDefaults := make(DefaultValues)
	// Copy spec defaults first
	if err = mergo.Merge(&mergedDefaults, specDefaults); err != nil {
		return fmt.Errorf("failed to merge spec defaults: %w", err)
	}
	// Merge environment defaults with override to allow env defaults to take precedence
	if err = mergo.Merge(&mergedDefaults, envDefaults, mergo.WithOverride); err != nil {
		return fmt.Errorf("failed to merge environment defaults: %w", err)
	}

	// Apply environment defaults
	for name := range spec.Environments {
		env := spec.Environments[name]
		if err = applyDefaultsRecursively(&env, mergedDefaults, "environments."+name, version); err != nil {
			return fmt.Errorf("failed to apply defaults to environment %s: %w", name, err)
		}
		spec.Environments[name] = env
	}

	return nil
}

// substitutePlaceholders replaces [PlaceholderName] ("{name}") in value with
// the cleaned form of envName (see [cleanEnvironmentName]). It returns value
// unchanged if value is not a non-empty string.
func substitutePlaceholders(value any, envName string) any {
	cleanEnvName := cleanEnvironmentName(envName)
	str := cast.ToString(value)
	if str != "" {
		return strings.ReplaceAll(str, PlaceholderName, cleanEnvName)
	}
	return value
}

// applyDefaultsToField sets field to its default value from defaults if the
// field is settable and currently zero. It tries the field's direct
// dot-notation path first, then falls back to an environment index path or
// a component pattern path (see [tryAlternativeDefaultPaths]) depending on
// currentPath.
func applyDefaultsToField(field reflect.Value, fieldType reflect.StructField, defaults DefaultValues, currentPath, version, envName string) error {
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

// applyFieldValue substitutes envName into defaultVal's placeholders (see
// [substitutePlaceholders]) and sets field to the result. fieldName and
// currentPath are used only to name the field in a returned error.
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
		idxPath := EnvironmentsPrefix + ArrayItemIndexTemplate + "." + fieldName
		if defaultVal, exists := defaults[idxPath]; exists {
			return applyFieldValue(field, defaultVal, envName, fieldName, currentPath)
		}
	}

	// Try pattern property path for components
	if isComponentPath(currentPath) {
		return tryComponentPatternPath(field, fieldName, currentPath, defaults, version)
	}

	return nil
}

// tryComponentPatternPath tries to apply defaults using component pattern paths
func tryComponentPatternPath(field reflect.Value, fieldName, currentPath string, defaults DefaultValues, version string) error {
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
func processStructField(field reflect.Value, fieldType reflect.StructField, defaults DefaultValues, currentPath, version string) error {
	fieldName := getJSONFieldName(fieldType)
	newPath := buildFieldPath(currentPath, fieldName)

	switch field.Kind() {
	case reflect.Pointer:
		// Handle pointer fields (only recurse into pointers to structs, e.g., *Autoscaling)
		if !field.IsNil() {
			if field.Elem().Kind() == reflect.Struct && !isOpaqueStruct(field.Elem().Type()) {
				if err := applyDefaultsRecursively(field.Interface(), defaults, newPath, version); err != nil {
					return fmt.Errorf("failed to apply defaults to pointer field %s at path %s: %w", fieldName, currentPath, err)
				}
			}
		}
	case reflect.Struct:
		if isOpaqueStruct(field.Type()) {
			return nil
		}
		if err := applyDefaultsRecursively(field.Addr().Interface(), defaults, newPath, version); err != nil {
			return fmt.Errorf("failed to apply defaults to struct field %s at path %s: %w", fieldName, currentPath, err)
		}
	case reflect.Map:
		if err := applyDefaultsToMap(field, defaults, newPath, version); err != nil {
			return fmt.Errorf("failed to apply defaults to map field %s at path %s: %w", fieldName, currentPath, err)
		}
	case reflect.Slice:
		if err := applyDefaultsToSlice(field, defaults, newPath, version); err != nil {
			return fmt.Errorf("failed to apply defaults to slice field %s at path %s: %w", fieldName, currentPath, err)
		}
	}

	return nil
}

// applyDefaultsRecursively applies default values to a struct recursively
func applyDefaultsRecursively(obj any, defaults DefaultValues, currentPath, version string) error {
	if obj == nil {
		return fmt.Errorf("cannot apply defaults to nil object at path %s", currentPath)
	}

	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Pointer {
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
			return fmt.Errorf("failed to apply defaults to field: %w", err)
		}

		// Always process struct fields recursively
		if err := processStructField(field, fieldType, defaults, currentPath, version); err != nil {
			return fmt.Errorf("failed to process struct field: %w", err)
		}
	}

	return nil
}

// isComponentPath reports whether path is under the "components." prefix
// with at least one path segment after it (a bare "components." prefix with
// nothing after it does not count).
func isComponentPath(path string) bool {
	return len(path) > componentsPrefixLength && strings.HasPrefix(path, ComponentsPrefix)
}

// isEnvironmentPath reports whether path is under the "environments."
// prefix.
func isEnvironmentPath(path string) bool {
	return strings.HasPrefix(path, EnvironmentsPrefix)
}

// buildFieldPath joins currentPath and fieldName with a dot, or returns
// fieldName alone if currentPath is empty.
func buildFieldPath(currentPath, fieldName string) string {
	if currentPath == "" {
		return fieldName
	}
	return currentPath + "." + fieldName
}

// extractEnvironmentName returns the environment name from an
// "environments.<name>..." path, or "" if path is not an environment path.
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

// cleanEnvironmentName strips the [EnvFileSuffix] suffix from envName, if
// present.
func cleanEnvironmentName(envName string) string {
	if before, ok := strings.CutSuffix(envName, EnvFileSuffix); ok {
		return before
	}
	return envName
}

// buildComponentPatternPath builds the pattern-property schema path for
// fieldName, e.g. "components.[<pattern>].port" for a direct field or
// "components.[<pattern>].<restPath>.<fieldName>" for a nested one. This
// lets any component name matching pattern share the same schema default.
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

// applyDefaultsToMap applies default values to a map (using [reflect.Value]).
func applyDefaultsToMap(mapVal reflect.Value, defaults DefaultValues, currentPath, version string) error {
	if mapVal.Kind() != reflect.Map {
		return nil
	}
	for _, key := range mapVal.MapKeys() {
		value := mapVal.MapIndex(key)
		keyStr := fmt.Sprintf("%v", key.Interface())
		newPath := buildFieldPath(currentPath, keyStr)
		switch value.Kind() {
		case reflect.Map:
			if err := applyDefaultsToMap(value, defaults, newPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to nested map at key %s in path %s: %w", keyStr, currentPath, err)
			}
		case reflect.Slice:
			if err := applyDefaultsToSlice(value, defaults, newPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to slice at key %s in path %s: %w", keyStr, currentPath, err)
			}
		case reflect.Struct:
			if err := applyDefaultsRecursively(value.Addr().Interface(), defaults, newPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to struct at key %s in path %s: %w", keyStr, currentPath, err)
			}
		}
	}
	return nil
}

// applyDefaultsToSlice applies default values to a slice (using [reflect.Value]).
func applyDefaultsToSlice(sliceVal reflect.Value, defaults DefaultValues, currentPath, version string) error {
	if sliceVal.Kind() != reflect.Slice {
		return nil
	}
	for i := 0; i < sliceVal.Len(); i++ {
		item := sliceVal.Index(i)
		itemPath := fmt.Sprintf("%s[%d]", currentPath, i)
		switch item.Kind() {
		case reflect.Map:
			if err := applyDefaultsToMap(item, defaults, itemPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to map item at index %d in path %s: %w", i, currentPath, err)
			}
		case reflect.Slice:
			if err := applyDefaultsToSlice(item, defaults, itemPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to nested slice at index %d in path %s: %w", i, currentPath, err)
			}
		case reflect.Struct:
			if isOpaqueStruct(item.Type()) {
				continue
			}
			if err := applyDefaultsRecursively(item.Addr().Interface(), defaults, itemPath, version); err != nil {
				return fmt.Errorf("failed to apply defaults to struct item at index %d in path %s: %w", i, currentPath, err)
			}
		}
	}
	return nil
}

// setFieldValue sets field to value, converting types as needed: direct
// assignment when the types already match, [resource.Quantity] parsing for
// resource fields, [github.com/spf13/cast] for basic scalar kinds, and
// mapstructure for structs, slices, and maps. field must be settable.
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

	// Schema defaults for resources are strings; convert into Quantity.
	if field.Type() == reflect.TypeFor[*resource.Quantity]() {
		s := cast.ToString(value)
		if s == "" {
			return nil
		}
		q, err := resource.ParseQuantity(s)
		if err != nil {
			return fmt.Errorf("invalid resource quantity %q: %w", s, err)
		}
		field.Set(reflect.ValueOf(&q))
		return nil
	}
	if field.Type() == quantityType {
		s := cast.ToString(value)
		if s == "" {
			return nil
		}
		q, err := resource.ParseQuantity(s)
		if err != nil {
			return fmt.Errorf("invalid resource quantity %q: %w", s, err)
		}
		field.Set(reflect.ValueOf(q))
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
			func(f, t reflect.Type, data any) (any, error) {
				if f.Kind() == reflect.String && t.Kind() == reflect.String {
					// Handle custom string types like ComponentRole, ComponentKind, etc.
					if t.String() == "spec.ComponentRole" ||
						t.String() == "spec.ComponentKind" ||
						t.String() == "spec.MetricType" ||
						t.String() == "spec.ResourcePreset" {
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

	if err = decoder.Decode(value); err != nil {
		return fmt.Errorf("failed to decode value %v (type %T) to target type %v: %w", value, value, field.Type(), err)
	}

	field.Set(reflect.ValueOf(target).Elem())
	return nil
}

// isZeroValue reports whether val is the Go zero value for its type. A nil
// val, and an empty (but non-nil) slice or map, both count as zero; a
// [resource.Quantity] is zero when [resource.Quantity.IsZero] reports true.
func isZeroValue(val any) bool {
	if val == nil {
		return true
	}
	v := reflect.ValueOf(val)
	// Empty (non-nil) slices/maps are zero for defaults; IsZero only treats nil.
	switch v.Kind() {
	case reflect.Slice, reflect.Map:
		return v.Len() == 0
	case reflect.Struct:
		// Quantity.IsZero is semantic; reflect field-walk is not enough.
		if v.Type() == quantityType {
			q, ok := v.Interface().(resource.Quantity)
			return ok && q.IsZero()
		}
	}
	return v.IsZero()
}

// CreateSpecWithDefaults creates a minimal [Spec] for projectName and fills
// it with the defaults declared by version's schema.
func CreateSpecWithDefaults(projectName, version string) (*Spec, error) {
	if projectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	if version == "" {
		return nil, fmt.Errorf("version cannot be empty")
	}

	spec := &Spec{
		APIVersion: version,
		Project:    projectName,
		Components: make(map[string]Component),
	}

	if err := FillSpecWithDefaults(spec, version); err != nil {
		return nil, fmt.Errorf("failed to fill spec with defaults: %w", err)
	}

	return spec, nil
}

// extractComponentPattern returns the regular expression pattern that
// component names must match under version's schema (e.g.
// "^[a-zA-Z0-9_-]+$"), or "" if the schema declares none. The result is
// cached per version.
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
	if propNames, hasNames := components["propertyNames"].(map[string]any); hasNames {
		if pattern, hasPattern := propNames["pattern"].(string); hasPattern {
			schemaMutex.Lock()
			patternCache[version] = pattern
			schemaMutex.Unlock()
			return pattern
		}
	}
	return ""
}
