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
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
)

func TestSecretRedaction_DefaultAndShowSecrets(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "old-tok")),
		After:    snap(secretObj("s", "new-pass", "new-tok")),
		Apply:    writeApply(),
	}}, nil)

	var hidden bytes.Buffer
	require.NoError(t, view.WriteHuman(&hidden, p, view.Options{}))
	text := hidden.String()
	assert.NotContains(t, text, "old-pass")
	assert.NotContains(t, text, "new-pass")
	assert.NotContains(t, text, "old-tok")
	assert.NotContains(t, text, "new-tok")
	assert.Contains(t, text, "token: (redacted)")
	assert.Contains(t, text, "password: (redacted)")
	assert.Contains(t, text, "data:")
	assert.Contains(t, text, "stringData:")
	assert.Contains(t, text, "s")

	var shown bytes.Buffer
	require.NoError(t, view.WriteHuman(&shown, p, view.Options{ShowSecrets: true}))
	assert.Contains(t, shown.String(), "old-pass")
	assert.Contains(t, shown.String(), "new-pass")
}

func TestSecretRedaction_ConfigMapNotRedacted(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(cm("app", "old")),
		After:    snap(cm("app", "new")),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	assert.Contains(t, buf.String(), "old")
	assert.Contains(t, buf.String(), "new")
	assert.NotContains(t, buf.String(), "(redacted)")
}

func TestSecretRedaction_FieldChangeComputedBeforeRedaction(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "same")),
		After:    snap(secretObj("s", "new-pass", "same")),
		Apply:    writeApply(),
	}}, nil)
	require.NotEmpty(t, p.Changes[0].Fields)
	found := false
	for _, f := range p.Changes[0].Fields {
		if f.Path == "/stringData/password" {
			found = true
			assert.Equal(t, "old-pass", f.Before)
			assert.Equal(t, "new-pass", f.After)
		}
	}
	assert.True(t, found, "secret field change must exist on the unredacted plan")

	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	assert.Contains(t, buf.String(), "password: (redacted)")
	assert.Contains(t, buf.String(), "stringData:")
	assert.NotContains(t, buf.String(), "old-pass")
	assert.Equal(t, "old-pass", objectString(t, p.Changes[0].Before.Object, "stringData", "password"))
}

func TestSecretRedaction_CoreAPIVersions(t *testing.T) {
	t.Parallel()
	obj := secretObj("s", "hidden", "tok")
	tests := []struct {
		name       string
		apiVersion string
		contains   []string
		omits      []string
	}{
		{name: "v1", apiVersion: "v1", contains: []string{"(redacted)"}, omits: []string{"hidden"}},
		{name: "core/v1", apiVersion: "core/v1", contains: []string{"(redacted)"}, omits: []string{"hidden"}},
		{name: "non-core group", apiVersion: "example.com/v1", contains: []string{"hidden"}, omits: []string{"(redacted)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := ref("Secret", "s")
			res.APIVersion = tt.apiVersion
			p := mustPlan(t, []semantic.ResourceChange{{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(obj),
				Apply:    writeApply(),
			}}, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			for _, want := range tt.contains {
				assert.Contains(t, text, want)
			}
			for _, omit := range tt.omits {
				assert.NotContains(t, text, omit)
			}
		})
	}
}

func TestSecretRedaction_MissingSectionsStayAbsent(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After: snap(map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": "s", "namespace": "prod"},
			"type":       "Opaque",
		}),
		Apply: writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "type: Opaque")
	assert.NotContains(t, text, "+ data:")
	assert.NotContains(t, text, "+ stringData:")
	assert.NotContains(t, text, "(redacted)")
}

func TestSecretRedaction_NestedValueShapes(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After: snap(map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": "s", "namespace": "prod"},
			"data": map[string]any{
				"scalar": "secret",
				"nested": map[string]any{"k": "secret"},
				"list":   []any{"secret", nil},
			},
			"stringData": map[string]string{"plain": "secret"},
		}),
		Apply: writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assert.NotContains(t, buf.String(), `"secret"`)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	changes, ok := doc["changes"].([]any)
	require.True(t, ok)
	require.Len(t, changes, 1)
	change, ok := changes[0].(map[string]any)
	require.True(t, ok)
	after, ok := change["after"].(map[string]any)
	require.True(t, ok)
	data, ok := after["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "(redacted)", data["scalar"])
	assert.Equal(t, map[string]any{"k": "(redacted)"}, data["nested"])
	assert.Equal(t, []any{"(redacted)", nil}, data["list"])
	stringData, ok := after["stringData"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "(redacted)", stringData["plain"])
}

func TestSecretRedaction_NullLeafStaysNull(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After: snap(map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": "s", "namespace": "prod"},
			"data":       map[string]any{"token": nil},
		}),
		Apply: writeApply(),
	}}, nil)
	var human, jsonBuf bytes.Buffer
	require.NoError(t, view.WriteHuman(&human, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&jsonBuf, p, view.Options{}))
	assert.Contains(t, human.String(), "token: null")
	assert.NotContains(t, human.String(), "leaked")
	assert.Contains(t, jsonBuf.String(), `"token": null`)
}

