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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestPointerTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pointer string
		want    []string
		ok      bool
	}{
		{name: "empty", pointer: ""},
		{name: "missing slash", pointer: "spec"},
		{name: "normal", pointer: "/spec/replicas", want: []string{"spec", "replicas"}, ok: true},
		{name: "unescape slash", pointer: "/foo~1bar", want: []string{"foo/bar"}, ok: true},
		{name: "unescape tilde", pointer: "/tilde~0x", want: []string{"tilde~x"}, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := pointerTokens(tt.pointer)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLookupPointer_Branches(t *testing.T) {
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
		{name: "empty", pointer: "", want: obj, ok: true},
		{name: "missing slash", pointer: "spec", ok: false},
		{name: "array index", pointer: "/spec/args/0", want: "a", ok: true},
		{name: "non-numeric", pointer: "/spec/args/x", ok: false},
		{name: "negative", pointer: "/spec/args/-1", ok: false},
		{name: "out of range", pointer: "/spec/args/8", ok: false},
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

func TestSetAndDeletePointer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		obj     map[string]any
		setPath string
		setVal  any
		delPath string
		want    map[string]any
	}{
		{
			name:    "top-level",
			obj:     map[string]any{"a": "1", "b": "2"},
			setPath: "/a",
			setVal:  "x",
			delPath: "/b",
			want:    map[string]any{"a": "x"},
		},
		{
			name:    "nested",
			obj:     map[string]any{"meta": map[string]any{"name": "old", "uid": "u"}},
			setPath: "/meta/name",
			setVal:  "new",
			delPath: "/meta/uid",
			want:    map[string]any{"meta": map[string]any{"name": "new"}},
		},
		{
			name:    "missing parent",
			obj:     map[string]any{"a": "1"},
			setPath: "/missing/key",
			setVal:  "x",
			delPath: "/missing/key",
			want:    map[string]any{"a": "1"},
		},
		{
			name:    "invalid pointer",
			obj:     map[string]any{"a": "1"},
			setPath: "a",
			setVal:  "x",
			delPath: "a",
			want:    map[string]any{"a": "1"},
		},
		{
			name:    "empty pointer",
			obj:     map[string]any{"a": "1"},
			setPath: "",
			setVal:  "x",
			delPath: "",
			want:    map[string]any{"a": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			setAtPointer(tt.obj, tt.setPath, tt.setVal)
			deleteAtPointer(tt.obj, tt.delPath)
			assert.Equal(t, tt.want, tt.obj)
		})
	}
}

func TestHumanObject_StripsBookkeeping(t *testing.T) {
	t.Parallel()
	obj := humanObject(&semantic.ResourceSnapshot{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":              "web",
			"namespace":         "prod",
			"uid":               "u1",
			"resourceVersion":   "11",
			"generation":        3,
			"creationTimestamp": "2020-01-01T00:00:00Z",
			"managedFields":     []any{map[string]any{"manager": "helm"}},
			"labels":            map[string]any{"app": "web"},
			"annotations": map[string]any{
				"example.com/keep": "yes",
				"cert-manager.io/issue-temporary-certificate": "true",
			},
		},
		"data":   map[string]any{"key": "v1"},
		"status": map[string]any{"ready": true},
	}})
	meta := mustMap(t, obj["metadata"])
	assert.Equal(t, "web", meta["name"])
	assert.Equal(t, map[string]any{"app": "web"}, meta["labels"])
	annotations := mustMap(t, meta["annotations"])
	assert.Equal(t, "yes", annotations["example.com/keep"])
	assert.Equal(t, "true", annotations["cert-manager.io/issue-temporary-certificate"])
	assert.Equal(t, "v1", mustMap(t, obj["data"])["key"])
	_, hasStatus := obj["status"]
	assert.False(t, hasStatus)
	for _, key := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields"} {
		_, found := meta[key]
		assert.False(t, found, key)
	}
}

func TestHumanObject_NilSnapshot(t *testing.T) {
	t.Parallel()
	got := humanObject(nil)
	require.NotNil(t, got)
	assert.Empty(t, got)
}

func TestMarkLeavesAndSlices(t *testing.T) {
	t.Parallel()
	nested := map[string]any{
		"keep": nil,
		"leaf": "secret",
		"child": map[string]any{
			"inner": "hidden",
		},
		"list": []any{
			"item",
			nil,
			map[string]any{"k": "v"},
			[]any{"deep"},
		},
	}
	markLeaves(nested, "SENTINEL")
	assert.Nil(t, nested["keep"])
	assert.Equal(t, "SENTINEL", nested["leaf"])
	assert.Equal(t, "SENTINEL", mustMap(t, nested["child"])["inner"])
	list, ok := nested["list"].([]any)
	require.True(t, ok)
	assert.Equal(t, "SENTINEL", list[0])
	assert.Nil(t, list[1])
	assert.Equal(t, "SENTINEL", mustMap(t, list[2])["k"])
	deep, ok := list[3].([]any)
	require.True(t, ok)
	assert.Equal(t, "SENTINEL", deep[0])
}

func TestSetSecretMarker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		obj     map[string]any
		pointer string
		want    map[string]any
	}{
		{
			name:    "string map",
			obj:     map[string]any{"data": map[string]string{"token": "secret"}},
			pointer: "/data",
			want:    map[string]any{"data": map[string]string{"token": "SENTINEL"}},
		},
		{
			name:    "missing pointer",
			obj:     map[string]any{"data": []any{"secret", nil}},
			pointer: "/missing",
			want:    map[string]any{"data": []any{"secret", nil}},
		},
		{
			name:    "slice",
			obj:     map[string]any{"data": []any{"secret", nil}},
			pointer: "/data",
			want:    map[string]any{"data": []any{"SENTINEL", nil}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			setSecretMarker(tt.obj, tt.pointer, "SENTINEL")
			assert.Equal(t, tt.want, tt.obj)
		})
	}
}

func TestParentMap(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"a": "scalar", "b": map[string]any{"c": "1"}}
	tests := []struct {
		name   string
		tokens []string
		ok     bool
		want   map[string]any
	}{
		{name: "root", tokens: nil, ok: true, want: obj},
		{name: "into scalar", tokens: []string{"a"}},
		{name: "child map", tokens: []string{"b"}, ok: true, want: map[string]any{"c": "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parentMap(obj, tt.tokens)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok)
	return m
}
