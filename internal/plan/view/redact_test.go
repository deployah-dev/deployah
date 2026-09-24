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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
)

func TestSecretRedaction_DefaultAndShowSecrets(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
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
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
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
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "same")),
		After:    snap(secretObj("s", "new-pass", "same")),
		Apply:    writeApply(),
	}}, nil)
	require.NotEmpty(t, p.Changes[0].Fields)
	idx := slices.IndexFunc(p.Changes[0].Fields, func(f semantic.FieldChange) bool {
		return f.Path == "/stringData/password"
	})
	require.GreaterOrEqual(t, idx, 0, "secret field change must exist on the unredacted plan")
	assert.Equal(t, "old-pass", p.Changes[0].Fields[idx].Before)
	assert.Equal(t, "new-pass", p.Changes[0].Fields[idx].After)

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
		{name: "v1", apiVersion: "v1", contains: []string{"+ apiVersion: v1", "+ kind: Secret", "name: s", "(redacted)"}, omits: []string{"hidden"}},
		{name: "core/v1", apiVersion: "core/v1", contains: []string{"(redacted)"}, omits: []string{"hidden"}},
		{name: "non-core group", apiVersion: "example.com/v1", contains: []string{"hidden"}, omits: []string{"(redacted)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := ref("Secret", "s")
			res.APIVersion = tt.apiVersion
			p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
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
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
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

func TestSecretRedaction_JSONValueShapes(t *testing.T) {
	t.Parallel()
	redactedSecret := `{
		"apiVersion": "v1",
		"kind": "Secret",
		"metadata": {"name": "s", "namespace": "prod"},
		"type": "Opaque",
		"data": {"token": "(redacted)"},
		"stringData": {"password": "(redacted)"}
	}`
	tests := []struct {
		name          string
		change        semantic.ResourceChange
		omit          []string
		humanContains []string
		at            []jsonPathWant
	}{
		{
			name: "nested values",
			change: semantic.ResourceChange{
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
			},
			omit: []string{`"secret"`},
			at: []jsonPathWant{{
				want: `{
					"apiVersion": "v1",
					"kind": "Secret",
					"metadata": {"name": "s", "namespace": "prod"},
					"data": {
						"scalar": "(redacted)",
						"nested": {"k": "(redacted)"},
						"list": ["(redacted)", null]
					},
					"stringData": {"plain": "(redacted)"}
				}`,
				path: []any{"changes", 0, "after"},
			}},
		},
		{
			name: "null leaf stays null",
			change: semantic.ResourceChange{
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
			},
			humanContains: []string{"token: null"},
			at: []jsonPathWant{{
				want: `{"token": null}`,
				path: []any{"changes", 0, "after", "data"},
			}},
		},
		{
			name: "whole map add keeps keys",
			change: semantic.ResourceChange{
				Resource: ref("Secret", "s"),
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before: snap(map[string]any{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata":   map[string]any{"name": "s", "namespace": "prod"},
					"type":       "Opaque",
				}),
				After: snap(secretObj("s", "new-pass", "new-tok")),
				Apply: writeApply(),
			},
			omit:          []string{"new-pass", "new-tok"},
			humanContains: []string{"token: (redacted)", "password: (redacted)"},
			at: []jsonPathWant{
				{want: redactedSecret, path: []any{"changes", 0, "after"}},
				{
					want: `[
						{"path":"/data","op":"add","after":{"token":"(redacted)"}},
						{"path":"/stringData","op":"add","after":{"password":"(redacted)"}}
					]`,
					path: []any{"changes", 0, "fields"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change}, nil)
			var human, jsonBuf bytes.Buffer
			require.NoError(t, view.WriteHuman(&human, p, view.Options{}))
			require.NoError(t, view.WriteJSON(&jsonBuf, p, view.Options{}))
			for _, omit := range tt.omit {
				assert.NotContains(t, human.String(), omit)
				assert.NotContains(t, jsonBuf.String(), omit)
			}
			for _, want := range tt.humanContains {
				assert.Contains(t, human.String(), want)
			}
			assertJSONPaths(t, jsonBuf.Bytes(), tt.at)
		})
	}
}

func TestWriteRenderers_CopyIsolation(t *testing.T) {
	t.Parallel()
	helm := &semantic.HelmOrigin{Release: "web", Namespace: "prod"}
	write := &semantic.WriteSemantics{Method: semantic.WriteServerSide, FieldManager: "deployah"}
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
	p, err := semantic.New(semantic.Header{Release: "web", Namespace: "prod"}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: helm},
		Action:   semantic.Update,
		Before:   snap(beforeObj),
		After:    snap(secretObj("s", "new", "tok2")),
		Apply:    semantic.ApplySemantics{Write: write},
	}}, nil, []semantic.Diagnostic{diag})
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes[0].Fields)
	fieldBefore := p.Changes[0].Fields[0].Before
	fieldAfter := p.Changes[0].Fields[0].After
	helmRelease := p.Changes[0].Origin.Helm.Release
	fieldManager := p.Changes[0].Apply.Write.FieldManager
	diagName := p.Diagnostics[0].Resource.Name

	require.NoError(t, view.WriteHuman(&bytes.Buffer{}, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&bytes.Buffer{}, p, view.Options{}))

	assert.Equal(t, helmRelease, p.Changes[0].Origin.Helm.Release)
	assert.Equal(t, fieldManager, p.Changes[0].Apply.Write.FieldManager)
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
		HelmAction: semantic.HelmUpgrade,
		Header:     semantic.Header{Release: "web"},
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
	assertJSONAt(t, jsonBuf.Bytes(), `{
		"labels": {"app": "web"},
		"args": ["serve"],
		"nested": ["x"],
		"empty": null
	}`, "changes", 0, "after")
	assertJSONAt(t, jsonBuf.Bytes(), `{}`, "changes", 1, "after")
}

