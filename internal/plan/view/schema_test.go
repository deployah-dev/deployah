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

	doc := mustPlanDoc(t, mustPlan(t, nil, nil))
	assert.Equal(t, view.SchemaV1ID, doc["schema"])
}

func TestSchemaV1_RejectsMalformedDocuments(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	update := mustPlanDoc(t, mustPlan(t, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(cm("app", "v1")),
		After:    snap(cm("app", "v2")),
		Apply:    writeApply(),
	}}, nil))
	create := mustPlanDoc(t, mustPlan(t, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(cm("app", "v1")),
		Apply:    writeApply(),
	}}, nil))
	deleteDoc := mustPlanDoc(t, mustPlan(t, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Delete,
		Before:   snap(cm("app", "v1")),
		Apply:    deleteApply(),
	}}, nil))
	recreate := mustPlanDoc(t, mustPlan(t, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Recreate,
		Before:   snap(cm("app", "v1")),
		After:    snap(cm("app", "v2")),
		Apply:    bothApply(),
	}}, nil))
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
		{name: "recreate with after null", raw: patched(t, recreate, func(d map[string]any) {
			firstChange(t, d)["after"] = nil
		})},
		{name: "recreate with before null", raw: patched(t, recreate, func(d map[string]any) {
			firstChange(t, d)["before"] = nil
		})},
		{name: "recreate missing write", raw: patched(t, recreate, func(d map[string]any) {
			delete(applyOf(t, d), "write")
		})},
		{name: "recreate missing delete", raw: patched(t, recreate, func(d map[string]any) {
			delete(applyOf(t, d), "delete")
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertSchemaRejects(t, tt.raw)
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
