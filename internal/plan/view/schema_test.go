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

	doc := mustPlanDoc(t, mustPlan(t, semantic.HelmNone, nil, nil))
	assert.Equal(t, view.SchemaV1ID, doc["schema"])
}

func TestSchemaV1_RejectsMalformedDocuments(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	update := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(cm("app", "v1")),
		After:    snap(cm("app", "v2")),
		Apply:    writeApply(),
	}}, nil))
	create := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(cm("app", "v1")),
		Apply:    writeApply(),
	}}, nil))
	deleteDoc := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Delete,
		Before:   snap(cm("app", "v1")),
		Apply:    deleteApply(),
	}}, nil))
	replace := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Replace,
		Before:   snap(cm("app", "v1")),
		After:    snap(cm("app", "v2")),
		Apply:    bothApply(),
	}}, nil))
	createWrite := mustPlanDoc(t, mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(cm("app", "v1")),
		Apply:    writeCreate(),
	}}, nil))
	noneDoc := mustPlanDoc(t, mustPlan(t, semantic.HelmNone, nil, nil))
	ssaWrite := map[string]any{
		"method":         "server_side_apply",
		"fieldManager":   "deployah",
		"forceConflicts": false,
	}
	bgDelete := map[string]any{"propagation": "background"}
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
		{name: "origin helm missing helm", raw: patched(t, update, func(d map[string]any) {
			delete(asObject(t, firstChange(t, d)["origin"]), "helm")
		})},
		{name: "empty fieldManager", raw: patched(t, update, func(d map[string]any) {
			asObject(t, applyOf(t, d)["write"])["fieldManager"] = ""
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
		{name: "create missing write", raw: patched(t, create, func(d map[string]any) {
			delete(applyOf(t, d), "write")
		})},
		{name: "create with delete", raw: patched(t, create, func(d map[string]any) {
			applyOf(t, d)["delete"] = bgDelete
		})},
		{name: "update with before null", raw: patched(t, update, func(d map[string]any) {
			firstChange(t, d)["before"] = nil
		})},
		{name: "update missing write", raw: patched(t, update, func(d map[string]any) {
			delete(applyOf(t, d), "write")
		})},
		{name: "update with delete", raw: patched(t, update, func(d map[string]any) {
			applyOf(t, d)["delete"] = bgDelete
		})},
		{name: "delete with after object", raw: patched(t, deleteDoc, func(d map[string]any) {
			firstChange(t, d)["after"] = map[string]any{"kind": "ConfigMap"}
		})},
		{name: "delete with fields", raw: patched(t, deleteDoc, func(d map[string]any) {
			firstChange(t, d)["fields"] = []any{addField}
		})},
		{name: "delete with write", raw: patched(t, deleteDoc, func(d map[string]any) {
			applyOf(t, d)["write"] = ssaWrite
		})},
		{name: "delete missing delete", raw: patched(t, deleteDoc, func(d map[string]any) {
			delete(applyOf(t, d), "delete")
		})},
		{name: "replace with after null", raw: patched(t, replace, func(d map[string]any) {
			firstChange(t, d)["after"] = nil
		})},
		{name: "replace with before null", raw: patched(t, replace, func(d map[string]any) {
			firstChange(t, d)["before"] = nil
		})},
		{name: "replace missing write", raw: patched(t, replace, func(d map[string]any) {
			delete(applyOf(t, d), "write")
		})},
		{name: "replace missing delete", raw: patched(t, replace, func(d map[string]any) {
			delete(applyOf(t, d), "delete")
		})},
		{name: "unknown top-level executions", raw: patched(t, update, func(d map[string]any) {
			d["executions"] = []any{}
		})},
		{name: "unknown top-level field", raw: patched(t, update, func(d map[string]any) {
			d["unknown"] = true
		})},
		{name: "hook definition apply", raw: patched(t, mustPlanDoc(t, mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
			Name:    "migrate",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskCreate,
			WillRun: true,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("Job", "migrate"),
				Action:   semantic.Create,
				After:    snap(cm("migrate", "v1")),
			}},
		}}, nil)), func(d map[string]any) {
			tasks, ok := d["tasks"].([]any)
			require.True(t, ok)
			require.NotEmpty(t, tasks)
			defs, ok := asObject(t, tasks[0])["definitions"].([]any)
			require.True(t, ok)
			require.NotEmpty(t, defs)
			asObject(t, defs[0])["apply"] = map[string]any{"write": ssaWrite}
		})},
		{name: "crd origin with helm", raw: patched(t, update, func(d map[string]any) {
			origin := asObject(t, firstChange(t, d)["origin"])
			origin["kind"] = "crd"
		})},
		{name: "namespace origin with helm", raw: patched(t, update, func(d map[string]any) {
			origin := asObject(t, firstChange(t, d)["origin"])
			origin["kind"] = "namespace"
		})},
		{name: "create write with fieldManager", raw: patched(t, createWrite, func(d map[string]any) {
			asObject(t, applyOf(t, d)["write"])["fieldManager"] = "deployah"
		})},
		{name: "create write with forceConflicts", raw: patched(t, createWrite, func(d map[string]any) {
			asObject(t, applyOf(t, d)["write"])["forceConflicts"] = true
		})},
		{name: "update write method create", raw: patched(t, update, func(d map[string]any) {
			write := asObject(t, applyOf(t, d)["write"])
			write["method"] = "create"
			delete(write, "fieldManager")
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
		{name: "none with origin helm", raw: patched(t, update, func(d map[string]any) {
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

func TestSchemaV1_AcceptsHelmActionOrigins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		plan semantic.Plan
	}{
		{name: "none with origin crd", plan: mustPlan(t, semantic.HelmNone, []semantic.ResourceChange{{
			Resource: semantic.ResourceRef{
				APIVersion: "apiextensions.k8s.io/v1",
				Kind:       "CustomResourceDefinition",
				Name:       "widgets.example.com",
			},
			Origin: semantic.ResourceOrigin{Kind: semantic.OriginCRD},
			Action: semantic.Create,
			After: snap(map[string]any{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinition",
				"metadata":   map[string]any{"name": "widgets.example.com"},
			}),
			Apply: writeCreate(),
		}}, nil)},
		{name: "install with origin namespace", plan: mustPlanWithHeader(t, semantic.Header{
			Project:      "web",
			Environment:  "prod",
			Release:      "web",
			Namespace:    "prod",
			FreshInstall: true,
		}, semantic.HelmInstall, []semantic.ResourceChange{{
			Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: "prod"},
			Origin:   semantic.ResourceOrigin{Kind: semantic.OriginNamespace},
			Action:   semantic.Create,
			After: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata":   map[string]any{"name": "prod"},
			}),
			Apply: writeCreate(),
		}}, nil, nil)},
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

func applyOf(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	return asObject(t, firstChange(t, doc)["apply"])
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
