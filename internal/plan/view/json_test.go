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

func TestWriteJSON_SchemaAndExecutions(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, nil, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
	assert.True(t, json.Valid(buf.Bytes()))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	assert.Equal(t, "deployah.semantic_plan.v1", doc["schema"])
	assert.Equal(t, "complete", doc["completeness"])
	execs, ok := doc["executions"].([]any)
	require.True(t, ok)
	assert.Empty(t, execs)
	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(0), summary["create"])
	assert.Equal(t, float64(0), summary["total"])
	assert.NotContains(t, doc, "Object")
	assert.NotContains(t, doc, "TypeMeta")
	assertNoSnakeCaseKeys(t, doc)
	validatePlanSchema(t, buf.Bytes())
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
	assertGolden(t, "json_update", b1.String())
	assertNoSnakeCaseKeysFromBytes(t, b1.Bytes())
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
	text := buf.String()
	assert.Contains(t, text, `"freshInstall"`)
	assert.Contains(t, text, `"apiVersion"`)
	assert.Contains(t, text, `"generateName"`)
	assert.Contains(t, text, `"fieldManager"`)
	assert.Contains(t, text, `"forceConflicts"`)
	assert.Contains(t, text, `"method": "server_side_apply"`)
	assert.NotContains(t, text, `"fresh_install"`)
	assert.NotContains(t, text, `"api_version"`)
	assert.NotContains(t, text, `"generate_name"`)
	assert.NotContains(t, text, `"field_manager"`)
	assert.NotContains(t, text, `"force_conflicts"`)
	validatePlanSchema(t, buf.Bytes())
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

	var hidden, shown bytes.Buffer
	require.NoError(t, view.WriteJSON(&hidden, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&shown, p, view.Options{ShowSecrets: true}))

	assert.NotContains(t, hidden.String(), "old-pass")
	assert.NotContains(t, hidden.String(), "new-pass")
	assert.Contains(t, hidden.String(), "(redacted)")
	assert.Contains(t, hidden.String(), `"/stringData/password"`)
	assert.Contains(t, shown.String(), "old-pass")
	assert.Contains(t, shown.String(), "new-pass")
	assert.Contains(t, hidden.String(), `"name"`)
	assert.Contains(t, hidden.String(), `"s"`)
	validatePlanSchema(t, hidden.Bytes())
	validatePlanSchema(t, shown.Bytes())
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
	var doc map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &doc))
	changes, ok := doc["changes"].([]any)
	require.True(t, ok)
	require.Len(t, changes, 1)
	change, ok := changes[0].(map[string]any)
	require.True(t, ok)
	fields, ok := change["fields"].([]any)
	require.True(t, ok)

	byPath := map[string]map[string]any{}
	for _, raw := range fields {
		f, isField := raw.(map[string]any)
		require.True(t, isField)
		path, isPath := f["path"].(string)
		require.True(t, isPath)
		byPath[path] = f
	}

	require.Contains(t, byPath, "/added")
	_, hasBefore := byPath["/added"]["before"]
	assert.False(t, hasBefore)
	assert.Contains(t, byPath["/added"], "after")
	assert.Nil(t, byPath["/added"]["after"])
	assert.Equal(t, "add", byPath["/added"]["op"])

	require.Contains(t, byPath, "/gone")
	_, hasAfter := byPath["/gone"]["after"]
	assert.False(t, hasAfter)
	assert.Contains(t, byPath["/gone"], "before")
	assert.Nil(t, byPath["/gone"]["before"])
	assert.Equal(t, "remove", byPath["/gone"]["op"])

	require.Contains(t, byPath, "/stays")
	assert.Equal(t, "y", byPath["/stays"]["before"])
	assert.Nil(t, byPath["/stays"]["after"])
	assert.Equal(t, "replace", byPath["/stays"]["op"])

	require.Contains(t, byPath, "/swap")
	assert.Nil(t, byPath["/swap"]["before"])
	assert.Equal(t, true, byPath["/swap"]["after"])
	assert.Equal(t, "replace", byPath["/swap"]["op"])

	validatePlanSchema(t, buf.Bytes())
}

func TestWriteJSON_MatchesSchemaForRepresentativePlans(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	plans := []semantic.Plan{
		mustPlan(t, nil, nil),
		mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
			Apply:    writeApply(),
		}}, nil),
		mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Delete,
			Before:   snap(cm("app", "v1")),
			Apply:    deleteApply(),
		}}, nil),
		mustPlan(t, []semantic.ResourceChange{{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before:   snap(cm("app", "v1")),
			Apply:    writeApply(),
		}}, []semantic.Diagnostic{limitationFor(res)}),
	}
	for _, p := range plans {
		var buf bytes.Buffer
		require.NoError(t, view.WriteJSON(&buf, p, view.Options{}))
		validatePlanSchema(t, buf.Bytes())
	}
}

func limitationFor(res semantic.ResourceRef) semantic.Diagnostic {
	return semantic.Diagnostic{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: managed-fields-migration",
		Resource: &res,
	}
}

func validatePlanSchema(t *testing.T, raw []byte) {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(view.SchemaV1()))
	require.NoError(t, err)
	require.NoError(t, compiler.AddResource(view.SchemaV1ID, doc))
	sch, err := compiler.Compile(view.SchemaV1ID)
	require.NoError(t, err)
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	require.NoError(t, sch.Validate(v))
}

func assertNoSnakeCaseKeysFromBytes(t *testing.T, raw []byte) {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	assertNoSnakeCaseKeys(t, doc)
}

func assertNoSnakeCaseKeys(t *testing.T, v any) {
	t.Helper()
	forbidden := []string{"fresh_install", "api_version", "generate_name", "field_manager", "force_conflicts"}
	var walk func(any)
	walk = func(cur any) {
		switch x := cur.(type) {
		case map[string]any:
			for key, child := range x {
				for _, name := range forbidden {
					assert.NotEqual(t, name, key)
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(v)
}
