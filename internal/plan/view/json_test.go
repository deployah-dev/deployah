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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestWriteJSON_HeaderContextAndRevision(t *testing.T) {
	t.Parallel()
	p := mustPlanWithHeader(t, humanHeader(), semantic.HelmNone, nil, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assertJSONAt(t, buf.Bytes(), `"production-eu"`, "header", "context")
	assertJSONAt(t, buf.Bytes(), `12`, "header", "revision")
	validatePlanSchema(t, buf.Bytes())
}

func TestWriteJSON_SchemaAndTasks(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmNone, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assert.JSONEq(t, `{
		"schema": "https://deployah.dev/schemas/plan/v1/schema.json",
		"header": {
			"project": "web",
			"environment": "prod",
			"release": "web",
			"namespace": "prod"
		},
		"helmAction": "none",
		"changes": [],
		"drift": [],
		"tasks": [],
		"chartCRDs": [],
		"summary": {
			"create": 0,
			"update": 0,
			"delete": 0,
			"total": 0
		}
	}`, buf.String())
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	_, hasCompleteness := doc["completeness"]
	assert.False(t, hasCompleteness)
	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok)
	_, hasReplace := summary["replace"]
	assert.False(t, hasReplace)
	assertNoJSONKeysFromBytes(t, buf.Bytes())
	validatePlanSchema(t, buf.Bytes())
}

func TestWriteJSON_CreateChangeShape(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    snap(cm("app", "v1")),
	}})
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	change := jsonObjects(t, jsonObject(mustJSON(t, buf.Bytes()))["changes"])[0]
	assert.Equal(t, []string{"action", "after", "before", "fields", "resource"}, sortedKeys(change))
	assert.Equal(t, "create", change["action"])
	assert.Nil(t, change["before"])
	assert.NotNil(t, change["after"])
	assert.Empty(t, change["fields"])
	validatePlanSchema(t, buf.Bytes())
}

func TestWriteJSON_TasksContract(t *testing.T) {
	t.Parallel()
	p := mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
		Name:    "migrate",
		Phase:   semantic.TaskPreDeploy,
		Action:  semantic.TaskCreate,
		WillRun: true,
		Definitions: []semantic.HookDefinition{{
			Resource: ref("Job", "migrate"),
			Action:   semantic.Create,
			After:    snap(cm("migrate", "v1")),
		}},
	}})
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assert.JSONEq(t, `{
		"schema": "https://deployah.dev/schemas/plan/v1/schema.json",
		"header": {
			"project": "web",
			"environment": "prod",
			"release": "web",
			"namespace": "prod"
		},
		"helmAction": "upgrade",
		"changes": [],
		"drift": [],
		"tasks": [{
			"name": "migrate",
			"phase": "preDeploy",
			"action": "create",
			"willRun": true,
			"definitions": [{
				"resource": {"apiVersion": "v1", "kind": "Job", "namespace": "prod", "name": "migrate"},
				"action": "create",
				"before": null,
				"after": {
					"apiVersion": "v1",
					"kind": "ConfigMap",
					"metadata": {"name": "migrate", "namespace": "prod"},
					"data": {"key": "v1"}
				},
				"fields": []
			}],
			"resources": []
		}],
		"chartCRDs": [],
		"summary": {"create": 0, "update": 0, "delete": 0, "total": 0}
	}`, buf.String())
	validatePlanSchema(t, buf.Bytes())
	assertNoJSONKeysFromBytes(t, buf.Bytes())
}