func TestWriteRenderers_NilTasks(t *testing.T) {
	t.Parallel()
	p := semantic.Plan{
		HelmAction: semantic.HelmNone,
		Header:     semantic.Header{Release: "web"},
	}
	assert.Nil(t, p.Tasks)
	require.NoError(t, view.WriteHuman(&bytes.Buffer{}, p, view.Options{}))
	var jsonBuf bytes.Buffer
	require.NoError(t, view.WriteJSON(&jsonBuf, p, view.Options{}))
	assertJSONAt(t, jsonBuf.Bytes(), `[]`, "tasks")
}

func TestSecretRedaction_OmitsPlaintext(t *testing.T) {
	t.Parallel()
	redactedData := `{"token":"(redacted)"}`
	redactedStringData := `{"password":"(redacted)"}`
	redactedReplaceFields := `[
		{"path":"/data/token","op":"replace","before":"(redacted)","after":"(redacted)"},
		{"path":"/stringData/password","op":"replace","before":"(redacted)","after":"(redacted)"}
	]`
	tests := []struct {
		name string
		plan semantic.Plan
		omit []string
		at   []jsonPathWant
	}{
		{
			name: "create and delete changes",
			plan: mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{
				{
					Resource: ref("Secret", "created"),
					Origin:   helmOrigin(),
					Action:   semantic.Create,
					After:    snap(secretObj("created", "c-pass", "c-tok")),
					Apply:    writeApply(),
				},
				{
					Resource: ref("Secret", "removed"),
					Origin:   helmOrigin(),
					Action:   semantic.Delete,
					Before:   snap(secretObj("removed", "d-pass", "d-tok")),
					Apply:    deleteApply(),
				},
			}, nil),
			omit: []string{"c-pass", "d-pass", "c-tok", "d-tok"},
			at: []jsonPathWant{
				{want: redactedData, path: []any{"changes", 0, "after", "data"}},
				{want: redactedStringData, path: []any{"changes", 0, "after", "stringData"}},
				{want: "null", path: []any{"changes", 0, "before"}},
				{want: redactedData, path: []any{"changes", 1, "before", "data"}},
				{want: redactedStringData, path: []any{"changes", 1, "before", "stringData"}},
				{want: "null", path: []any{"changes", 1, "after"}},
			},
		},
		{
			name: "hook definitions",
			plan: mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUpdate,
				WillRun: true,
				Definitions: []semantic.HookDefinition{
					{
						Resource: ref("Secret", "hook-secret"),
						Action:   semantic.Update,
						Before:   snap(secretObj("hook-secret", "old-pass", "old-tok")),
						After:    snap(secretObj("hook-secret", "new-pass", "new-tok")),
					},
					{
						Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: "prod", Name: "created"},
						Action:   semantic.Create,
						After:    snap(secretObj("created", "c-pass", "c-tok")),
					},
					{
						Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: "prod", Name: "removed"},
						Action:   semantic.Delete,
						Before:   snap(secretObj("removed", "d-pass", "d-tok")),
					},
				},
			}}, nil),
			omit: []string{"old-pass", "new-pass", "c-pass", "d-pass", "old-tok", "new-tok", "c-tok", "d-tok"},
			at: []jsonPathWant{
				{want: redactedData, path: []any{"tasks", 0, "definitions", 0, "after", "data"}},
				{want: redactedStringData, path: []any{"tasks", 0, "definitions", 0, "after", "stringData"}},
				{want: "null", path: []any{"tasks", 0, "definitions", 0, "before"}},
				{want: redactedData, path: []any{"tasks", 0, "definitions", 1, "before", "data"}},
				{want: redactedStringData, path: []any{"tasks", 0, "definitions", 1, "before", "stringData"}},
				{want: redactedData, path: []any{"tasks", 0, "definitions", 1, "after", "data"}},
				{want: redactedStringData, path: []any{"tasks", 0, "definitions", 1, "after", "stringData"}},
				{want: redactedReplaceFields, path: []any{"tasks", 0, "definitions", 1, "fields"}},
				{want: redactedData, path: []any{"tasks", 0, "definitions", 2, "before", "data"}},
				{want: redactedStringData, path: []any{"tasks", 0, "definitions", 2, "before", "stringData"}},
				{want: "null", path: []any{"tasks", 0, "definitions", 2, "after"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var human, jsonBuf bytes.Buffer
			require.NoError(t, view.WriteHuman(&human, tt.plan, view.Options{}))
			require.NoError(t, view.WriteJSON(&jsonBuf, tt.plan, view.Options{}))
			for _, omit := range tt.omit {
				assert.NotContains(t, human.String(), omit)
				assert.NotContains(t, jsonBuf.String(), omit)
			}
			assertJSONPaths(t, jsonBuf.Bytes(), tt.at)
		})
	}
}
