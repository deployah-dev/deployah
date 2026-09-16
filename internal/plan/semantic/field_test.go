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

package semantic_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestDiffFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before map[string]any
		after  map[string]any
		want   []semantic.FieldChange
	}{
		{
			name:   "scalar replace",
			before: map[string]any{"spec": map[string]any{"replicas": 1}},
			after:  map[string]any{"spec": map[string]any{"replicas": 2}},
			want: []semantic.FieldChange{{
				Path: "/spec/replicas", Op: semantic.FieldReplace,
				Before: json.Number("1"), After: json.Number("2"),
			}},
		},
		{
			name:   "field add",
			before: map[string]any{"data": map[string]any{"a": "1"}},
			after:  map[string]any{"data": map[string]any{"a": "1", "b": "2"}},
			want: []semantic.FieldChange{{
				Path: "/data/b", Op: semantic.FieldAdd, After: "2",
			}},
		},
		{
			name:   "added object with go int",
			before: map[string]any{},
			after:  map[string]any{"spec": map[string]any{"replicas": 1}},
			want: []semantic.FieldChange{{
				Path: "/spec", Op: semantic.FieldAdd, After: map[string]any{"replicas": json.Number("1")},
			}},
		},
		{
			name:   "field remove",
			before: map[string]any{"data": map[string]any{"a": "1", "b": "2"}},
			after:  map[string]any{"data": map[string]any{"a": "1"}},
			want: []semantic.FieldChange{{
				Path: "/data/b", Op: semantic.FieldRemove, Before: "2",
			}},
		},
		{
			name: "nested object",
			before: map[string]any{
				"spec": map[string]any{"template": map[string]any{"image": "v1"}},
			},
			after: map[string]any{
				"spec": map[string]any{"template": map[string]any{"image": "v2"}},
			},
			want: []semantic.FieldChange{{
				Path: "/spec/template/image", Op: semantic.FieldReplace, Before: "v1", After: "v2",
			}},
		},
		{
			name: "arrays",
			before: map[string]any{
				"spec": map[string]any{"args": []any{"a", "b"}},
			},
			after: map[string]any{
				"spec": map[string]any{"args": []any{"a", "c", "d"}},
			},
			want: []semantic.FieldChange{
				{Path: "/spec/args/1", Op: semantic.FieldReplace, Before: "b", After: "c"},
				{Path: "/spec/args/2", Op: semantic.FieldAdd, After: "d"},
			},
		},
		{
			name:   "null replace",
			before: map[string]any{"spec": map[string]any{"paused": nil}},
			after:  map[string]any{"spec": map[string]any{"paused": true}},
			want: []semantic.FieldChange{{
				Path: "/spec/paused", Op: semantic.FieldReplace, Before: nil, After: true,
			}},
		},
		{
			name:   "null vs absent",
			before: map[string]any{"spec": map[string]any{}},
			after:  map[string]any{"spec": map[string]any{"paused": nil}},
			want: []semantic.FieldChange{{
				Path: "/spec/paused", Op: semantic.FieldAdd, After: nil,
			}},
		},
		{
			name:   "type change",
			before: map[string]any{"spec": map[string]any{"replicas": 1}},
			after:  map[string]any{"spec": map[string]any{"replicas": "one"}},
			want: []semantic.FieldChange{{
				Path: "/spec/replicas", Op: semantic.FieldReplace, Before: json.Number("1"), After: "one",
			}},
		},
		{
			name:   "int and float64 are equivalent",
			before: map[string]any{"spec": map[string]any{"replicas": int64(2)}},
			after:  map[string]any{"spec": map[string]any{"replicas": float64(2)}},
			want:   nil,
		},
		{
			name:   "json.Number forms are equivalent",
			before: map[string]any{"n": 2},
			after:  map[string]any{"n": json.Number("2.00")},
			want:   nil,
		},
		{
			name:   "scientific and integer forms are equivalent",
			before: map[string]any{"n": json.Number("2e0")},
			after:  map[string]any{"n": json.Number("2.0")},
			want:   nil,
		},
		{
			name:   "large integers remain distinct",
			before: map[string]any{"n": json.Number("9007199254740992")},
			after:  map[string]any{"n": json.Number("9007199254740993")},
			want: []semantic.FieldChange{{
				Path: "/n", Op: semantic.FieldReplace,
				Before: json.Number("9007199254740992"),
				After:  json.Number("9007199254740993"),
			}},
		},
		{
			name: "bookkeeping omitted",
			before: map[string]any{
				"metadata": map[string]any{"name": "app", "resourceVersion": "1", "uid": "u1"},
				"status":   map[string]any{"ready": false},
				"data":     map[string]any{"key": "v1"},
			},
			after: map[string]any{
				"metadata": map[string]any{"name": "app", "resourceVersion": "2", "uid": "u2"},
				"status":   map[string]any{"ready": true},
				"data":     map[string]any{"key": "v2"},
			},
			want: []semantic.FieldChange{{
				Path: "/data/key", Op: semantic.FieldReplace, Before: "v1", After: "v2",
			}},
		},
		{
			name: "deterministic field order",
			before: map[string]any{
				"z": "1",
				"a": "1",
			},
			after: map[string]any{
				"z": "2",
				"a": "2",
			},
			want: []semantic.FieldChange{
				{Path: "/a", Op: semantic.FieldReplace, Before: "1", After: "2"},
				{Path: "/z", Op: semantic.FieldReplace, Before: "1", After: "2"},
			},
		},
		{
			name: "escaped pointer tokens",
			before: map[string]any{
				"foo/bar": "1",
				"tilde~x": "1",
			},
			after: map[string]any{
				"foo/bar": "2",
				"tilde~x": "2",
			},
			want: []semantic.FieldChange{
				{Path: "/foo~1bar", Op: semantic.FieldReplace, Before: "1", After: "2"},
				{Path: "/tilde~0x", Op: semantic.FieldReplace, Before: "1", After: "2"},
			},
		},
		{
			name: "annotation slash escape",
			before: map[string]any{
				"metadata": map[string]any{"annotations": map[string]any{"foo/bar": "1"}},
			},
			after: map[string]any{
				"metadata": map[string]any{"annotations": map[string]any{"foo/bar": "2"}},
			},
			want: []semantic.FieldChange{{
				Path: "/metadata/annotations/foo~1bar", Op: semantic.FieldReplace, Before: "1", After: "2",
			}},
		},
		{
			name:   "nil before is empty object",
			before: nil,
			after:  map[string]any{"a": json.Number("1")},
			want: []semantic.FieldChange{{
				Path: "/a", Op: semantic.FieldAdd, After: json.Number("1"),
			}},
		},
		{
			name:   "nil after is empty object",
			before: map[string]any{"a": json.Number("1")},
			after:  nil,
			want: []semantic.FieldChange{{
				Path: "/a", Op: semantic.FieldRemove, Before: json.Number("1"),
			}},
		},
		{
			name:   "array append one item",
			before: map[string]any{"items": []any{"a"}},
			after:  map[string]any{"items": []any{"a", "b"}},
			want: []semantic.FieldChange{{
				Path: "/items/1", Op: semantic.FieldAdd, After: "b",
			}},
		},
		{
			name:   "array append multiple items",
			before: map[string]any{"items": []any{"a"}},
			after:  map[string]any{"items": []any{"a", "b", "c"}},
			want: []semantic.FieldChange{
				{Path: "/items/1", Op: semantic.FieldAdd, After: "b"},
				{Path: "/items/2", Op: semantic.FieldAdd, After: "c"},
			},
		},
		{
			name:   "string slice append",
			before: map[string]any{"items": []string{"a"}},
			after:  map[string]any{"items": []string{"a", "b"}},
			want: []semantic.FieldChange{{
				Path: "/items/1", Op: semantic.FieldAdd, After: "b",
			}},
		},
		{
			name: "nested array index is stable",
			before: map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"args": []any{"a", "b"}},
					},
				},
			},
			after: map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"args": []any{"a", "c"}},
					},
				},
			},
			want: []semantic.FieldChange{{
				Path: "/spec/containers/0/args/1", Op: semantic.FieldReplace, Before: "b", After: "c",
			}},
		},
		{
			name: "container image is a leaf replace",
			before: map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "app", "image": "nginx:1"},
							},
						},
					},
				},
			},
			after: map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "app", "image": "nginx:2"},
							},
						},
					},
				},
			},
			want: []semantic.FieldChange{{
				Path: "/spec/template/spec/containers/0/image", Op: semantic.FieldReplace,
				Before: "nginx:1", After: "nginx:2",
			}},
		},
		{
			name: "escaped secret key",
			before: map[string]any{
				"data": map[string]any{"foo/bar": "old", "tilde~x": "old"},
			},
			after: map[string]any{
				"data": map[string]any{"foo/bar": "new", "tilde~x": "new"},
			},
			want: []semantic.FieldChange{
				{Path: "/data/foo~1bar", Op: semantic.FieldReplace, Before: "old", After: "new"},
				{Path: "/data/tilde~0x", Op: semantic.FieldReplace, Before: "old", After: "new"},
			},
		},
		{
			name:   "2 equals 2.0",
			before: map[string]any{"n": 2},
			after:  map[string]any{"n": 2.0},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := semantic.DiffFields(tt.before, tt.after)
			require.NoError(t, err)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsBookkeepingPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{
		"/status",
		"/metadata/resourceVersion",
		"/metadata/uid",
		"/metadata/generation",
		"/metadata/creationTimestamp",
		"/metadata/managedFields",
	}, semantic.BookkeepingPointers())

	tests := []struct {
		path string
		want bool
	}{
		{path: "/status", want: true},
		{path: "/status/ready", want: true},
		{path: "/metadata/resourceVersion", want: true},
		{path: "/metadata/uid", want: true},
		{path: "/metadata/generation", want: true},
		{path: "/metadata/creationTimestamp", want: true},
		{path: "/metadata/managedFields", want: true},
		{path: "/metadata/managedFields/0", want: true},
		{path: "/metadata/annotations", want: false},
		{path: "/metadata/labels", want: false},
		{path: "/spec/replicas", want: false},
		{path: "/statusFoo", want: false},
		{path: "/metadata/uidSuffix", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, semantic.IsBookkeepingPath(tt.path))
		})
	}
}

