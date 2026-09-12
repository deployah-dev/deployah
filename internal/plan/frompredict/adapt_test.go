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

package frompredict_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/plan/frompredict"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"
)

func TestFromResults_ActionMapping(t *testing.T) {
	t.Parallel()
	header := semantic.Header{Release: "web", Namespace: "prod"}
	live := configMap("app", "prod", "old")
	predicted := configMap("app", "prod", "new")

	p, err := frompredict.FromResults(header, []predict.Result{
		{
			Identity:  id("app"),
			Action:    predict.ActionCreate,
			Predicted: predicted,
		},
		{
			Identity:  id("keep"),
			Action:    predict.ActionNoOp,
			Live:      configMap("keep", "prod", "same"),
			Predicted: configMap("keep", "prod", "same"),
		},
		{
			Identity:  id("app-up"),
			Action:    predict.ActionUpdate,
			Live:      configMap("app-up", "prod", "old"),
			Predicted: configMap("app-up", "prod", "new"),
		},
		{
			Identity: id("gone"),
			Action:   predict.ActionDelete,
			Live:     configMap("gone", "prod", "old"),
		},
	})
	require.NoError(t, err)
	require.Len(t, p.Changes, 3)
	assert.NotNil(t, p.Executions)
	assert.Empty(t, p.Executions)
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)

	byName := map[string]semantic.ResourceChange{}
	for _, c := range p.Changes {
		byName[c.Resource.Name] = c
		assert.NotEqual(t, semantic.Recreate, c.Action)
	}
	require.Contains(t, byName, "app")
	assert.Equal(t, semantic.Create, byName["app"].Action)
	assert.Nil(t, byName["app"].Before)
	require.NotNil(t, byName["app"].After)
	assert.NotNil(t, byName["app"].Apply.Write)
	assert.Equal(t, "deployah", byName["app"].Apply.Write.FieldManager)
	assert.False(t, byName["app"].Apply.Write.ForceConflicts)
	assert.Nil(t, byName["app"].Apply.Delete)

	require.Contains(t, byName, "app-up")
	assert.Equal(t, semantic.Update, byName["app-up"].Action)
	require.NotNil(t, byName["app-up"].Before)
	require.NotNil(t, byName["app-up"].After)
	assert.NotNil(t, byName["app-up"].Apply.Write)
	assert.Nil(t, byName["app-up"].Apply.Delete)

	require.Contains(t, byName, "gone")
	assert.Equal(t, semantic.Delete, byName["gone"].Action)
	require.NotNil(t, byName["gone"].Before)
	assert.Nil(t, byName["gone"].After)
	assert.Nil(t, byName["gone"].Apply.Write)
	require.NotNil(t, byName["gone"].Apply.Delete)
	assert.Equal(t, semantic.PropagationBackground, byName["gone"].Apply.Delete.Propagation)

	assert.Equal(t, "old", objectString(t, live.Object, "data", "key"))
	assert.Equal(t, "new", objectString(t, predicted.Object, "data", "key"))
}

func TestFromResults_NoOpOmitted(t *testing.T) {
	t.Parallel()
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  id("app"),
		Action:    predict.ActionNoOp,
		Live:      configMap("app", "prod", "same"),
		Predicted: configMap("app", "prod", "same"),
	}})
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
}

func TestFromResults_LimitationKeepsPredicted(t *testing.T) {
	t.Parallel()
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:   id("app"),
		Action:     predict.ActionUpdate,
		Live:       configMap("app", "prod", "old"),
		Predicted:  configMap("app", "prod", "new"),
		Limitation: predict.LimitationManagedFieldsMigration,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	require.NotNil(t, p.Changes[0].After)
	assert.Equal(t, "new", objectString(t, p.Changes[0].After.Object, "data", "key"))
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
	require.Len(t, p.Diagnostics, 1)
	assert.Equal(t, semantic.CategoryPredictionLimitation, p.Diagnostics[0].Category)
	assert.Contains(t, p.Diagnostics[0].Message, predict.LimitationManagedFieldsMigration)
}

