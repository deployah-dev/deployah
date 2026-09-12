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

package view

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func TestMarshalOrderedYAML_KeyOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      map[string]any
		ordered []string
		omits   []string
	}{
		{
			name: "preferred root keys",
			in: map[string]any{
				"spec":       map[string]any{"replicas": 1},
				"kind":       "Deployment",
				"apiVersion": "apps/v1",
				"metadata":   map[string]any{"name": "web"},
				"status":     map[string]any{"ready": true},
			},
			ordered: []string{"apiVersion:", "kind:", "metadata:", "spec:", "status:"},
		},
		{
			name: "absent preferred keys",
			in: map[string]any{
				"zeta":  "1",
				"alpha": "2",
				"kind":  "ConfigMap",
			},
			ordered: []string{"kind:", "alpha:", "zeta:"},
			omits:   []string{"apiVersion:", "metadata:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := marshalOrderedYAML(tt.in)
			require.NoError(t, err)
			assertYAMLOrder(t, got, tt.ordered)
			for _, omit := range tt.omits {
				assert.NotContains(t, got, omit)
			}
		})
	}
}

func TestMarshalOrderedYAML_NilRoot(t *testing.T) {
	t.Parallel()
	got, err := marshalOrderedYAML(nil)
	require.NoError(t, err)
	assert.Equal(t, "{}\n", got)
}

func TestMarshalOrderedYAML_MetadataOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      map[string]any
		ordered []string
		omits   []string
	}{
		{
			name: "name wins",
			in: map[string]any{
				"metadata": map[string]any{
					"generateName": "app-",
					"namespace":    "prod",
					"name":         "web",
					"labels":       map[string]any{"a": "1"},
				},
			},
			ordered: []string{"name: web", "namespace: prod", "generateName:", "labels:"},
		},
		{
			name: "generateName without name",
			in: map[string]any{
				"metadata": map[string]any{
					"namespace":    "prod",
					"generateName": "app-",
				},
			},
			ordered: []string{"generateName:", "namespace:"},
		},
		{
			name: "name without namespace",
			in: map[string]any{
				"metadata": map[string]any{"name": "web", "labels": map[string]any{"a": "1"}},
			},
			ordered: []string{"name:", "labels:"},
			omits:   []string{"namespace:"},
		},
		{
			name: "string map metadata",
			in: map[string]any{
				"metadata": map[string]string{"namespace": "prod", "name": "web"},
			},
			ordered: []string{"name:", "namespace:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := marshalOrderedYAML(tt.in)
			require.NoError(t, err)
			assertYAMLOrder(t, got, tt.ordered)
			for _, omit := range tt.omits {
				assert.NotContains(t, got, omit)
			}
		})
	}
}

func TestMarshalOrderedYAML_NestedMapsAndArrays(t *testing.T) {
	t.Parallel()
	got, err := marshalOrderedYAML(map[string]any{
		"data": map[string]any{
			"z": "1",
			"a": "2",
			"items": []any{
				map[string]any{"z": "1", "a": "2"},
				"keep-order",
			},
			"tags": []string{"zeta", "alpha"},
		},
	})
	require.NoError(t, err)
	assert.Regexp(t, `(?s)a: ["']?2["']?.*items:.*tags:.*z: ["']?1["']?`, got)
	assert.Less(t, strings.Index(got, "zeta"), strings.Index(got, "alpha"))
}

func TestMarshalOrderedYAML_Scalars(t *testing.T) {
	t.Parallel()
	got, err := marshalOrderedYAML(map[string]any{
		"absent":  nil,
		"ok":      true,
		"name":    "web",
		"count":   3,
		"count32": int32(4),
		"count64": int64(5),
		"ratio32": float32(1.5),
		"ratio64": 2.5,
		"numInt":  json.Number("7"),
		"numDec":  json.Number("1.25"),
		"numExp":  json.Number("2e3"),
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(got), &decoded))
	assert.Nil(t, decoded["absent"])
	assert.Equal(t, true, decoded["ok"])
	assert.Equal(t, "web", decoded["name"])
	assert.Equal(t, 3, decoded["count"])
	assert.Equal(t, 4, decoded["count32"])
	assert.Equal(t, 5, decoded["count64"])
	assert.InDelta(t, 1.5, asFloat(t, decoded["ratio32"]), 0.001)
	assert.InDelta(t, 2.5, asFloat(t, decoded["ratio64"]), 0.001)
	assert.Equal(t, 7, decoded["numInt"])
	assert.InDelta(t, 1.25, asFloat(t, decoded["numDec"]), 0.001)
	assert.InDelta(t, 2000, asFloat(t, decoded["numExp"]), 0.001)
}

func TestMarshalOrderedYAML_StringMapNonMetadata(t *testing.T) {
	t.Parallel()
	got, err := marshalOrderedYAML(map[string]any{
		"labels": map[string]string{"z": "1", "a": "1"},
	})
	require.NoError(t, err)
	assert.Less(t, strings.Index(got, "a:"), strings.Index(got, "z:"))
}

func TestMarshalOrderedYAML_LooksLikeScalarStayStrings(t *testing.T) {
	t.Parallel()
	got, err := marshalOrderedYAML(map[string]any{
		"t":    "true",
		"f":    "false",
		"n":    "123",
		"d":    "1.2",
		"none": "null",
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(got), &decoded))
	assert.Equal(t, "true", decoded["t"])
	assert.Equal(t, "false", decoded["f"])
	assert.Equal(t, "123", decoded["n"])
	assert.Equal(t, "1.2", decoded["d"])
	assert.Equal(t, "null", decoded["none"])
}

func assertYAMLOrder(t *testing.T, got string, ordered []string) {
	t.Helper()
	prev := -1
	for _, frag := range ordered {
		idx := strings.Index(got, frag)
		require.Greater(t, idx, prev, frag)
		prev = idx
	}
}

func asFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	default:
		t.Fatalf("asFloat: unexpected %T", v)
		return 0
	}
}