func TestWriteRenderers_CopyIsolation(t *testing.T) {
	t.Parallel()
	helm := &semantic.HelmOrigin{Release: "web", Namespace: "prod"}
	write := &semantic.WriteSemantics{Method: semantic.WriteServerSide, FieldManager: "deployah"}
	del := &semantic.DeleteSemantics{Propagation: semantic.PropagationBackground}
	res := ref("Secret", "s")
	diag := semantic.Diagnostic{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: managed-fields-migration",
		Resource: &res,
	}
	labels := map[string]string{"app": "web"}
	args := []string{"serve"}
	beforeObj := secretObj("s", "old", "tok")
	meta, ok := beforeObj["metadata"].(map[string]any)
	require.True(t, ok)
	meta["labels"] = labels
	beforeObj["args"] = args
	p, err := semantic.New(semantic.Header{Release: "web", Namespace: "prod"}, []semantic.ResourceChange{{
		Resource: res,
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: helm},
		Action:   semantic.Recreate,
		Before:   snap(beforeObj),
		After:    snap(secretObj("s", "new", "tok2")),
		Apply:    semantic.ApplySemantics{Write: write, Delete: del},
	}}, []semantic.Diagnostic{diag})
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes[0].Fields)
	fieldBefore := p.Changes[0].Fields[0].Before
	fieldAfter := p.Changes[0].Fields[0].After
	helmRelease := p.Changes[0].Origin.Helm.Release
	fieldManager := p.Changes[0].Apply.Write.FieldManager
	propagation := p.Changes[0].Apply.Delete.Propagation
	diagName := p.Diagnostics[0].Resource.Name

	require.NoError(t, view.WriteHuman(&bytes.Buffer{}, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&bytes.Buffer{}, p, view.Options{}))

	assert.Equal(t, helmRelease, p.Changes[0].Origin.Helm.Release)
	assert.Equal(t, fieldManager, p.Changes[0].Apply.Write.FieldManager)
	assert.Equal(t, propagation, p.Changes[0].Apply.Delete.Propagation)
	assert.Equal(t, diagName, p.Diagnostics[0].Resource.Name)
	assert.Equal(t, "old", objectString(t, p.Changes[0].Before.Object, "stringData", "password"))
	assert.Equal(t, "new", objectString(t, p.Changes[0].After.Object, "stringData", "password"))
	assert.Equal(t, fieldBefore, p.Changes[0].Fields[0].Before)
	assert.Equal(t, fieldAfter, p.Changes[0].Fields[0].After)
	assert.NotEqual(t, "(redacted)", p.Changes[0].Fields[0].Before)
	assert.NotEqual(t, "(redacted)", p.Changes[0].Fields[0].After)
}

func TestWriteRenderers_ManualSnapshotShapes(t *testing.T) {
	t.Parallel()
	p := semantic.Plan{
		Completeness: semantic.CompletenessComplete,
		Header:       semantic.Header{Release: "web"},
		Changes: []semantic.ResourceChange{
			{
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After: &semantic.ResourceSnapshot{Object: map[string]any{
					"labels": map[string]string{"app": "web"},
					"args":   []string{"serve"},
					"nested": []any{"x"},
					"empty":  map[string]any(nil),
				}},
				Apply: writeApply(),
			},
			{
				Resource: ref("ConfigMap", "blank"),
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{},
				Apply:    writeApply(),
			},
		},
	}
	var human, jsonBuf bytes.Buffer
	require.NoError(t, view.WriteHuman(&human, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&jsonBuf, p, view.Options{}))
	assert.Contains(t, human.String(), "app: web")
	assert.Contains(t, human.String(), "serve")
	assert.Contains(t, jsonBuf.String(), `"app": "web"`)
}

func TestWriteRenderers_NilExecutions(t *testing.T) {
	t.Parallel()
	p := semantic.Plan{
		Completeness: semantic.CompletenessComplete,
		Header:       semantic.Header{Release: "web"},
	}
	assert.Nil(t, p.Executions)
	require.NoError(t, view.WriteHuman(&bytes.Buffer{}, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&bytes.Buffer{}, p, view.Options{}))
}

func TestSecretRedaction_WholeMapKeepsKeys(t *testing.T) {
	t.Parallel()
	before := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "s", "namespace": "prod"},
		"type":       "Opaque",
	}
	after := secretObj("s", "new-pass", "new-tok")
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(before),
		After:    snap(after),
		Apply:    writeApply(),
	}}, nil)
	var hidden, jsonBuf bytes.Buffer
	require.NoError(t, view.WriteHuman(&hidden, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&jsonBuf, p, view.Options{}))
	assert.Contains(t, hidden.String(), "token: (redacted)")
	assert.Contains(t, hidden.String(), "password: (redacted)")
	assert.NotContains(t, hidden.String(), "new-pass")
	assert.NotContains(t, jsonBuf.String(), `"after": "(redacted)"`)
	assert.Contains(t, jsonBuf.String(), `"token": "(redacted)"`)
	assert.Contains(t, jsonBuf.String(), `"password": "(redacted)"`)
}
