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

func TestSchemaV1ID_MatchesEmbeddedAndRendered(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://deployah.dev/schemas/plan/v1/schema.json", view.SchemaV1ID)

	var sch map[string]any
	require.NoError(t, json.Unmarshal(view.SchemaV1(), &sch))
	assert.Equal(t, view.SchemaV1ID, sch["$id"])
	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", sch["$schema"])
	props, ok := sch["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, props, "chartCRDs")
	required, ok := sch["required"].([]any)
	require.True(t, ok)
	assert.Contains(t, required, "chartCRDs")
	assert.NotContains(t, required, "completeness")
	assert.NotContains(t, props, "completeness")

	defs, ok := sch["$defs"].(map[string]any)
	require.True(t, ok)
	summary, ok := defs["Summary"].(map[string]any)
	require.True(t, ok)
	summaryProps, ok := summary["properties"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, summaryProps, "replace")
	change, ok := defs["ResourceChange"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"resource", "action", "before", "after", "fields"}, change["required"])

	doc := mustPlanDoc(t, mustPlan(t, semantic.HelmNone, nil))
	assert.Equal(t, view.SchemaV1ID, doc["schema"])
}

func TestSchemaV1_RejectsMalformedDocuments(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	update := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Action:   semantic.Update,
		Before:   snap(cm("app", "v1")),
		After:    snap(cm("app", "v2")),
	}}))
	create := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Action:   semantic.Create,
		After:    snap(cm("app", "v1")),
	}}))
	deleteDoc := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Action:   semantic.Delete,
		Before:   snap(cm("app", "v1")),
	}}))
	noneDoc := mustPlanDoc(t, mustPlan(t, semantic.HelmNone, nil))
	addField := map[string]any{"path": "/data/extra", "op": "add", "after": "x"}

	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "field add missing after", raw: patched(t, update, func(d map[string]any) {
			f := firstField(t, d)
			f["op"] = "add"
			delete(f, "before")
			delete(f, "after")
		})},
		{name: "field add with explicit null before", raw: patched(t, update, func(d map[string]any) {
			f := firstField(t, d)
			f["op"] = "add"
			f["before"] = nil
			f["after"] = "x"
		})},
		{name: "field remove missing before", raw: patched(t, update, func(d map[string]any) {
			f := firstField(t, d)
			f["op"] = "remove"
			delete(f, "before")
			delete(f, "after")
		})},
		{name: "field remove with explicit null after", raw: patched(t, update, func(d map[string]any) {
			f := firstField(t, d)
			f["op"] = "remove"
			f["before"] = "x"
			f["after"] = nil
		})},
		{name: "field replace missing before", raw: patched(t, update, func(d map[string]any) {
			delete(firstField(t, d), "before")
		})},
		{name: "field replace missing after", raw: patched(t, update, func(d map[string]any) {
			delete(firstField(t, d), "after")
		})},
		{name: "create with before object", raw: patched(t, create, func(d map[string]any) {
			firstChange(t, d)["before"] = map[string]any{"kind": "ConfigMap"}
		})},
		{name: "create with after null", raw: patched(t, create, func(d map[string]any) {
			firstChange(t, d)["after"] = nil
		})},
		{name: "create with fields", raw: patched(t, create, func(d map[string]any) {
			firstChange(t, d)["fields"] = []any{addField}
		})},
		{name: "update with before null", raw: patched(t, update, func(d map[string]any) {
			firstChange(t, d)["before"] = nil
		})},
		{name: "update missing after", raw: patched(t, update, func(d map[string]any) {
			firstChange(t, d)["after"] = nil
		})},
		{name: "update with empty fields", raw: patched(t, update, func(d map[string]any) {
			firstChange(t, d)["fields"] = []any{}
		})},
		{name: "delete with after object", raw: patched(t, deleteDoc, func(d map[string]any) {
			firstChange(t, d)["after"] = map[string]any{"kind": "ConfigMap"}
		})},
		{name: "delete with fields", raw: patched(t, deleteDoc, func(d map[string]any) {
			firstChange(t, d)["fields"] = []any{addField}
		})},
		{name: "top-level completeness", raw: patched(t, update, func(d map[string]any) {
			d["completeness"] = "complete"
		})},
		{name: "action replace", raw: patched(t, update, func(d map[string]any) {
			firstChange(t, d)["action"] = "replace"
		})},
		{name: "unknown top-level executions", raw: patched(t, update, func(d map[string]any) {
			d["executions"] = []any{}
		})},
		{name: "unknown top-level field", raw: patched(t, update, func(d map[string]any) {
			d["unknown"] = true
		})},
		{name: "install without freshInstall", raw: patched(t, noneDoc, func(d map[string]any) {
			d["helmAction"] = "install"
		})},
		{name: "freshInstall with none", raw: patched(t, noneDoc, func(d map[string]any) {
			asObject(t, d["header"])["freshInstall"] = true
		})},
		{name: "freshInstall with upgrade", raw: patched(t, update, func(d map[string]any) {
			asObject(t, d["header"])["freshInstall"] = true
		})},
		{name: "none with changes", raw: patched(t, update, func(d map[string]any) {
			d["helmAction"] = "none"
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertSchemaRejects(t, tt.raw)
		})
	}
}

