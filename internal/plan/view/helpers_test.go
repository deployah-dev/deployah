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
	"encoding/json"
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

func noisyCM(name, value, rv, uid string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":              name,
			"namespace":         "prod",
			"uid":               uid,
			"resourceVersion":   rv,
			"generation":        3,
			"creationTimestamp": "2020-01-01T00:00:00Z",
			"managedFields": []any{
				map[string]any{"manager": "helm"},
			},
			"annotations": map[string]any{
				"example.com/keep": "yes",
				"cert-manager.io/issue-temporary-certificate": "true",
			},
			"labels": map[string]any{"app": "web"},
		},
		"data":   map[string]any{"key": value},
		"status": map[string]any{"observedGeneration": 3},
	}
}

func widget(name, color string) map[string]any {
	return map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": name, "namespace": "prod"},
		"spec":       map[string]any{"color": color, "size": "large"},
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

func batchRef(kind, name string) semantic.ResourceRef {
	return semantic.ResourceRef{
		APIVersion: "batch/v1",
		Kind:       kind,
		Namespace:  "prod",
		Name:       name,
	}
}

func cmWithMeta(name, value string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "prod",
			"labels":    map[string]any{"app": "web"},
			"annotations": map[string]any{
				"example.com/keep": "yes",
			},
		},
		"data": map[string]any{"key": value},
	}
}

func jobObj(name, task, hook, weight string) map[string]any {
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "prod",
			"labels": map[string]any{
				"deployah.dev/task": task,
			},
			"annotations": map[string]any{
				"helm.sh/hook":        hook,
				"helm.sh/hook-weight": weight,
			},
		},
		"spec": jobSpec(task),
	}
}

func cronJob(name, task, schedule string) map[string]any {
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "prod",
			"labels": map[string]any{
				"deployah.dev/task": task,
			},
		},
		"spec": map[string]any{
			"schedule": schedule,
			"jobTemplate": map[string]any{
				"spec": jobSpec(task),
			},
		},
	}
}

func jobSpec(container string) map[string]any {
	return map[string]any{
		"backoffLimit": 1,
		"template": map[string]any{
			"spec": map[string]any{
				"restartPolicy": "OnFailure",
				"containers": []any{
					map[string]any{
						"name":    container,
						"image":   "ghcr.io/example/web:1.2.3",
						"command": []any{"./" + container},
					},
				},
			},
		},
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

func createChangeForHuman() semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(cm("app", "v1")),
		Apply:    writeApply(),
	}
}

func humanHeader() semantic.Header {
	return semantic.Header{
		Project:     "web",
		Environment: "prod",
		Release:     "web",
		Namespace:   "prod",
		Context:     "production-eu",
		Revision:    12,
	}
}

func mustPlan(tb testing.TB, changes []semantic.ResourceChange, diags []semantic.Diagnostic) semantic.Plan {
	tb.Helper()
	return mustPlanWithTasks(tb, changes, nil, diags)
}

func mustPlanWithTasks(tb testing.TB, changes []semantic.ResourceChange, tasks []semantic.TaskPlan, diags []semantic.Diagnostic) semantic.Plan {
	tb.Helper()
	return mustPlanWithHeader(tb, semantic.Header{
		Project:     "web",
		Environment: "prod",
		Release:     "web",
		Namespace:   "prod",
	}, changes, tasks, diags)
}

func mustPlanWithHeader(tb testing.TB, header semantic.Header, changes []semantic.ResourceChange, tasks []semantic.TaskPlan, diags []semantic.Diagnostic) semantic.Plan {
	tb.Helper()
	p, err := semantic.New(header, changes, tasks, diags)
	require.NoError(tb, err)
	return p
}

func assertNoBookkeeping(t *testing.T, text string) {
	t.Helper()
	assert.NotContains(t, text, "resourceVersion")
	assert.NotContains(t, text, "managedFields")
	assert.NotContains(t, text, "creationTimestamp")
	assert.NotContains(t, text, "observedGeneration")
	assert.NotContains(t, text, "uid:")
	assert.NotContains(t, text, "generation:")
	assert.NotContains(t, text, "status:")
}

func assertKeepsUserMeta(t *testing.T, text string) {
	t.Helper()
	assert.Contains(t, text, "example.com/keep")
	assert.Contains(t, text, "cert-manager.io/issue-temporary-certificate")
	assert.Contains(t, text, "app: web")
}

func k8sObj(apiVersion, kind, namespace, name string) map[string]any {
	meta := map[string]any{"name": name}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	return map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   meta,
	}
}

func jsonSelect(tb testing.TB, raw []byte, path ...any) string {
	tb.Helper()
	var cur any
	require.NoError(tb, json.Unmarshal(raw, &cur))
	for _, p := range path {
		switch key := p.(type) {
		case string:
			m, ok := cur.(map[string]any)
			require.True(tb, ok, "jsonSelect: expected object at %v", p)
			next, ok := m[key]
			require.True(tb, ok, "jsonSelect: missing key %q", key)
			cur = next
		case int:
			a, ok := cur.([]any)
			require.True(tb, ok, "jsonSelect: expected array at %v", p)
			require.GreaterOrEqual(tb, key, 0)
			require.Less(tb, key, len(a))
			cur = a[key]
		default:
			tb.Fatalf("jsonSelect: unsupported path element %T", p)
		}
	}
	b, err := json.Marshal(cur)
	require.NoError(tb, err)
	return string(b)
}

func assertJSONAt(tb testing.TB, raw []byte, want string, path ...any) {
	tb.Helper()
	assert.JSONEq(tb, want, jsonSelect(tb, raw, path...))
}

type jsonPathWant struct {
	want string
	path []any
}

func assertJSONPaths(tb testing.TB, raw []byte, wants []jsonPathWant) {
	tb.Helper()
	for _, w := range wants {
		assertJSONAt(tb, raw, w.want, w.path...)
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	assert.Equal(t, string(readGolden(t, name, got)), got)
}

func assertJSONGolden(t *testing.T, name, got string) {
	t.Helper()
	assert.JSONEq(t, string(readGolden(t, name, got)), got)
}

func readGolden(t *testing.T, name, got string) []byte {
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
	return want
}
