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
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	"deployah.dev/deployah/internal/spec/schema"
)

// TestExtractDefaultsFromSchemaData tests extractDefaultsFromSchemaData.
func TestExtractDefaultsFromSchemaData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		schema   map[string]any
		path     string
		expected DefaultValues
	}{
		{
			name: "simple default value",
			schema: map[string]any{
				"default": "test-value",
			},
			path: "test.path",
			expected: DefaultValues{
				"test.path": "test-value",
			},
		},
		{
			name: "nested properties with defaults",
			schema: map[string]any{
				"properties": map[string]any{
					"field1": map[string]any{
						"default": "value1",
					},
					"field2": map[string]any{
						"default": 42,
					},
				},
			},
			path: "root",
			expected: DefaultValues{
				"root.field1": "value1",
				"root.field2": 42,
			},
		},
		{
			name: "pattern properties with defaults",
			schema: map[string]any{
				"patternProperties": map[string]any{
					"^[a-z]+$": map[string]any{
						"default": "pattern-value",
					},
				},
			},
			path: "components",
			expected: DefaultValues{
				"components.[^[a-z]+$]": "pattern-value",
			},
		},
		{
			name: "array items with defaults",
			schema: map[string]any{
				"items": map[string]any{
					"default": "array-item",
				},
			},
			path: "list",
			expected: DefaultValues{
				"list.[0]": "array-item",
			},
		},
		{
			name: "complex nested structure",
			schema: map[string]any{
				"default": "root-default",
				"properties": map[string]any{
					"nested": map[string]any{
						"properties": map[string]any{
							"deep": map[string]any{
								"default": "deep-value",
							},
						},
					},
				},
			},
			path: "root",
			expected: DefaultValues{
				"root":             "root-default",
				"root.nested.deep": "deep-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defaults := make(DefaultValues)
			w := &defaultsWalker{root: tt.schema, visited: make(map[string]bool)}
			w.walk(tt.schema, tt.path, defaults)
			assert.Equal(t, tt.expected, defaults)
		})
	}
}

// TestGetDefaultValues tests the GetDefaultValues function
func TestGetDefaultValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		version    string
		schemaType schema.SchemaType
		expectErr  bool
	}{
		{
			name:       "valid manifest schema",
			version:    "v1-alpha.2",
			schemaType: schema.SchemaTypeManifest,
			expectErr:  false,
		},
		{
			name:       "valid environments schema",
			version:    "v1-alpha.2",
			schemaType: schema.SchemaTypeEnvironments,
			expectErr:  false,
		},
		{
			name:       "invalid version",
			version:    "invalid-version",
			schemaType: schema.SchemaTypeManifest,
			expectErr:  true,
		},
		{
			name:       "unsupported schema type",
			version:    "v1-alpha.2",
			schemaType: "unsupported",
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defaults, err := GetDefaultValues(tt.version, tt.schemaType)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, defaults)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, defaults)
				// The environments schema declares no defaults in
				// v1-alpha.2; only the manifest schema must be non-empty.
				if tt.schemaType == schema.SchemaTypeManifest {
					assert.NotEmpty(t, defaults)
				}
			}
		})
	}
}

// TestFillSpecWithDefaults tests the FillSpecWithDefaults function
func TestFillSpecWithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		manifest  *Spec
		version   string
		expectErr bool
	}{
		{
			name: "valid manifest with components",
			manifest: &Spec{
				APIVersion: "v1-alpha.2",
				Project:    "test-project",
				Components: map[string]Component{
					"web": {
						Image: "nginx:latest",
					},
				},
			},
			version:   "v1-alpha.2",
			expectErr: false,
		},
		{
			name: "manifest with nil components",
			manifest: &Spec{
				APIVersion: "v1-alpha.2",
				Project:    "test-project",
				Components: nil,
			},
			version:   "v1-alpha.2",
			expectErr: false,
		},
		{
			name: "manifest with environments",
			manifest: &Spec{
				APIVersion: "v1-alpha.2",
				Project:    "test-project",
				Components: map[string]Component{},
				Environments: map[string]Environment{
					"production": {},
				},
			},
			version:   "v1-alpha.2",
			expectErr: false,
		},
		{
			name: "invalid version",
			manifest: &Spec{
				APIVersion: "v1-alpha.2",
				Project:    "test-project",
				Components: map[string]Component{},
			},
			version:   "invalid-version",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := FillSpecWithDefaults(tt.manifest, tt.version)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify that components map was initialized if it was nil
				if tt.manifest.Components == nil {
					assert.NotNil(t, tt.manifest.Components)
				}
			}
		})
	}
}