func TestWriteJSON_ChartCRDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		header      semantic.Header
		action      semantic.HelmAction
		lifecycle   semantic.ChartCRDLifecycle
		willProcess bool
	}{
		{name: "fresh install", header: semantic.Header{Project: "web", FreshInstall: true}, action: semantic.HelmInstall, lifecycle: semantic.ChartCRDProcess, willProcess: true},
		{name: "fresh skip", header: semantic.Header{Project: "web", FreshInstall: true}, action: semantic.HelmInstall, lifecycle: semantic.ChartCRDSkip},
		{name: "upgrade", header: semantic.Header{Project: "web"}, action: semantic.HelmUpgrade, lifecycle: semantic.ChartCRDUpgrade},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlanWithHeader(t, tc.header, tc.action, nil, nil)
			var err error
			p, err = semantic.AttachChartCRDs(p, []semantic.ChartCRD{{
				Source:      ".deployah/crds/widgets.yaml",
				Kind:        "CustomResourceDefinition",
				Name:        "widgets.example.com",
				Lifecycle:   tc.lifecycle,
				WillProcess: tc.willProcess,
			}})
			require.NoError(t, err)
			var buf bytes.Buffer
			require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
			var doc map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
			assert.Equal(t, view.SchemaV1ID, doc["schema"])
			crds := jsonObjects(t, doc["chartCRDs"])
			require.Len(t, crds, 1)
			entry := crds[0]
			assert.Equal(t, "CustomResourceDefinition", entry["kind"])
			assert.Equal(t, "widgets.example.com", entry["name"])
			assert.Equal(t, ".deployah/crds/widgets.yaml", entry["source"])
			assert.Equal(t, tc.lifecycle.String(), entry["lifecycle"])
			assert.Equal(t, tc.willProcess, entry["willProcess"])
			assert.Equal(t, float64(0), entry["index"])
			for _, banned := range []string{"action", "origin", "before", "after", "apply", "fields", "namespace", "apiVersion"} {
				assert.NotContains(t, entry, banned)
			}
			assert.NotContains(t, buf.String(), "Helm install will process")
			validatePlanSchema(t, buf.Bytes())
			assertNoJSONKeysFromBytes(t, buf.Bytes())
		})
	}
}

func TestWriteJSON_Deterministic(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Update,
		Before:   snap(map[string]any{"z": "1", "a": "1"}),
		After:    snap(map[string]any{"a": "2", "z": "2"}),
	}})
	var b1, b2 bytes.Buffer
	require.NoError(t, view.WriteJSON(&b1, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&b2, p, view.Options{}))
	assert.Equal(t, b1.String(), b2.String())
	assertJSONGolden(t, "json_update", b1.String())
	assertNoJSONKeysFromBytes(t, b1.Bytes())
	validatePlanSchema(t, b1.Bytes())
}

