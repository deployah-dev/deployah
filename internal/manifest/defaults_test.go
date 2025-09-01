package manifest

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/deployah-dev/deployah/internal/manifest/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"golang.org/x/exp/maps"
)

// DefaultsTestSuite is a test suite for the defaults package
type DefaultsTestSuite struct {
	suite.Suite
}

// TestExtractDefaultsFromSchemaData tests the extractDefaultsFromSchemaData function
func (s *DefaultsTestSuite) TestExtractDefaultsFromSchemaData() {
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
		s.T().Run(tt.name, func(t *testing.T) {
			defaults := make(DefaultValues)
			extractDefaultsFromSchemaData(tt.schema, tt.path, defaults)
			assert.Equal(t, tt.expected, defaults)
		})
	}
}

// TestGetDefaultValues tests the GetDefaultValues function
func (s *DefaultsTestSuite) TestGetDefaultValues() {
	tests := []struct {
		name       string
		version    string
		schemaType schema.SchemaType
		expectErr  bool
	}{
		{
			name:       "valid manifest schema",
			version:    "v1-alpha.1",
			schemaType: schema.SchemaTypeManifest,
			expectErr:  false,
		},
		{
			name:       "valid environments schema",
			version:    "v1-alpha.1",
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
			version:    "v1-alpha.1",
			schemaType: "unsupported",
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			defaults, err := GetDefaultValues(tt.version, tt.schemaType)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, defaults)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, defaults)
				assert.NotEmpty(t, defaults)
			}
		})
	}
}

