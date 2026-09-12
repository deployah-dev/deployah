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

package semantic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wI2L/jsondiff"
)

func TestMapPatchOp_Unsupported(t *testing.T) {
	t.Parallel()
	_, keep, err := mapPatchOp(jsondiff.Operation{
		Type: jsondiff.OperationMove,
		Path: "/spec/replicas",
	}, map[string]any{}, map[string]int{})
	require.Error(t, err)
	assert.False(t, keep)
	assert.ErrorContains(t, err, "unsupported json patch operation")
	assert.ErrorContains(t, err, "/spec/replicas")
}

func TestLookupPointer(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"spec": map[string]any{
			"args": []any{"a", "b"},
			"name": "web",
		},
	}
	tests := []struct {
		name    string
		pointer string
		want    any
		ok      bool
	}{
		{name: "empty pointer", pointer: "", want: obj, ok: true},
		{name: "missing slash", pointer: "spec", ok: false},
		{name: "map key", pointer: "/spec/name", want: "web", ok: true},
		{name: "array index", pointer: "/spec/args/1", want: "b", ok: true},
		{name: "non-numeric index", pointer: "/spec/args/x", ok: false},
		{name: "negative index", pointer: "/spec/args/-1", ok: false},
		{name: "out of range", pointer: "/spec/args/9", ok: false},
		{name: "missing key", pointer: "/spec/missing", ok: false},
		{name: "into scalar", pointer: "/spec/name/x", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := lookupPointer(obj, tt.pointer)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestArrayLenAt(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"any":    []any{"a", "b"},
		"string": []string{"a"},
		"scalar": "x",
	}
	tests := []struct {
		name    string
		pointer string
		want    int
	}{
		{name: "any slice", pointer: "/any", want: 2},
		{name: "string slice", pointer: "/string", want: 1},
		{name: "missing", pointer: "/missing"},
		{name: "scalar", pointer: "/scalar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, arrayLenAt(obj, tt.pointer))
		})
	}
}