func TestWriteJSON_CamelCasePropertyNames(t *testing.T) {
	t.Parallel()
	header := semantic.Header{
		Project:      "web",
		Environment:  "prod",
		Release:      "web",
		Namespace:    "prod",
		FreshInstall: true,
	}
	p := mustPlanWithHeader(t, header, semantic.HelmInstall, []semantic.ResourceChange{{
		Resource: semantic.ResourceRef{
			APIVersion:   "v1",
			Kind:         "ConfigMap",
			Namespace:    "prod",
			GenerateName: "app-",
		},
		Action: semantic.Create,
		After:  snap(cm("app", "v1")),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	raw := buf.Bytes()
	assertJSONAt(t, raw, `{
		"project": "web",
		"environment": "prod",
		"release": "web",
		"namespace": "prod",
		"freshInstall": true
	}`, "header")
	assertJSONAt(t, raw, `{
		"apiVersion": "v1",
		"kind": "ConfigMap",
		"namespace": "prod",
		"name": "",
		"generateName": "app-"
	}`, "changes", 0, "resource")
	change := jsonObjects(t, jsonObject(mustJSON(t, raw))["changes"])[0]
	assert.Equal(t, "create", change["action"])
	assert.Empty(t, change["fields"])
	assertNoJSONKeysFromBytes(t, raw)
	validatePlanSchema(t, raw)
}

func TestWriteJSON_InvalidZero(t *testing.T) {
	t.Parallel()
	err := view.WriteJSON(&bytes.Buffer{}, semantic.Plan{}, view.Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid helmAction")
}

func TestWriteJSON_ShowSecrets(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "old-tok")),
		After:    snap(secretObj("s", "new-pass", "new-tok")),
	}})
	tests := []struct {
		name       string
		opts       view.Options
		data       string
		stringData string
		fields     string
		omit       []string
	}{
		{
			name:       "redacted",
			opts:       view.Options{},
			data:       `{"token":"(redacted)"}`,
			stringData: `{"password":"(redacted)"}`,
			fields: `[
				{"path":"/data/token","op":"replace","before":"(redacted)","after":"(redacted)"},
				{"path":"/stringData/password","op":"replace","before":"(redacted)","after":"(redacted)"}
			]`,
			omit: []string{"old-pass", "new-pass", "old-tok", "new-tok"},
		},
		{
			name:       "shown",
			opts:       view.Options{ShowSecrets: true},
			data:       `{"token":"new-tok"}`,
			stringData: `{"password":"new-pass"}`,
			fields: `[
				{"path":"/data/token","op":"replace","before":"old-tok","after":"new-tok"},
				{"path":"/stringData/password","op":"replace","before":"old-pass","after":"new-pass"}
			]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, view.WriteJSON(&buf, p, tt.opts))
			raw := buf.Bytes()
			assertJSONAt(t, raw, tt.data, "changes", 0, "after", "data")
			assertJSONAt(t, raw, tt.stringData, "changes", 0, "after", "stringData")
			assertJSONAt(t, raw, tt.fields, "changes", 0, "fields")
			for _, omit := range tt.omit {
				assert.NotContains(t, buf.String(), omit)
			}
			validatePlanSchema(t, raw)
		})
	}
}

func TestWriteJSON_DoesNotMutatePlan(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "old-tok")),
		After:    snap(secretObj("s", "new-pass", "new-tok")),
	}})
	require.NoError(t, view.WriteJSON(&bytes.Buffer{}, p, view.Options{}))
	assert.Equal(t, "old-pass", objectString(t, p.Changes[0].Before.Object, "stringData", "password"))
}

func TestWriteJSON_ExplicitNullFields(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Update,
		Before: snap(map[string]any{
			"keep":  "x",
			"gone":  nil,
			"swap":  nil,
			"stays": "y",
		}),
		After: snap(map[string]any{
			"keep":  "x",
			"added": nil,
			"swap":  true,
			"stays": nil,
		}),
	}})
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assertJSONAt(t, buf.Bytes(), `[
		{"path":"/added","op":"add","after":null},
		{"path":"/gone","op":"remove","before":null},
		{"path":"/stays","op":"replace","before":"y","after":null},
		{"path":"/swap","op":"replace","before":null,"after":true}
	]`, "changes", 0, "fields")
	validatePlanSchema(t, buf.Bytes())
}

func TestWriteJSON_MatchesSchemaForRepresentativePlans(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	tests := []struct {
		name string
		plan semantic.Plan
	}{
		{name: "empty", plan: mustPlan(t, semantic.HelmNone, nil)},
		{name: "create", plan: mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
			Resource: res,
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
		}})},
		{name: "delete", plan: mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
			Resource: res,
			Action:   semantic.Delete,
			Before:   snap(cm("app", "v1")),
		}})},
		{name: "update", plan: mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
			Resource: res,
			Action:   semantic.Update,
			Before:   snap(cm("app", "v1")),
			After:    snap(cm("app", "v2")),
		}})},
		{name: "namespace create", plan: mustPlanWithHeader(t, semantic.Header{
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
		{name: "task create", plan: mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
			Name:    "migrate",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskCreate,
			WillRun: true,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("Job", "migrate"),
				Action:   semantic.Create,
				After:    snap(cm("migrate", "v1")),
			}},
		}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, view.WriteJSON(&buf, tt.plan, view.Options{}))
			validatePlanSchema(t, buf.Bytes())
			assertNoJSONKeysFromBytes(t, buf.Bytes())
		})
	}
}

func TestWriteJSON_SnapshotMayUseProtocolKeyNames(t *testing.T) {
	t.Parallel()
	obj := cm("app", "v1")
	obj["data"] = map[string]any{
		"executions":  "ok",
		"api_version": "1",
		"will_run":    "yes",
	}
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    snap(obj),
	}})
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assertJSONAt(t, buf.Bytes(), `{
		"executions": "ok",
		"api_version": "1",
		"will_run": "yes"
	}`, "changes", 0, "after", "data")
	assertNoJSONKeysFromBytes(t, buf.Bytes())
	validatePlanSchema(t, buf.Bytes())
}

func compilePlanSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(view.SchemaV1()))
	require.NoError(t, err)
	require.NoError(t, compiler.AddResource(view.SchemaV1ID, doc))
	sch, err := compiler.Compile(view.SchemaV1ID)
	require.NoError(t, err)
	return sch
}

func validatePlanSchema(t *testing.T, raw []byte) {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	require.NoError(t, compilePlanSchema(t).Validate(v))
}

func assertSchemaRejects(t *testing.T, raw []byte) {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	require.Error(t, compilePlanSchema(t).Validate(v))
}

func assertNoJSONKeysFromBytes(t *testing.T, raw []byte) {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	assertNoJSONKeys(t, doc)
	assert.Contains(t, doc, "helmAction")
	if header := jsonObject(doc["header"]); header != nil {
		assertNoJSONKeys(t, header)
	}
	if summary := jsonObject(doc["summary"]); summary != nil {
		assertNoJSONKeys(t, summary)
	}
	for _, item := range jsonObjects(t, doc["changes"]) {
		assertNoJSONChangeKeys(t, item)
	}
	for _, item := range jsonObjects(t, doc["tasks"]) {
		assertNoJSONTaskKeys(t, item)
	}
	for _, item := range jsonObjects(t, doc["chartCRDs"]) {
		assertNoJSONKeys(t, item)
	}
}

func mustJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func jsonObject(v any) map[string]any {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return obj
}

func jsonObjects(t *testing.T, v any) []map[string]any {
	t.Helper()
	if v == nil {
		return nil
	}
	raw, isArray := v.([]any)
	require.True(t, isArray)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		obj, isObject := item.(map[string]any)
		require.True(t, isObject)
		out = append(out, obj)
	}
	return out
}

func assertNoJSONChangeKeys(t *testing.T, change map[string]any) {
	t.Helper()
	assertNoJSONKeys(t, change)
	assertNoJSONResourceKeys(t, change["resource"])
	assertNoJSONFieldKeys(t, change["fields"])
}

func TestWriteJSON_DriftShapes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, sampleDriftPlan(t), view.Options{}))
	raw := buf.Bytes()
	validatePlanSchema(t, raw)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	_, hasChecked := doc["driftChecked"]
	assert.False(t, hasChecked)
	entries := jsonObjects(t, doc["drift"])
	require.Len(t, entries, 3)

	modified := entries[0]
	assert.Equal(t, "modified", modified["action"])
	assert.NotNil(t, modified["previous"])
	assert.NotNil(t, modified["live"])
	assert.NotEmpty(t, modified["fields"])

	unexpected := entries[1]
	assert.Equal(t, "unexpected", unexpected["action"])
	assert.Nil(t, unexpected["previous"])
	assert.NotNil(t, unexpected["live"])
	assert.Empty(t, unexpected["fields"])

	missing := entries[2]
	assert.Equal(t, "missing", missing["action"])
	assert.NotNil(t, missing["previous"])
	assert.Nil(t, missing["live"])
	assert.Empty(t, missing["fields"])
	assertNoJSONKeysFromBytes(t, raw)
}

func TestWriteJSON_DriftSecretRedaction(t *testing.T) {
	t.Parallel()
	p := secretDriftPlan(t)
	tests := []struct {
		name   string
		opts   view.Options
		has    []string
		omit   []string
		data   string
		before string
	}{
		{
			name:   "redacted",
			has:    []string{"(redacted)"},
			omit:   []string{"b2xk", "bmV3", "c2VjcmV0", "kubectl.kubernetes.io/last-applied-configuration"},
			data:   `{"password":"(redacted)"}`,
			before: `"(redacted)"`,
		},
		{
			name:   "shown",
			opts:   view.Options{ShowSecrets: true},
			has:    []string{"b2xk", "bmV3", "c2VjcmV0"},
			omit:   []string{"kubectl.kubernetes.io/last-applied-configuration"},
			data:   `{"password":"bmV3"}`,
			before: `"b2xk"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, view.WriteJSON(&buf, p, tt.opts))
			text := buf.String()
			for _, want := range tt.has {
				assert.Contains(t, text, want)
			}
			for _, omit := range tt.omit {
				assert.NotContains(t, text, omit)
			}
			assertJSONAt(t, buf.Bytes(), tt.data, "drift", 0, "live", "data")
			assertJSONAt(t, buf.Bytes(), tt.before, "drift", 0, "fields", 0, "before")
			validatePlanSchema(t, buf.Bytes())
			require.Equal(t, "b2xk", p.Drift[0].Fields[0].Before)
		})
	}
}