// TestFillManifestWithDefaults tests the FillManifestWithDefaults function
func (s *DefaultsTestSuite) TestFillManifestWithDefaults() {
	tests := []struct {
		name      string
		manifest  *Manifest
		version   string
		expectErr bool
	}{
		{
			name: "valid manifest with components",
			manifest: &Manifest{
				ApiVersion: "v1-alpha.1",
				Project:    "test-project",
				Components: map[string]Component{
					"web": {
						Image: "nginx:latest",
					},
				},
			},
			version:   "v1-alpha.1",
			expectErr: false,
		},
		{
			name: "manifest with nil components",
			manifest: &Manifest{
				ApiVersion: "v1-alpha.1",
				Project:    "test-project",
				Components: nil,
			},
			version:   "v1-alpha.1",
			expectErr: false,
		},
		{
			name: "manifest with environments",
			manifest: &Manifest{
				ApiVersion: "v1-alpha.1",
				Project:    "test-project",
				Components: map[string]Component{},
				Environments: []Environment{
					{Name: "production"},
				},
			},
			version:   "v1-alpha.1",
			expectErr: false,
		},
		{
			name: "invalid version",
			manifest: &Manifest{
				ApiVersion: "v1-alpha.1",
				Project:    "test-project",
				Components: map[string]Component{},
			},
			version:   "invalid-version",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			err := FillManifestWithDefaults(tt.manifest, tt.version)
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
func (s *DefaultsTestSuite) TestApplyDefaultsRecursively() {
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
		s.T().Run(tt.name, func(t *testing.T) {
			applyDefaultsRecursively(tt.obj, tt.defaults, tt.path, "v1-alpha.1")
			assert.Equal(t, tt.expected, tt.obj)
		})
	}
}

// TestApplyDefaultsToMap tests the applyDefaultsToMap function
func (s *DefaultsTestSuite) TestApplyDefaultsToMap() {
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
		s.T().Run(tt.name, func(t *testing.T) {
			// This test mainly ensures the function doesn't panic
			applyDefaultsToMap(tt.mapVal, tt.defaults, tt.path, "v1-alpha.1")
			// No specific assertions as this is mainly testing for panics
		})
	}
}

// TestApplyDefaultsToSlice tests the applyDefaultsToSlice function
func (s *DefaultsTestSuite) TestApplyDefaultsToSlice() {
	tests := []struct {
		name     string
		sliceVal reflect.Value
		defaults DefaultValues
		path     string
	}{
		{
			name: "apply defaults to slice with struct items",
			sliceVal: reflect.ValueOf([]*Environment{
				{Name: "prod"},
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
		s.T().Run(tt.name, func(t *testing.T) {
			// This test mainly ensures the function doesn't panic
			applyDefaultsToSlice(tt.sliceVal, tt.defaults, tt.path, "v1-alpha.1")
			// No specific assertions as this is mainly testing for panics
		})
	}
}

// Fix for TestSetFieldValue: use reflect.New to get a settable value for primitive types
func (s *DefaultsTestSuite) TestSetFieldValue() {
	tests := []struct {
		name     string
		field    reflect.Value
		value    any
		expected any
	}{
		{
			name:     "set string field",
			field:    reflect.New(reflect.TypeOf("")).Elem(),
			value:    "test-string",
			expected: "test-string",
		},
		{
			name:     "set int field",
			field:    reflect.New(reflect.TypeOf(0)).Elem(),
			value:    42,
			expected: 42,
		},
		{
			name:     "set bool field",
			field:    reflect.New(reflect.TypeOf(false)).Elem(),
			value:    true,
			expected: true,
		},
		{
			name:     "set float64 field from float64",
			field:    reflect.New(reflect.TypeOf(0.0)).Elem(),
			value:    3.14,
			expected: 3.14,
		},
		{
			name:     "set int field from float64",
			field:    reflect.New(reflect.TypeOf(0)).Elem(),
			value:    42.0,
			expected: 42,
		},
		{
			name:     "set uint field from float64",
			field:    reflect.New(reflect.TypeOf(uint(0))).Elem(),
			value:    42.0,
			expected: uint(42),
		},
		{
			name:     "set slice field from []any",
			field:    reflect.New(reflect.TypeOf([]string{})).Elem(),
			value:    []any{"item1", "item2"},
			expected: []string{"item1", "item2"}, // mapstructure correctly converts []any to []string
		},
	}
	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			setFieldValue(tt.field, tt.value)
			assert.Equal(t, tt.expected, tt.field.Interface())
		})
	}
}

// TestIsZeroValue tests the isZeroValue function
func (s *DefaultsTestSuite) TestIsZeroValue() {
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
		s.T().Run(tt.name, func(t *testing.T) {
			result := isZeroValue(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCreateManifestWithDefaults tests the CreateManifestWithDefaults function
func (s *DefaultsTestSuite) TestCreateManifestWithDefaults() {
	tests := []struct {
		name        string
		projectName string
		version     string
		expectErr   bool
	}{
		{
			name:        "valid manifest creation",
			projectName: "test-project",
			version:     "v1-alpha.1",
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
		s.T().Run(tt.name, func(t *testing.T) {
			manifest, err := CreateManifestWithDefaults(tt.projectName, tt.version)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, manifest)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manifest)
				assert.Equal(t, tt.projectName, manifest.Project)
				assert.Equal(t, tt.version, manifest.ApiVersion)
				assert.NotNil(t, manifest.Components)
			}
		})
	}
}

// TestGetJSONFieldName tests the getJSONFieldName function
func (s *DefaultsTestSuite) TestGetJSONFieldName() {
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
		s.T().Run(tt.name, func(t *testing.T) {
			result := getJSONFieldName(tt.field)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsComponentPath tests the isComponentPath function
func (s *DefaultsTestSuite) TestIsComponentPath() {
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
		s.T().Run(tt.name, func(t *testing.T) {
			result := isComponentPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIntegration tests integration scenarios with real schema defaults
func (s *DefaultsTestSuite) TestIntegration() {
	s.T().Run("create manifest with defaults and verify component defaults", func(t *testing.T) {
		manifest, err := CreateManifestWithDefaults("test-project", "v1-alpha.1")
		assert.NoError(t, err)
		assert.NotNil(t, manifest)

		// Add a component and verify defaults are applied
		manifest.Components["web"] = Component{
			Image: "nginx:latest",
		}

		err = FillManifestWithDefaults(manifest, "v1-alpha.1")
		assert.NoError(t, err)

		webComponent := manifest.Components["web"]
		// Verify that defaults from schema are applied
		assert.Equal(t, ComponentRoleService, webComponent.Role)
		assert.Equal(t, ComponentKindStateless, webComponent.Kind)
		assert.Equal(t, 8080, webComponent.Port)
	})

	s.T().Run("verify autoscaling defaults", func(t *testing.T) {
		manifest := &Manifest{
			ApiVersion: "v1-alpha.1",
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

		err := FillManifestWithDefaults(manifest, "v1-alpha.1")
		assert.NoError(t, err)

		apiComponent := manifest.Components["api"]
		assert.NotNil(t, apiComponent.Autoscaling)
		assert.Equal(t, 2, apiComponent.Autoscaling.MinReplicas)
		assert.Equal(t, 5, apiComponent.Autoscaling.MaxReplicas)
		assert.Len(t, apiComponent.Autoscaling.Metrics, 1)
		assert.Equal(t, MetricTypeCPU, apiComponent.Autoscaling.Metrics[0].Type)
		assert.Equal(t, 75, apiComponent.Autoscaling.Metrics[0].Target)
	})

	s.T().Run("verify environment defaults", func(t *testing.T) {
		manifest := &Manifest{
			ApiVersion: "v1-alpha.1",
			Project:    "test-project",
			Components: map[string]Component{},
			Environments: []Environment{
				{Name: "production"},
			},
		}

		err := FillManifestWithDefaults(manifest, "v1-alpha.1")
		assert.NoError(t, err)

		assert.Equal(t, ".env.production", manifest.Environments[0].EnvFile)
		assert.Equal(t, "config.production.yaml", manifest.Environments[0].ConfigFile)
		assert.NotNil(t, manifest.Environments[0].Variables)
	})
}

// Fix for TestDefaultValuesJSON: compare after normalizing int/float types
func (s *DefaultsTestSuite) TestDefaultValuesJSON() {
	defaults := DefaultValues{
		"components.web.role": "service",
		"components.web.port": 8080,
		"components.web.kind": "stateless",
	}

	// Test marshaling
	jsonData, err := json.Marshal(defaults)
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), jsonData)

	// Test unmarshaling
	var unmarshaled DefaultValues
	err = json.Unmarshal(jsonData, &unmarshaled)
	assert.NoError(s.T(), err)

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
	assert.Equal(s.T(), normalize(defaults), normalize(unmarshaled))
}

// TestDefaultValuesCopy tests copying of DefaultValues
func (s *DefaultsTestSuite) TestDefaultValuesCopy() {
	original := DefaultValues{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	// Test using maps.Copy
	copied := make(DefaultValues)
	maps.Copy(copied, original)
	assert.Equal(s.T(), original, copied)

	// Verify they are independent
	copied["key4"] = "new-value"
	assert.NotEqual(s.T(), original, copied)
}

// TestDefaultsTestSuite runs the defaults test suite
func TestDefaultsTestSuite(t *testing.T) {
	suite.Run(t, new(DefaultsTestSuite))
}
