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

package view_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

var update = flag.Bool("update", false, "update golden files")

func helmOrigin() semantic.ResourceOrigin {
	return semantic.ResourceOrigin{
		Kind: semantic.OriginHelm,
		Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"},
	}
}

func writeApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Write: &semantic.WriteSemantics{
			Method:       semantic.WriteServerSide,
			FieldManager: "deployah",
		},
	}
}

func deleteApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Delete: &semantic.DeleteSemantics{Propagation: semantic.PropagationBackground},
	}
}

func bothApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Write:  writeApply().Write,
		Delete: deleteApply().Delete,
	}
}

func snap(obj map[string]any) *semantic.ResourceSnapshot {
	return &semantic.ResourceSnapshot{Object: obj}
}

func cm(name, value string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "prod",
		},
		"data": map[string]any{"key": value},
	}
}

func secretObj(name, password, token string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "prod",
		},
		"type": "Opaque",
		"data": map[string]any{
			"token": token,
		},
		"stringData": map[string]any{
			"password": password,
		},
	}
}

func ref(kind, name string) semantic.ResourceRef {
	return semantic.ResourceRef{
		APIVersion: "v1",
		Kind:       kind,
		Namespace:  "prod",
		Name:       name,
	}
}

func objectString(tb testing.TB, obj map[string]any, keys ...string) string {
	tb.Helper()
	var cur any = obj
	for _, key := range keys {
		m, ok := cur.(map[string]any)
		require.True(tb, ok)
		cur, ok = m[key]
		require.True(tb, ok)
	}
	s, ok := cur.(string)
	require.True(tb, ok)
	return s
}

func mustPlan(tb testing.TB, changes []semantic.ResourceChange, diags []semantic.Diagnostic) semantic.Plan {
	tb.Helper()
	p, err := semantic.New(semantic.Header{
		Project:     "web",
		Environment: "prod",
		Release:     "web",
		Namespace:   "prod",
	}, changes, diags)
	require.NoError(tb, err)
	return p
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Clean(filepath.Join("testdata", "golden", name+".golden"))
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing or unreadable: %v\n--- got ---\n%s", path, err, got)
	}
	assert.Equal(t, string(want), got)
}