// TestApplyDefaultsRecursively tests the applyDefaultsRecursively function
func TestApplyDefaultsRecursively(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		obj      any
		defaults DefaultValues
		path     string
		expected any
	}{
		{
			name: "apply string default to zero value",
			obj: &struct {
				Field string `json:"field"`
			}{},
			defaults: DefaultValues{
				"test.field": "default-value",
			},
			path: "test",
			expected: &struct {
				Field string `json:"field"`
			}{
				Field: "default-value",
			},
		},
		{
			name: "apply int default to zero value",
			obj: &struct {
				Field int `json:"field"`
			}{},
			defaults: DefaultValues{
				"test.field": 42,
			},
			path: "test",
			expected: &struct {
				Field int `json:"field"`
			}{
				Field: 42,
			},
		},
		{
			name: "apply bool default to zero value",
			obj: &struct {
				Field bool `json:"field"`
			}{},
			defaults: DefaultValues{
				"test.field": true,
			},
			path: "test",
			expected: &struct {
				Field bool `json:"field"`
			}{
				Field: true,
			},
		},
		{
			name: "don't apply default to non-zero value",
			obj: &struct {
				Field string `json:"field"`
			}{
				Field: "existing-value",
			},
			defaults: DefaultValues{
				"test.field": "default-value",
			},
			path: "test",
			expected: &struct {
				Field string `json:"field"`
			}{
				Field: "existing-value",
			},
		},
		{
			name: "apply default to nested struct",
			obj: &struct {
				Nested struct {
					Field string `json:"field"`
				} `json:"nested"`
			}{},
			defaults: DefaultValues{
				"test.nested.field": "nested-default",
			},
			path: "test",
			expected: &struct {
				Nested struct {
					Field string `json:"field"`
				} `json:"nested"`
			}{
				Nested: struct {
					Field string `json:"field"`
				}{
					Field: "nested-default",
				},
			},
		},
		{
			name: "apply default to pointer field",
			obj: &struct {
				PtrField *Autoscaling `json:"ptrField"`
			}{
				PtrField: &Autoscaling{}, // Initialize the pointer
			},
			defaults: DefaultValues{
				"test.ptrField.enabled":     true,
				"test.ptrField.minReplicas": 2,
				"test.ptrField.maxReplicas": 5,
			},
			path: "test",
			expected: &struct {
				PtrField *Autoscaling `json:"ptrField"`
			}{
				PtrField: &Autoscaling{
					Enabled:     true,
					MinReplicas: 2,
					MaxReplicas: 5,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, applyDefaultsRecursively(tt.obj, tt.defaults, tt.path, "v1-alpha.2"))
			assert.Equal(t, tt.expected, tt.obj)
		})
	}
}

// TestApplyDefaultsToMap tests the applyDefaultsToMap function
func TestApplyDefaultsToMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapVal   reflect.Value
		defaults DefaultValues
		path     string
	}{
		{
			name: "apply defaults to map with struct values",
			mapVal: reflect.ValueOf(map[string]*Component{
				"web": {Image: "nginx:latest"},
			}),
			defaults: DefaultValues{
				"components.web.role": "service",
				"components.web.port": 8080,
			},
			path: "components",
		},
		{
			name: "apply defaults to map with slice values",
			mapVal: reflect.ValueOf(map[string]*[]string{
				"commands": {"echo", "hello"},
			}),
			defaults: DefaultValues{},
			path:     "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// This test mainly ensures the function doesn't panic
			require.NoError(t, applyDefaultsToMap(tt.mapVal, tt.defaults, tt.path, "v1-alpha.2"))
			// No specific assertions as this is mainly testing for panics
		})
	}
}