func TestBookkeepingPointers_IsCopy(t *testing.T) {
	t.Parallel()
	got := semantic.BookkeepingPointers()
	got[0] = "/mutated"
	assert.True(t, semantic.IsBookkeepingPath("/status"))
	assert.Equal(t, "/status", semantic.BookkeepingPointers()[0])
}

func TestDiffFields_NoMutation(t *testing.T) {
	t.Parallel()
	before := map[string]any{"data": map[string]any{"key": "old"}}
	after := map[string]any{"data": map[string]any{"key": "new"}}
	got, err := semantic.DiffFields(before, after)
	require.NoError(t, err)
	require.Len(t, got, 1)
	got[0].Before = "mutated"
	got[0].After = "mutated"
	assert.Equal(t, "old", objectString(t, before, "data", "key"))
	assert.Equal(t, "new", objectString(t, after, "data", "key"))
	got2, err := semantic.DiffFields(before, after)
	require.NoError(t, err)
	assert.Equal(t, "old", got2[0].Before)
	assert.Equal(t, "new", got2[0].After)
}

func TestDiffFields_EncodeErrors(t *testing.T) {
	t.Parallel()
	t.Run("before", func(t *testing.T) {
		t.Parallel()
		_, err := semantic.DiffFields(map[string]any{"n": math.NaN()}, map[string]any{"n": 1})
		require.Error(t, err)
		assert.ErrorContains(t, err, "encode before snapshot")
	})
	t.Run("after", func(t *testing.T) {
		t.Parallel()
		_, err := semantic.DiffFields(map[string]any{"n": 1}, map[string]any{"n": math.Inf(1)})
		require.Error(t, err)
		assert.ErrorContains(t, err, "encode after snapshot")
	})
}

func TestDiffFields_NestedCopyIsolation(t *testing.T) {
	t.Parallel()
	labels := map[string]string{"app": "web"}
	args := []string{"a", "b"}
	nested := []any{map[string]any{"k": "v"}}
	inner := map[string]any{"x": "1"}
	before := map[string]any{
		"labels": labels,
		"args":   args,
		"nested": nested,
		"inner":  inner,
	}
	after := map[string]any{
		"labels": map[string]string{"app": "api"},
		"args":   []string{"a", "c"},
		"nested": []any{map[string]any{"k": "w"}},
		"inner":  map[string]any{"x": "2"},
	}
	got, err := semantic.DiffFields(before, after)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	for i := range got {
		got[i].Before = "mutated"
		got[i].After = "mutated"
	}

	assert.Equal(t, "web", labels["app"])
	assert.Equal(t, []string{"a", "b"}, args)
	nestedMap, ok := nested[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "v", nestedMap["k"])
	assert.Equal(t, "1", inner["x"])
}
