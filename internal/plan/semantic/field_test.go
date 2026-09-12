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
				Path: "/spec/replicas", Op: semantic.FieldReplace, Before: 1, After: 2,
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
				Path: "/spec", Op: semantic.FieldAdd, After: map[string]any{"replicas": 1},
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
				Path: "/spec/replicas", Op: semantic.FieldReplace, Before: 1, After: "one",
			}},
		},
		{
			name:   "number type equivalence",
			before: map[string]any{"spec": map[string]any{"replicas": int64(2)}},
			after:  map[string]any{"spec": map[string]any{"replicas": float64(2)}},
			want:   nil,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := semantic.DiffFields(tt.before, tt.after)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDiffFields_NoMutation(t *testing.T) {
	t.Parallel()
	before := map[string]any{"data": map[string]any{"key": "old"}}
	after := map[string]any{"data": map[string]any{"key": "new"}}
	got := semantic.DiffFields(before, after)
	require.Len(t, got, 1)
	got[0].Before = "mutated"
	got[0].After = "mutated"
	assert.Equal(t, "old", objectString(t, before, "data", "key"))
	assert.Equal(t, "new", objectString(t, after, "data", "key"))
	got2 := semantic.DiffFields(before, after)
	assert.Equal(t, "old", got2[0].Before)
	assert.Equal(t, "new", got2[0].After)
}