// TestApplyDefaultsToSlice tests the applyDefaultsToSlice function
func TestApplyDefaultsToSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sliceVal reflect.Value
		defaults DefaultValues
		path     string
	}{
		{
			name: "apply defaults to slice with struct items",
			sliceVal: reflect.ValueOf([]*Environment{
				{},
			}),
			defaults: DefaultValues{
				"environments[0].envFile": ".env.prod",
			},
			path: "environments",
		},
		{
			name:     "apply defaults to empty slice",
			sliceVal: reflect.ValueOf([]*string{}),
			defaults: DefaultValues{},
			path:     "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// This test mainly ensures the function doesn't panic
			require.NoError(t, applyDefaultsToSlice(tt.sliceVal, tt.defaults, tt.path, "v1-alpha.2"))
			// No specific assertions as this is mainly testing for panics
		})
	}
}

// TestSetFieldValue uses [reflect.New] to get settable values for primitives.
func TestSetFieldValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		field    reflect.Value
		value    any
		expected any
	}{
		{
			name:     "set string field",
			field:    reflect.New(reflect.TypeFor[string]()).Elem(),
			value:    "test-string",
			expected: "test-string",
		},
		{
			name:     "set int field",
			field:    reflect.New(reflect.TypeFor[int]()).Elem(),
			value:    42,
			expected: 42,
		},
		{
			name:     "set bool field",
			field:    reflect.New(reflect.TypeFor[bool]()).Elem(),
			value:    true,
			expected: true,
		},
		{
			name:     "set float64 field from float64",
			field:    reflect.New(reflect.TypeFor[float64]()).Elem(),
			value:    3.14,
			expected: 3.14,
		},
		{
			name:     "set int field from float64",
			field:    reflect.New(reflect.TypeFor[int]()).Elem(),
			value:    42.0,
			expected: 42,
		},
		{
			name:     "set uint field from float64",
			field:    reflect.New(reflect.TypeFor[uint]()).Elem(),
			value:    42.0,
			expected: uint(42),
		},
		{
			name:     "set slice field from []any",
			field:    reflect.New(reflect.TypeFor[[]string]()).Elem(),
			value:    []any{"item1", "item2"},
			expected: []string{"item1", "item2"}, // mapstructure correctly converts []any to []string
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, setFieldValue(tt.field, tt.value))
			assert.Equal(t, tt.expected, tt.field.Interface())
		})
	}
}

// TestIsZeroValue tests the isZeroValue function
func TestIsZeroValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: true,
		},
		{
			name:     "empty string",
			value:    "",
			expected: true,
		},
		{
			name:     "non-empty string",
			value:    "test",
			expected: false,
		},
		{
			name:     "zero int",
			value:    0,
			expected: true,
		},
		{
			name:     "non-zero int",
			value:    42,
			expected: false,
		},
		{
			name:     "false bool",
			value:    false,
			expected: true,
		},
		{
			name:     "true bool",
			value:    true,
			expected: false,
		},
		{
			name:     "zero float",
			value:    0.0,
			expected: true,
		},
		{
			name:     "non-zero float",
			value:    3.14,
			expected: false,
		},
		{
			name:     "empty slice",
			value:    []string{},
			expected: true,
		},
		{
			name:     "non-empty slice",
			value:    []string{"item"},
			expected: false,
		},
		{
			name:     "empty map",
			value:    map[string]string{},
			expected: true,
		},
		{
			name:     "non-empty map",
			value:    map[string]string{"key": "value"},
			expected: false,
		},
		{
			name:     "nil pointer",
			value:    (*string)(nil),
			expected: true,
		},
		{
			name:     "non-nil pointer",
			value:    &[]string{"test"}[0],
			expected: false,
		},
		{
			name: "empty struct",
			value: struct {
				Field string
			}{},
			expected: true,
		},
		{
			name: "struct with non-zero field",
			value: struct {
				Field string
			}{
				Field: "test",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isZeroValue(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCreateSpecWithDefaults tests the CreateSpecWithDefaults function
func TestCreateSpecWithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		projectName string
		version     string
		expectErr   bool
	}{
		{
			name:        "valid manifest creation",
			projectName: "test-project",
			version:     "v1-alpha.2",
			expectErr:   false,
		},
		{
			name:        "invalid version",
			projectName: "test-project",
			version:     "invalid-version",
			expectErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifest, err := CreateSpecWithDefaults(tt.projectName, tt.version)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, manifest)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manifest)
				assert.Equal(t, tt.projectName, manifest.Project)
				assert.Equal(t, tt.version, manifest.APIVersion)
				assert.NotNil(t, manifest.Components)
			}
		})
	}
}