func TestSchemaV1_RejectsHookUpdateWithoutFields(t *testing.T) {
	t.Parallel()
	doc := mustPlanDoc(t, mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
		Name:    "migrate",
		Phase:   semantic.TaskPreDeploy,
		Action:  semantic.TaskUpdate,
		WillRun: true,
		Definitions: []semantic.HookDefinition{{
			Resource: ref("ConfigMap", "app"),
			Action:   semantic.Update,
			Before:   snap(cm("app", "v1")),
			After:    snap(cm("app", "v2")),
		}},
	}}))
	tasks, ok := doc["tasks"].([]any)
	require.True(t, ok)
	require.Len(t, tasks, 1)
	defs, ok := asObject(t, tasks[0])["definitions"].([]any)
	require.True(t, ok)
	require.Len(t, defs, 1)
	asObject(t, defs[0])["fields"] = []any{}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	assertSchemaRejects(t, raw)
}

func TestSchemaV1_AcceptsNamespaceCreate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan semantic.Plan
	}{
		{name: "install namespace", plan: mustPlanWithHeader(t, semantic.Header{
			Project:      "web",
			Environment:  "prod",
			Release:      "web",
			Namespace:    "prod",
			FreshInstall: true,
		}, semantic.HelmInstall, []semantic.ResourceChange{{
			Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: "prod"},
			Action:   semantic.Create,
			After: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata":   map[string]any{"name": "prod"},
			}),
		}}, nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, view.WriteJSON(&buf, tt.plan, view.Options{}))
			validatePlanSchema(t, buf.Bytes())
		})
	}
}

func TestSchemaV1_ChartCRDs(t *testing.T) {
	t.Parallel()
	valid := []struct {
		name        string
		header      semantic.Header
		action      semantic.HelmAction
		lifecycle   semantic.ChartCRDLifecycle
		willProcess bool
	}{
		{name: "process", header: semantic.Header{Project: "web", FreshInstall: true}, action: semantic.HelmInstall, lifecycle: semantic.ChartCRDProcess, willProcess: true},
		{name: "skip", header: semantic.Header{Project: "web", FreshInstall: true}, action: semantic.HelmInstall, lifecycle: semantic.ChartCRDSkip},
		{name: "upgrade", header: semantic.Header{Project: "web"}, action: semantic.HelmUpgrade, lifecycle: semantic.ChartCRDUpgrade},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlanWithHeader(t, tc.header, tc.action, nil, nil)
			var attachErr error
			p, attachErr = semantic.AttachChartCRDs(p, []semantic.ChartCRD{{
				Source:      ".deployah/crds/widgets.yaml",
				Kind:        "CustomResourceDefinition",
				Name:        "widgets.example.com",
				Lifecycle:   tc.lifecycle,
				WillProcess: tc.willProcess,
			}})
			require.NoError(t, attachErr)
			raw, err := json.Marshal(mustPlanDoc(t, p))
			require.NoError(t, err)
			validatePlanSchema(t, raw)
		})
	}

	p := mustPlan(t, semantic.HelmUpgrade, nil)
	var err error
	p, err = semantic.AttachChartCRDs(p, []semantic.ChartCRD{{
		Source:    ".deployah/crds/widgets.yaml",
		Kind:      "CustomResourceDefinition",
		Name:      "widgets.example.com",
		Lifecycle: semantic.ChartCRDUpgrade,
	}})
	require.NoError(t, err)
	base := mustPlanDoc(t, p)

	tests := []struct {
		name string
		fn   func(map[string]any)
	}{
		{name: "unknown property", fn: func(d map[string]any) { firstChartCRD(t, d)["action"] = "create" }},
		{name: "lifecycle create", fn: func(d map[string]any) { firstChartCRD(t, d)["lifecycle"] = "create" }},
		{name: "upgrade willProcess true", fn: func(d map[string]any) { firstChartCRD(t, d)["willProcess"] = true }},
		{name: "process willProcess false", fn: func(d map[string]any) {
			crd := firstChartCRD(t, d)
			crd["lifecycle"] = "process"
			crd["willProcess"] = false
		}},
		{name: "skip willProcess true", fn: func(d map[string]any) {
			crd := firstChartCRD(t, d)
			crd["lifecycle"] = "skip"
			crd["willProcess"] = true
		}},
		{name: "missing name", fn: func(d map[string]any) { delete(firstChartCRD(t, d), "name") }},
		{name: "apiVersion field", fn: func(d map[string]any) { firstChartCRD(t, d)["apiVersion"] = "apiextensions.k8s.io/v1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertSchemaRejects(t, patched(t, base, tt.fn))
		})
	}
}