func TestFromResults_LimitationDoesNotFabricateAfter(t *testing.T) {
	t.Parallel()
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:   id("app"),
		Action:     predict.ActionUpdate,
		Live:       configMap("app", "prod", "old"),
		Limitation: predict.LimitationManagedFieldsMigration,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Nil(t, p.Changes[0].After)
	assert.Empty(t, p.Changes[0].Fields)
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
}

func TestFromResults_UnknownAction(t *testing.T) {
	t.Parallel()
	_, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity: id("app"),
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid predict action")
}

func TestFromResults_CreateRequiresPredicted(t *testing.T) {
	t.Parallel()
	_, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity: id("app"),
		Action:   predict.ActionCreate,
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "create requires a predicted object")
}

func TestFromResults_DoesNotMutateResults(t *testing.T) {
	t.Parallel()
	live := configMap("app", "prod", "old")
	predicted := configMap("app", "prod", "new")
	results := []predict.Result{{
		Identity:  id("app"),
		Action:    predict.ActionUpdate,
		Live:      live,
		Predicted: predicted,
	}}
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, results)
	require.NoError(t, err)

	results[0].Action = predict.ActionDelete
	setObjectString(t, live.Object, "mutated", "data", "key")
	setObjectString(t, predicted.Object, "mutated", "data", "key")

	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	assert.Equal(t, "old", objectString(t, p.Changes[0].Before.Object, "data", "key"))
	assert.Equal(t, "new", objectString(t, p.Changes[0].After.Object, "data", "key"))
}

func TestFromResults_GoIntInObject(t *testing.T) {
	t.Parallel()
	predicted := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "web",
			"namespace": "prod",
		},
		"spec": map[string]any{"replicas": 3},
	}}
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predict.Identity{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "prod", Name: "web"},
		Action:    predict.ActionCreate,
		Predicted: predicted,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	require.NotNil(t, p.Changes[0].After)
	require.NotNil(t, p.Changes[0].Apply.Write)
	spec, ok := p.Changes[0].After.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, spec["replicas"])
	assert.Equal(t, "deployah", p.Changes[0].Apply.Write.FieldManager)

	callerSpec, ok := predicted.Object["spec"].(map[string]any)
	require.True(t, ok)
	callerSpec["replicas"] = 9
	spec, ok = p.Changes[0].After.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, spec["replicas"])
}

func TestFromResults_GenerateName(t *testing.T) {
	t.Parallel()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"generateName": "app-",
			"namespace":    "prod",
		},
		"data": map[string]any{"key": "v1"},
	}}
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predict.Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod"},
		Action:    predict.ActionCreate,
		Predicted: obj,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Empty(t, p.Changes[0].Resource.Name)
	assert.Equal(t, "app-", p.Changes[0].Resource.GenerateName)
}

func TestFromResults_NeverEmitsRecreate(t *testing.T) {
	t.Parallel()
	p, err := frompredict.FromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{
		{Identity: id("a"), Action: predict.ActionCreate, Predicted: configMap("a", "prod", "1")},
		{Identity: id("b"), Action: predict.ActionUpdate, Live: configMap("b", "prod", "1"), Predicted: configMap("b", "prod", "2")},
		{Identity: id("c"), Action: predict.ActionDelete, Live: configMap("c", "prod", "1")},
		{Identity: id("d"), Action: predict.ActionNoOp, Live: configMap("d", "prod", "1"), Predicted: configMap("d", "prod", "1")},
	})
	require.NoError(t, err)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.Recreate, c.Action)
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

func setObjectString(tb testing.TB, obj map[string]any, value string, keys ...string) {
	tb.Helper()
	require.NotEmpty(tb, keys)
	cur := obj
	for _, key := range keys[:len(keys)-1] {
		next, ok := cur[key].(map[string]any)
		require.True(tb, ok)
		cur = next
	}
	cur[keys[len(keys)-1]] = value
}

func id(name string) predict.Identity {
	return predict.Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: name}
}

func configMap(name, ns, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": ns,
		},
		"data": map[string]any{"key": value},
	}}
}