// TestGetJSONFieldName tests the getJSONFieldName function
func TestGetJSONFieldName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		field    reflect.StructField
		expected string
	}{
		{
			name: "field with json tag",
			field: reflect.StructField{
				Name: "FieldName",
				Tag:  `json:"json_name"`,
			},
			expected: "json_name",
		},
		{
			name: "field with json tag and omitempty",
			field: reflect.StructField{
				Name: "FieldName",
				Tag:  `json:"json_name,omitempty"`,
			},
			expected: "json_name",
		},
		{
			name: "field without json tag",
			field: reflect.StructField{
				Name: "FieldName",
				Tag:  "",
			},
			expected: "FieldName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := getJSONFieldName(tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsComponentPath tests the isComponentPath function
func TestIsComponentPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "valid component path",
			path:     "components.web",
			expected: true,
		},
		{
			name:     "component path with nested field",
			path:     "components.web.port",
			expected: true,
		},
		{
			name:     "not a component path",
			path:     "environments.prod",
			expected: false,
		},
		{
			name:     "short path",
			path:     "comp",
			expected: false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isComponentPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIntegration tests integration scenarios with real schema defaults
func TestIntegration(t *testing.T) {
	t.Parallel()

	t.Run("create manifest with defaults and verify component defaults", func(t *testing.T) {
		t.Parallel()

		manifest, err := CreateSpecWithDefaults("test-project", "v1-alpha.2")
		assert.NoError(t, err)
		assert.NotNil(t, manifest)

		// Add a component and verify defaults are applied
		manifest.Components["web"] = Component{
			Image: "nginx:latest",
		}

		err = FillSpecWithDefaults(manifest, "v1-alpha.2")
		assert.NoError(t, err)

		webComponent := manifest.Components["web"]
		// Verify that defaults from schema are applied
		assert.Equal(t, ComponentRoleService, webComponent.Role)
		assert.Equal(t, ComponentKindStateless, webComponent.Kind)
		assert.Equal(t, 8080, webComponent.Port)
	})

	t.Run("verify autoscaling defaults", func(t *testing.T) {
		t.Parallel()

		manifest := &Spec{
			APIVersion: "v1-alpha.2",
			Project:    "test-project",
			Components: map[string]Component{
				"api": {
					Image: "api:latest",
					Autoscaling: &Autoscaling{
						Enabled: true,
					},
				},
			},
		}

		err := FillSpecWithDefaults(manifest, "v1-alpha.2")
		assert.NoError(t, err)

		apiComponent := manifest.Components["api"]
		assert.NotNil(t, apiComponent.Autoscaling)
		assert.Equal(t, 2, apiComponent.Autoscaling.MinReplicas)
		assert.Equal(t, 5, apiComponent.Autoscaling.MaxReplicas)
		assert.Len(t, apiComponent.Autoscaling.Metrics, 1)
		assert.Equal(t, MetricTypeCPU, apiComponent.Autoscaling.Metrics[0].Type)
		assert.Equal(t, 75, apiComponent.Autoscaling.Metrics[0].Target)
	})

	t.Run("verify environment defaults", func(t *testing.T) {
		t.Parallel()

		manifest := &Spec{
			APIVersion: "v1-alpha.2",
			Project:    "test-project",
			Components: map[string]Component{},
			Environments: map[string]Environment{
				"production": {},
			},
		}

		err := FillSpecWithDefaults(manifest, "v1-alpha.2")
		assert.NoError(t, err)

		// v1-alpha.2 declares no envFile/configFile defaults: the loader's
		// convention-based lookup replaced them.
		assert.Empty(t, manifest.Environments["production"].EnvFile)
		assert.Empty(t, manifest.Environments["production"].ConfigFile)
	})
}

// TestDefaultValuesJSON verifies DefaultValues round-trips through JSON,
// comparing after normalizing int/float types (JSON numbers decode as
// float64).
func TestDefaultValuesJSON(t *testing.T) {
	t.Parallel()

	defaults := DefaultValues{
		"components.web.role": "service",
		"components.web.port": 8080,
		"components.web.kind": "stateless",
	}

	// Test marshaling
	jsonData, err := json.Marshal(defaults)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonData)

	// Test unmarshaling
	var unmarshaled DefaultValues
	err = json.Unmarshal(jsonData, &unmarshaled)
	assert.NoError(t, err)

	// Normalize types for comparison
	normalize := func(m DefaultValues) map[string]any {
		n := make(map[string]any, len(m))
		for k, v := range m {
			switch x := v.(type) {
			case float64:
				// If the value is a whole number, convert to int
				if x == float64(int(x)) {
					n[k] = int(x)
				} else {
					n[k] = x
				}
			default:
				n[k] = v
			}
		}
		return n
	}
	assert.Equal(t, normalize(defaults), normalize(unmarshaled))
}

// TestDefaultValuesCopy tests copying of DefaultValues
func TestDefaultValuesCopy(t *testing.T) {
	t.Parallel()

	original := DefaultValues{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	// Test using maps.Copy for independent map copies.
	copied := make(DefaultValues)
	maps.Copy(copied, original)
	assert.Equal(t, original, copied)

	// Verify they are independent
	copied["key4"] = "new-value"
	assert.NotEqual(t, original, copied)
}

// TestFillSpecWithDefaults_GuardClauses verifies the nil-spec and
// empty-version guard clauses, which [TestFillSpecWithDefaults] above does
// not exercise.
func TestFillSpecWithDefaults_GuardClauses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		spec        *Spec
		version     string
		errContains string
	}{
		{name: "nil spec returns error", spec: nil, version: "v1-alpha.2", errContains: "spec cannot be nil"},
		{name: "empty version returns error", spec: &Spec{Project: "test"}, version: "", errContains: "version cannot be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := FillSpecWithDefaults(tt.spec, tt.version)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestApplyDefaultsToMap_EdgeCases verifies applyDefaultsToMap handles
// non-map input, and maps whose values are maps, slices, or pointers to
// structs, without panicking.
func TestApplyDefaultsToMap_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    reflect.Value
		defaults DefaultValues
		path     string
		check    func(t *testing.T, value reflect.Value)
	}{
		{
			name:     "non-map input is a no-op",
			value:    reflect.ValueOf(42),
			defaults: DefaultValues{"x": 1},
			path:     "test",
		},
		{
			name:     "map with nested map value recurses without error",
			value:    reflect.ValueOf(map[string]map[string]string{"outer": {"inner": "value"}}),
			defaults: DefaultValues{},
			path:     "test",
			check: func(t *testing.T, value reflect.Value) {
				t.Helper()
				m, ok := value.Interface().(map[string]map[string]string)
				require.True(t, ok)
				assert.Equal(t, "value", m["outer"]["inner"])
			},
		},
		{
			name:     "map with slice value recurses without error",
			value:    reflect.ValueOf(map[string][]string{"cmd": {"echo", "hi"}}),
			defaults: DefaultValues{},
			path:     "test",
			check: func(t *testing.T, value reflect.Value) {
				t.Helper()
				m, ok := value.Interface().(map[string][]string)
				require.True(t, ok)
				assert.Equal(t, []string{"echo", "hi"}, m["cmd"])
			},
		},
		{
			name: "map with pointer-to-struct value applies defaults without panicking",
			// map[string]*Component mirrors the pointer-valued map shape
			// used elsewhere in the codebase (real reflect.Value map
			// entries are never addressable, so a genuine non-pointer
			// struct entry would panic on value.Addr(); the pointer
			// indirection sidesteps that).
			value:    reflect.ValueOf(map[string]*Component{"web": {Image: "nginx:latest"}}),
			defaults: DefaultValues{"components.web.role": "service"},
			path:     "components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := applyDefaultsToMap(tt.value, tt.defaults, tt.path, "v1-alpha.2")
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, tt.value)
			}
		})
	}
}