func TestSchemaV1_AcceptsHelmNoneWithDrift(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, sampleDriftPlan(t), view.Options{}))
	validatePlanSchema(t, buf.Bytes())
}

func TestSchemaV1_RejectsMalformedDrift(t *testing.T) {
	t.Parallel()
	base := mustPlanDoc(t, sampleDriftPlan(t))
	install := mustPlanDoc(t, mustPlanWithHeader(t, semantic.Header{
		Project:      "web",
		Environment:  "prod",
		Release:      "web",
		Namespace:    "prod",
		FreshInstall: true,
	}, semantic.HelmInstall, nil, nil))
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "missing with live", raw: patched(t, base, func(d map[string]any) {
			items, ok := d["drift"].([]any)
			require.True(t, ok)
			asObject(t, items[2])["live"] = map[string]any{"kind": "ConfigMap"}
		})},
		{name: "unexpected with previous", raw: patched(t, base, func(d map[string]any) {
			items, ok := d["drift"].([]any)
			require.True(t, ok)
			asObject(t, items[1])["previous"] = map[string]any{"kind": "ConfigMap"}
		})},
		{name: "modified without fields", raw: patched(t, base, func(d map[string]any) {
			items, ok := d["drift"].([]any)
			require.True(t, ok)
			asObject(t, items[0])["fields"] = []any{}
		})},
		{name: "unknown action", raw: patched(t, base, func(d map[string]any) {
			items, ok := d["drift"].([]any)
			require.True(t, ok)
			asObject(t, items[0])["action"] = "update"
		})},
		{name: "missing drift key", raw: patched(t, base, func(d map[string]any) {
			delete(d, "drift")
		})},
		{name: "fresh install with drift", raw: patched(t, install, func(d map[string]any) {
			d["drift"] = base["drift"]
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertSchemaRejects(t, tt.raw)
		})
	}
}

func sampleDriftPlan(t *testing.T) semantic.Plan {
	t.Helper()
	p, err := semantic.AttachDrift(mustPlan(t, semantic.HelmNone, nil), []semantic.DriftChange{
		{
			Resource: ref("ConfigMap", "app"),
			Action:   semantic.DriftModified,
			Previous: snap(cm("app", "old")),
			Live:     snap(cm("app", "new")),
			Fields: []semantic.FieldChange{{
				Path:   "/data/key",
				Op:     semantic.FieldReplace,
				Before: "old",
				After:  "new",
			}},
		},
		{
			Resource: ref("ConfigMap", "other"),
			Action:   semantic.DriftMissing,
			Previous: snap(cm("other", "gone")),
		},
		{
			Resource: ref("ConfigMap", "extra"),
			Action:   semantic.DriftUnexpected,
			Live:     snap(cm("extra", "live")),
		},
	})
	require.NoError(t, err)
	return p
}

func firstChartCRD(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	crds, ok := doc["chartCRDs"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, crds)
	return asObject(t, crds[0])
}

func assertSchemaRejects(t *testing.T, raw []byte) {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	require.Error(t, compilePlanSchema(t).Validate(v))
}

func mustPlanDoc(t *testing.T, p semantic.Plan) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	return doc
}

func patched(t *testing.T, base map[string]any, fn func(map[string]any)) []byte {
	t.Helper()
	doc := cloneJSONMap(t, base)
	fn(doc)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return raw
}

func cloneJSONMap(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func asObject(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok)
	return m
}

func firstChange(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	changes, ok := doc["changes"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, changes)
	return asObject(t, changes[0])
}

func firstField(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	fields, ok := firstChange(t, doc)["fields"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, fields)
	f, ok := fields[0].(map[string]any)
	require.True(t, ok)
	return f
}
