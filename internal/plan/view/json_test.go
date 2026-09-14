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

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestWriteJSON_SchemaAndTasks(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, nil, nil)
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
		"changes": [],
		"tasks": [],
		"diagnostics": [],
		"summary": {
			"create": 0,
			"update": 0,
			"delete": 0,
			"replace": 0,
			"total": 0
		},
		"completeness": "complete"
	}`, buf.String())
	assertNoJSONKeysFromBytes(t, buf.Bytes())
	validatePlanSchema(t, buf.Bytes())
}

func TestWriteJSON_TasksContract(t *testing.T) {
	t.Parallel()
	p := mustPlanWithTasks(t, nil, []semantic.TaskPlan{{
		Name:    "migrate",
		Phase:   semantic.TaskPreDeploy,
		Action:  semantic.TaskCreate,
		WillRun: true,
		Definitions: []semantic.HookDefinition{{
			Resource: ref("Job", "migrate"),
			Action:   semantic.Create,
			After:    snap(cm("migrate", "v1")),
		}},
	}}, nil)
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
		"changes": [],
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
		"diagnostics": [],
		"summary": {"create": 0, "update": 0, "delete": 0, "replace": 0, "total": 0},
		"completeness": "complete"
	}`, buf.String())
	validatePlanSchema(t, buf.Bytes())
	assertNoJSONKeysFromBytes(t, buf.Bytes())
}

func TestWriteJSON_Deterministic(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(map[string]any{"z": "1", "a": "1"}),
		After:    snap(map[string]any{"a": "2", "z": "2"}),
		Apply:    writeApply(),
	}}, nil)
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
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: semantic.ResourceRef{
			APIVersion:   "v1",
			Kind:         "ConfigMap",
			Namespace:    "prod",
			GenerateName: "app-",
		},
		Origin: helmOrigin(),
		Action: semantic.Create,
		After:  snap(cm("app", "v1")),
		Apply:  writeApply(),
	}}, nil)
	p.Header.FreshInstall = true
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
	assertJSONAt(t, raw, `{
		"method": "server_side_apply",
		"fieldManager": "deployah",
		"forceConflicts": false
	}`, "changes", 0, "apply", "write")
	assertNoJSONKeysFromBytes(t, raw)
	validatePlanSchema(t, raw)
}

func TestWriteJSON_InvalidZero(t *testing.T) {
	t.Parallel()
	err := view.WriteJSON(&bytes.Buffer{}, semantic.Plan{}, view.Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid completeness")
}

func TestWriteJSON_ShowSecrets(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "old-tok")),
		After:    snap(secretObj("s", "new-pass", "new-tok")),
		Apply:    writeApply(),
	}}, nil)
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
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "old-tok")),
		After:    snap(secretObj("s", "new-pass", "new-tok")),
		Apply:    writeApply(),
	}}, nil)
	require.NoError(t, view.WriteJSON(&bytes.Buffer{}, p, view.Options{}))
	assert.Equal(t, "old-pass", objectString(t, p.Changes[0].Before.Object, "stringData", "password"))
}

func TestWriteJSON_ExplicitNullFields(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
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
		Apply: writeApply(),
	}}, nil)
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
		{name: "empty", plan: mustPlan(t, nil, nil)},
		{name: "create", plan: mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
			Apply:    writeApply(),
		}}, nil)},
		{name: "delete", plan: mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Delete,
			Before:   snap(cm("app", "v1")),
			Apply:    deleteApply(),
		}}, nil)},
		{name: "partial update", plan: mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before:   snap(cm("app", "v1")),
			Apply:    writeApply(),
		}}, []semantic.Diagnostic{limitationFor(res)})},
		{name: "replace", plan: mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Replace,
			Before:   snap(cm("app", "v1")),
			After:    snap(cm("app", "v2")),
			Apply:    bothApply(),
		}}, nil)},
		{name: "task create", plan: mustPlanWithTasks(t, nil, []semantic.TaskPlan{{
			Name:    "migrate",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskCreate,
			WillRun: true,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("Job", "migrate"),
				Action:   semantic.Create,
				After:    snap(cm("migrate", "v1")),
			}},
		}}, nil)},
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
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(obj),
		Apply:    writeApply(),
	}}, nil)
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

func limitationFor(res semantic.ResourceRef) semantic.Diagnostic {
	return semantic.Diagnostic{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: managed-fields-migration",
		Resource: &res,
	}
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
	for _, item := range jsonObjects(t, doc["diagnostics"]) {
		assertNoJSONDiagKeys(t, item)
	}
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
	if origin := jsonObject(change["origin"]); origin != nil {
		assertNoJSONKeys(t, origin)
		if helm := jsonObject(origin["helm"]); helm != nil {
			assertNoJSONKeys(t, helm)
		}
	}
	if apply := jsonObject(change["apply"]); apply != nil {
		assertNoJSONKeys(t, apply)
		if write := jsonObject(apply["write"]); write != nil {
			assertNoJSONKeys(t, write)
		}
		if del := jsonObject(apply["delete"]); del != nil {
			assertNoJSONKeys(t, del)
		}
	}
	assertNoJSONFieldKeys(t, change["fields"])
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

func assertNoJSONDiagKeys(t *testing.T, diag map[string]any) {
	t.Helper()
	assertNoJSONKeys(t, diag)
	if res := diag["resource"]; res != nil {
		assertNoJSONResourceKeys(t, res)
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
		"helmAction",
	} {
		assert.NotContains(t, obj, name)
	}
}