// TestApplyDefaultsToSlice_EdgeCases verifies applyDefaultsToSlice handles
// non-slice input, and slices of maps, structs, and opaque structs.
func TestApplyDefaultsToSlice_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    reflect.Value
		defaults DefaultValues
		path     string
		check    func(t *testing.T, value reflect.Value)
	}{
		{
			name:     "non-slice input is a no-op",
			value:    reflect.ValueOf("not-a-slice"),
			defaults: DefaultValues{"x": 1},
			path:     "test",
		},
		{
			name:     "slice with map items recurses without error",
			value:    reflect.ValueOf([]map[string]string{{"key": "value"}}),
			defaults: DefaultValues{},
			path:     "test",
			check: func(t *testing.T, value reflect.Value) {
				t.Helper()
				s, ok := value.Interface().([]map[string]string)
				require.True(t, ok)
				assert.Equal(t, "value", s[0]["key"])
			},
		},
		{
			name:  "slice with struct items applies defaults to each item",
			value: reflect.ValueOf([]Environment{{}, {}}),
			defaults: DefaultValues{
				"envs[0].envFile": ".env.a",
				"envs[1].envFile": ".env.b",
			},
			path: "envs",
			check: func(t *testing.T, value reflect.Value) {
				t.Helper()
				s, ok := value.Interface().([]Environment)
				require.True(t, ok)
				assert.Equal(t, ".env.a", s[0].EnvFile)
				assert.Equal(t, ".env.b", s[1].EnvFile)
			},
		},
		{
			name:     "slice with opaque struct items is skipped",
			value:    reflect.ValueOf([]resource.Quantity{*MustQuantity("100m")}),
			defaults: DefaultValues{"test[0].value": 5},
			path:     "test",
			check: func(t *testing.T, value reflect.Value) {
				t.Helper()
				s, ok := value.Interface().([]resource.Quantity)
				require.True(t, ok)
				assert.Equal(t, "100m", s[0].String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := applyDefaultsToSlice(tt.value, tt.defaults, tt.path, "v1-alpha.2")
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, tt.value)
			}
		})
	}
}

