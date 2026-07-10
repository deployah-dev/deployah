package spec

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"deployah.dev/deployah/internal/spec/schema"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaTypesAtPath resolves the JSON Schema types declared for a dotted
// field path into the compiled manifest schema, e.g. path []string{"components",
// "web", "port"} resolves to []string{"integer"}. A false result means
// "unknown, don't guess" rather than an error.
func SchemaTypesAtPath(version string, path []string) ([]string, bool) {
	compiled, err := compileManifestSchema(version)
	if err != nil {
		return nil, false
	}

	current := compiled
	for _, seg := range path {
		next, ok := descendSchema(current, seg)
		if !ok {
			return nil, false
		}
		current = next
	}

	types := schemaTypes(current)
	if types == nil {
		return nil, false
	}
	return types, true
}

// descendSchema resolves one path segment, looking through $ref and
// anyOf/oneOf members (e.g. the boolean|object expose union).
func descendSchema(s *jsonschema.Schema, seg string) (*jsonschema.Schema, bool) {
	for _, candidate := range unionMembers(s) {
		if next, hasSeg := candidate.Properties[seg]; hasSeg {
			return next, true
		}
		// An arbitrary map key, e.g. a component or environment name.
		if next, hasAdditional := candidate.AdditionalProperties.(*jsonschema.Schema); hasAdditional {
			return next, true
		}
	}
	return nil, false
}

// unionMembers returns s plus its anyOf/oneOf members, all deref'd.
func unionMembers(s *jsonschema.Schema) []*jsonschema.Schema {
	s = derefSchema(s)
	members := []*jsonschema.Schema{s}
	for _, m := range s.AnyOf {
		members = append(members, derefSchema(m))
	}
	for _, m := range s.OneOf {
		members = append(members, derefSchema(m))
	}
	return members
}

// schemaTypes reports the declared types of s, unioning anyOf/oneOf member
// types when s declares none itself. A field can allow more than one type,
// e.g. an env var value typed string|number|boolean.
func schemaTypes(s *jsonschema.Schema) []string {
	var types []string
	for _, m := range unionMembers(s) {
		if m.Types != nil {
			types = append(types, m.Types.ToStrings()...)
		}
	}
	slices.Sort(types)
	return slices.Compact(types)
}

// derefSchema follows compiled $ref indirection to the schema it points to.
// The compiler resolves $ref at compile time, so this is just a pointer
// hop rather than a JSON Pointer re-implementation.
func derefSchema(s *jsonschema.Schema) *jsonschema.Schema {
	for s.Ref != nil {
		s = s.Ref
	}
	return s
}

// CoerceSetValue upgrades the string leaf strvals.ParseIntoString stored in
// obj to a properly typed Go value (int64, float64, or bool) by consulting
// the manifest schema's declared type for the dotted key path in kv.
func CoerceSetValue(kv string, obj map[string]any, version string) error {
	key, _, ok := strings.Cut(kv, "=")
	if !ok {
		return nil
	}
	path := strings.Split(key, ".")

	types, ok := SchemaTypesAtPath(version, path)
	// Skip: unknown path, or the field allows a string as-is.
	if !ok || slices.Contains(types, "string") {
		return nil
	}

	str, ok := valueAtPath(obj, path).(string)
	if !ok {
		return nil
	}

	strict := len(types) == 1
	if slices.Contains(types, "boolean") {
		if b, err := strconv.ParseBool(str); err == nil {
			setAtPath(obj, path, b)
			return nil
		} else if strict {
			return fmt.Errorf("value %q is not a valid boolean", str)
		}
	}
	if slices.Contains(types, "integer") {
		if n, err := strconv.ParseInt(str, 10, 64); err == nil {
			setAtPath(obj, path, n)
			return nil
		} else if strict {
			return fmt.Errorf("value %q is not a valid integer", str)
		}
	}
	if slices.Contains(types, "number") {
		if n, err := strconv.ParseFloat(str, 64); err == nil {
			setAtPath(obj, path, n)
			return nil
		} else if strict {
			return fmt.Errorf("value %q is not a valid number", str)
		}
	}
	return nil
}

// valueAtPath returns the value at the dotted path within m, or nil if any
// segment is missing.
func valueAtPath(m map[string]any, path []string) any {
	var current any = m
	for _, seg := range path {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[seg]
	}
	return current
}

// setAtPath overwrites the leaf at the dotted path within m. It assumes
// every intermediate segment already exists as a map[string]any, which
// holds here because strvals.ParseIntoString creates them.
func setAtPath(m map[string]any, path []string, value any) {
	for _, seg := range path[:len(path)-1] {
		next, ok := m[seg].(map[string]any)
		if !ok {
			return
		}
		m = next
	}
	m[path[len(path)-1]] = value
}

// compileManifestSchema compiles the manifest schema for version, the same
// way [validateYAMLAgainstSchema] does.
func compileManifestSchema(version string) (*jsonschema.Schema, error) {
	schemaBytes, err := schema.GetManifestSchema(version)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest schema version %q: %w", version, err)
	}

	compiler := jsonschema.NewCompiler()
	schemaID := filepath.Join(version, "manifest.json")
	jsonSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid manifest schema JSON for version %q: %w", version, err)
	}
	if err = compiler.AddResource(schemaID, jsonSchema); err != nil {
		return nil, fmt.Errorf("failed to load manifest schema version %q: %w", version, err)
	}
	return compiler.Compile(schemaID)
}
