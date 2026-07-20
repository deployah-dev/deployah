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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaTypesAtPath verifies dotted paths resolve to the types the
// manifest schema declares, including through additionalProperties (map
// keys like a component name) and $ref indirection (e.g. Autoscaling).
func TestSchemaTypesAtPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      []string
		wantTypes []string
		wantOK    bool
	}{
		{name: "project is string", path: []string{"project"}, wantTypes: []string{"string"}, wantOK: true},
		{name: "component port is integer", path: []string{"components", "web", "port"}, wantTypes: []string{"integer"}, wantOK: true},
		{name: "component image is string", path: []string{"components", "web", "image"}, wantTypes: []string{"string"}, wantOK: true},
		{name: "autoscaling minReplicas through $ref", path: []string{"components", "web", "autoscaling", "minReplicas"}, wantTypes: []string{"integer"}, wantOK: true},
		{name: "autoscaling enabled through $ref", path: []string{"components", "web", "autoscaling", "enabled"}, wantTypes: []string{"boolean"}, wantOK: true},
		{name: "env var value allows string, number, or boolean", path: []string{"environments", "production", "variables", "FOO"}, wantTypes: []string{"boolean", "number", "string"}, wantOK: true},
		{name: "unknown field", path: []string{"components", "web", "doesNotExist"}, wantOK: false},
		{name: "empty path returns root object type", path: []string{}, wantTypes: []string{"object"}, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SchemaTypesAtPath(CurrentManifestVersion, tt.path)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.ElementsMatch(t, tt.wantTypes, got)
			}
		})
	}
}

// TestSchemaTypesAtPath_UnknownVersion verifies an unresolvable schema
// version reports false rather than panicking.
func TestSchemaTypesAtPath_UnknownVersion(t *testing.T) {
	t.Parallel()
	_, ok := SchemaTypesAtPath("v99-does-not-exist", []string{"project"})
	assert.False(t, ok)
}

// TestCoerceSetValue verifies the coercion matrix: integer and boolean
// fields are upgraded from string; string fields and multi-type fields are
// left as strings.
func TestCoerceSetValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kv      string
		obj     map[string]any
		want    any
		path    []string
		wantErr bool
	}{
		{
			name: "integer field coerced",
			kv:   "components.web.port=8080",
			obj:  map[string]any{"components": map[string]any{"web": map[string]any{"port": "8080"}}},
			path: []string{"components", "web", "port"},
			want: int64(8080),
		},
		{
			name: "boolean field coerced",
			kv:   "components.web.autoscaling.enabled=true",
			obj:  map[string]any{"components": map[string]any{"web": map[string]any{"autoscaling": map[string]any{"enabled": "true"}}}},
			path: []string{"components", "web", "autoscaling", "enabled"},
			want: true,
		},
		{
			name: "string field left untouched even when value looks numeric",
			kv:   "components.web.image=1.25",
			obj:  map[string]any{"components": map[string]any{"web": map[string]any{"image": "1.25"}}},
			path: []string{"components", "web", "image"},
			want: "1.25",
		},
		{
			name:    "invalid integer value returns error",
			kv:      "components.web.port=not-a-number",
			obj:     map[string]any{"components": map[string]any{"web": map[string]any{"port": "not-a-number"}}},
			path:    []string{"components", "web", "port"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CoerceSetValue(tt.kv, tt.obj, CurrentManifestVersion)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			got := valueAtPath(tt.obj, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}