// TestSetFieldValue_Errors verifies setFieldValue error paths: an
// unsettable field, an invalid resource quantity, and a mapstructure
// decode failure. [TestSetFieldValue] above only covers the happy path
// for this function.
func TestSetFieldValue_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		field       func() reflect.Value
		value       any
		wantErr     bool
		errContains string
		check       func(t *testing.T, field reflect.Value)
	}{
		{
			name: "unsettable field returns error",
			// reflect.ValueOf on a plain value (not obtained via a
			// pointer's Elem()) is never settable.
			field:       func() reflect.Value { return reflect.ValueOf("literal") },
			value:       "x",
			wantErr:     true,
			errContains: "is not settable",
		},
		{
			name:        "invalid resource quantity pointer returns error",
			field:       func() reflect.Value { return reflect.New(reflect.TypeFor[*resource.Quantity]()).Elem() },
			value:       "not-a-quantity",
			wantErr:     true,
			errContains: "invalid resource quantity",
		},
		{
			name:  "empty string resource quantity pointer is a no-op",
			field: func() reflect.Value { return reflect.New(reflect.TypeFor[*resource.Quantity]()).Elem() },
			value: "",
			check: func(t *testing.T, field reflect.Value) { t.Helper(); assert.True(t, field.IsNil()) },
		},
		{
			name:        "invalid resource quantity value type returns error",
			field:       func() reflect.Value { return reflect.New(reflect.TypeFor[resource.Quantity]()).Elem() },
			value:       "garbage",
			wantErr:     true,
			errContains: "invalid resource quantity",
		},
		{
			name:        "mapstructure decode failure returns wrapped error",
			field:       func() reflect.Value { return reflect.New(reflect.TypeFor[[]int]()).Elem() },
			value:       []any{"not-a-number"},
			wantErr:     true,
			errContains: "failed to decode value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field := tt.field()
			err := setFieldValue(field, tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, field)
			}
		})
	}
}