func secretDriftPlan(t *testing.T) semantic.Plan {
	t.Helper()
	p, err := semantic.AttachDrift(mustPlan(t, semantic.HelmNone, nil), []semantic.DriftChange{
		{
			Resource: ref("Secret", "db"),
			Action:   semantic.DriftModified,
			Previous: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]any{"name": "db", "namespace": "prod"},
				"stringData": map[string]any{"password": "old"},
			}),
			Live: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]any{"name": "db", "namespace": "prod"},
				"data":       map[string]any{"password": "bmV3"},
			}),
			Fields: []semantic.FieldChange{{
				Path:   "/data/password",
				Op:     semantic.FieldReplace,
				Before: "b2xk",
				After:  "bmV3",
			}},
		},
		{
			Resource: ref("Secret", "leaked"),
			Action:   semantic.DriftUnexpected,
			Live: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]any{"name": "leaked", "namespace": "prod"},
				"data":       map[string]any{"password": "c2VjcmV0"},
			}),
		},
	})
	require.NoError(t, err)
	return p
}

func assertNoJSONTaskKeys(t *testing.T, task map[string]any) {
	t.Helper()
	assertNoJSONKeys(t, task)
	for _, item := range jsonObjects(t, task["resources"]) {
		assertNoJSONKeys(t, item)
	}
	for _, def := range jsonObjects(t, task["definitions"]) {
		assertNoJSONKeys(t, def)
		assertNoJSONResourceKeys(t, def["resource"])
		assertNoJSONFieldKeys(t, def["fields"])
	}
}

func assertNoJSONResourceKeys(t *testing.T, raw any) {
	t.Helper()
	res, isObject := raw.(map[string]any)
	require.True(t, isObject)
	assertNoJSONKeys(t, res)
}

func assertNoJSONFieldKeys(t *testing.T, raw any) {
	t.Helper()
	for _, field := range jsonObjects(t, raw) {
		assertNoJSONKeys(t, field)
	}
}

func assertNoJSONKeys(t *testing.T, obj map[string]any) {
	t.Helper()
	require.NotNil(t, obj)
	for _, name := range []string{
		"fresh_install",
		"api_version",
		"generate_name",
		"field_manager",
		"force_conflicts",
		"will_run",
		"marker",
		"displayAction",
		"executions",
		"helm_action",
	} {
		assert.NotContains(t, obj, name)
	}
}