// TestProcessStructField verifies each [reflect.Kind] branch of
// processStructField: nil and non-nil pointers, opaque and non-opaque
// structs, maps, slices, and unhandled scalar kinds.
func TestProcessStructField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		defaults DefaultValues
		path     string
		// setup builds the holder-specific field/type pair and returns a
		// verify closure that checks the holder's post-call state, since
		// each case needs a differently shaped holder struct.
		setup func() (field reflect.Value, fieldType reflect.StructField, verify func(t *testing.T))
	}{
		{
			name:     "nil pointer field is a no-op",
			defaults: DefaultValues{"test.autoscale.minReplicas": 2},
			path:     "test",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Autoscale *Autoscaling `json:"autoscale"`
				}
				h := &holder{}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) {
					t.Helper()
					assert.Nil(t, h.Autoscale)
				}
			},
		},
		{
			name: "non-nil pointer to struct recurses and applies defaults",
			defaults: DefaultValues{
				"test.autoscale.minReplicas": 2,
				"test.autoscale.maxReplicas": 5,
			},
			path: "test",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Autoscale *Autoscaling `json:"autoscale"`
				}
				h := &holder{Autoscale: &Autoscaling{}}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) {
					t.Helper()
					assert.Equal(t, 2, h.Autoscale.MinReplicas)
					assert.Equal(t, 5, h.Autoscale.MaxReplicas)
				}
			},
		},
		{
			name:     "pointer to opaque struct is not recursed into",
			defaults: DefaultValues{"test.q.value": 1},
			path:     "test",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Q *resource.Quantity `json:"q"`
				}
				h := &holder{Q: MustQuantity("100m")}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) {
					t.Helper()
					assert.Equal(t, "100m", h.Q.String())
				}
			},
		},
		{
			name:     "opaque struct value field is not recursed into",
			defaults: DefaultValues{},
			path:     "test",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Q resource.Quantity `json:"q"`
				}
				h := &holder{Q: *MustQuantity("200m")}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) {
					t.Helper()
					assert.Equal(t, "200m", h.Q.String())
				}
			},
		},
		{
			name:     "map field delegates to applyDefaultsToMap",
			defaults: DefaultValues{},
			path:     "",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Components map[string]*Component `json:"components"`
				}
				h := &holder{Components: map[string]*Component{"web": {Image: "nginx:latest"}}}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) { t.Helper() }
			},
		},
		{
			name:     "slice field delegates to applyDefaultsToSlice",
			defaults: DefaultValues{"envs[0].envFile": ".env.prod"},
			path:     "",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Envs []Environment `json:"envs"`
				}
				h := &holder{Envs: []Environment{{}}}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) {
					t.Helper()
					assert.Equal(t, ".env.prod", h.Envs[0].EnvFile)
				}
			},
		},
		{
			name:     "scalar field kind is a no-op",
			defaults: DefaultValues{"test.name": "should-not-apply"},
			path:     "test",
			setup: func() (reflect.Value, reflect.StructField, func(t *testing.T)) {
				type holder struct {
					Name string `json:"name"`
				}
				h := &holder{Name: "unchanged"}
				val := reflect.ValueOf(h).Elem()
				return val.Field(0), val.Type().Field(0), func(t *testing.T) {
					t.Helper()
					assert.Equal(t, "unchanged", h.Name)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field, fieldType, verify := tt.setup()
			err := processStructField(field, fieldType, tt.defaults, tt.path, "v1-alpha.2")
			require.NoError(t, err)
			verify(t)
		})
	}
}

// TestBuildFieldPath verifies dot-notation path construction, including the
// empty-parent and empty-field-name boundary cases.
func TestBuildFieldPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		currentPath string
		fieldName   string
		want        string
	}{
		{name: "empty parent returns bare field name", currentPath: "", fieldName: "project", want: "project"},
		{name: "empty field name appends trailing dot", currentPath: "components.web", fieldName: "", want: "components.web."},
		{name: "both populated joins with a dot", currentPath: "components.web", fieldName: "port", want: "components.web.port"},
		{name: "both empty returns empty string", currentPath: "", fieldName: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, buildFieldPath(tt.currentPath, tt.fieldName))
		})
	}
}
