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

package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"

	// Pin Helm's kube.ManagedFieldsManager to "deployah".
	_ "deployah.dev/deployah/internal/helm"
)

// semanticPlanFromResults maps predict results onto a semantic plan for
// tests. HelmAction is fixed from the header: install when FreshInstall
// is set, otherwise upgrade. It is not derived from the results.
func semanticPlanFromResults(header semantic.Header, results []predict.Result) (semantic.Plan, error) {
	origin := semantic.ResourceOrigin{
		Kind: semantic.OriginHelm,
		Helm: &semantic.HelmOrigin{
			Release:   header.Release,
			Namespace: header.Namespace,
		},
	}
	changes, diags, err := mapPredictResults(origin, results)
	if err != nil {
		return semantic.Plan{}, err
	}
	if orderErr := stampHelmApplyOrder(changes); orderErr != nil {
		return semantic.Plan{}, orderErr
	}
	helmAction := semantic.HelmUpgrade
	if header.FreshInstall {
		helmAction = semantic.HelmInstall
	}
	return semantic.New(header, helmAction, changes, nil, diags)
}

func TestSemanticPlanFromResults_ActionMapping(t *testing.T) {
	t.Parallel()
	header := semantic.Header{Release: "web", Namespace: "prod"}
	live := predictionConfigMap("app", "prod", "old")
	predicted := predictionConfigMap("app", "prod", "new")

	p, err := semanticPlanFromResults(header, []predict.Result{
		{
			Identity:  predictionIdentity("app"),
			Action:    predict.ActionCreate,
			Predicted: predicted,
		},
		{
			Identity:  predictionIdentity("keep"),
			Action:    predict.ActionNoOp,
			Live:      predictionConfigMap("keep", "prod", "same"),
			Predicted: predictionConfigMap("keep", "prod", "same"),
		},
		{
			Identity:  predictionIdentity("app-up"),
			Action:    predict.ActionUpdate,
			Live:      predictionConfigMap("app-up", "prod", "old"),
			Predicted: predictionConfigMap("app-up", "prod", "new"),
		},
		{
			Identity: predictionIdentity("gone"),
			Action:   predict.ActionDelete,
			Live:     predictionConfigMap("gone", "prod", "old"),
		},
	})
	require.NoError(t, err)
	require.Len(t, p.Changes, 3)
	assert.NotNil(t, p.Tasks)
	assert.Empty(t, p.Tasks)

	byName := map[string]semantic.ResourceChange{}
	for _, c := range p.Changes {
		byName[c.Resource.Name] = c
	}
	require.Contains(t, byName, "app")
	assert.Equal(t, semantic.Create, byName["app"].Action)
	assert.Nil(t, byName["app"].Before)
	require.NotNil(t, byName["app"].After)
	assert.NotNil(t, byName["app"].Apply.Write)
	assert.Equal(t, kube.ManagedFieldsManager, byName["app"].Apply.Write.FieldManager)
	assert.NotEmpty(t, byName["app"].Apply.Write.FieldManager)
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

	assert.Equal(t, "old", predictionObjectString(t, live.Object, "data", "key"))
	assert.Equal(t, "new", predictionObjectString(t, predicted.Object, "data", "key"))
}

func TestSemanticPlanFromResults_NoOpOmitted(t *testing.T) {
	t.Parallel()
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predictionIdentity("app"),
		Action:    predict.ActionNoOp,
		Live:      predictionConfigMap("app", "prod", "same"),
		Predicted: predictionConfigMap("app", "prod", "same"),
	}})
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
}

func TestSemanticPlanFromResults_LimitationKeepsPredicted(t *testing.T) {
	t.Parallel()
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:   predictionIdentity("app"),
		Action:     predict.ActionUpdate,
		Live:       predictionConfigMap("app", "prod", "old"),
		Predicted:  predictionConfigMap("app", "prod", "new"),
		Limitation: predict.LimitationManagedFieldsMigration,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	require.NotNil(t, p.Changes[0].After)
	assert.Equal(t, "new", predictionObjectString(t, p.Changes[0].After.Object, "data", "key"))
	require.Len(t, p.Diagnostics, 1)
	assert.Equal(t, semantic.CategoryPredictionLimitation, p.Diagnostics[0].Category)
	assert.Contains(t, p.Diagnostics[0].Message, predict.LimitationManagedFieldsMigration)
}

func TestSemanticPlanFromResults_LimitationDoesNotFabricateAfter(t *testing.T) {
	t.Parallel()
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:   predictionIdentity("app"),
		Action:     predict.ActionUpdate,
		Live:       predictionConfigMap("app", "prod", "old"),
		Limitation: predict.LimitationManagedFieldsMigration,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Nil(t, p.Changes[0].After)
	assert.Empty(t, p.Changes[0].Fields)
	require.Len(t, p.Diagnostics, 1)
	assert.Equal(t, semantic.CategoryPredictionLimitation, p.Diagnostics[0].Category)
}

func TestSemanticPlanFromResults_UnknownAction(t *testing.T) {
	t.Parallel()
	_, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity: predictionIdentity("app"),
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid predict action")
}

func TestSemanticPlanFromResults_CreateRequiresPredicted(t *testing.T) {
	t.Parallel()
	_, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity: predictionIdentity("app"),
		Action:   predict.ActionCreate,
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "create requires a predicted object")
}

func TestSemanticPlanFromResults_DoesNotMutateResults(t *testing.T) {
	t.Parallel()
	live := predictionConfigMap("app", "prod", "old")
	predicted := predictionConfigMap("app", "prod", "new")
	results := []predict.Result{{
		Identity:  predictionIdentity("app"),
		Action:    predict.ActionUpdate,
		Live:      live,
		Predicted: predicted,
	}}
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, results)
	require.NoError(t, err)

	results[0].Action = predict.ActionDelete
	setPredictionObjectString(t, live.Object, "mutated", "data", "key")
	setPredictionObjectString(t, predicted.Object, "mutated", "data", "key")

	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	assert.Equal(t, "old", predictionObjectString(t, p.Changes[0].Before.Object, "data", "key"))
	assert.Equal(t, "new", predictionObjectString(t, p.Changes[0].After.Object, "data", "key"))
}

func TestSemanticPlanFromResults_GoIntInObject(t *testing.T) {
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
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predict.Identity{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "prod", Name: "web"},
		Action:    predict.ActionCreate,
		Predicted: predicted,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	require.NotNil(t, p.Changes[0].After)
	require.NotNil(t, p.Changes[0].Apply.Write)
	assert.Equal(t, "apps/v1", p.Changes[0].Resource.APIVersion)
	spec, ok := p.Changes[0].After.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, spec["replicas"])
	assert.Equal(t, kube.ManagedFieldsManager, p.Changes[0].Apply.Write.FieldManager)
	assert.NotEmpty(t, p.Changes[0].Apply.Write.FieldManager)

	callerSpec, ok := predicted.Object["spec"].(map[string]any)
	require.True(t, ok)
	callerSpec["replicas"] = 9
	spec, ok = p.Changes[0].After.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, spec["replicas"])
}

func TestSemanticPlanFromResults_GenerateName(t *testing.T) {
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
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predict.Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod"},
		Action:    predict.ActionCreate,
		Predicted: obj,
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Empty(t, p.Changes[0].Resource.Name)
	assert.Equal(t, "app-", p.Changes[0].Resource.GenerateName)
}

func TestSemanticPlanFromResults_NoOpWithLimitation(t *testing.T) {
	t.Parallel()
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:   predictionIdentity("app"),
		Action:     predict.ActionNoOp,
		Predicted:  predictionConfigMap("app", "prod", "same"),
		Limitation: predict.LimitationManagedFieldsMigration,
	}})
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	require.Len(t, p.Diagnostics, 1)
	assert.Equal(t, semantic.CategoryPredictionLimitation, p.Diagnostics[0].Category)
	assert.Contains(t, p.Diagnostics[0].Message, predict.LimitationManagedFieldsMigration)
	assert.Equal(t, "app", p.Diagnostics[0].Resource.Name)
}

func TestSemanticPlanFromResults_UpdateRequiresLive(t *testing.T) {
	t.Parallel()
	_, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predictionIdentity("app"),
		Action:    predict.ActionUpdate,
		Predicted: predictionConfigMap("app", "prod", "new"),
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "update requires a live object")
}

func TestSemanticPlanFromResults_DeleteRequiresLive(t *testing.T) {
	t.Parallel()
	_, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity: predictionIdentity("app"),
		Action:   predict.ActionDelete,
	}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "delete requires a live object")
}

func TestSemanticPlanFromResults_NilObjectSnapshot(t *testing.T) {
	t.Parallel()
	p, err := semanticPlanFromResults(semantic.Header{Release: "web", Namespace: "prod"}, []predict.Result{{
		Identity:  predictionIdentity("app"),
		Action:    predict.ActionCreate,
		Predicted: &unstructured.Unstructured{},
	}})
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	require.NotNil(t, p.Changes[0].After)
	assert.Nil(t, p.Changes[0].After.Object)
}

func predictionObjectString(tb testing.TB, obj map[string]any, keys ...string) string {
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

func setPredictionObjectString(tb testing.TB, obj map[string]any, value string, keys ...string) {
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

func predictionIdentity(name string) predict.Identity {
	return predict.Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: name}
}

func predictionConfigMap(name, ns, value string) *unstructured.Unstructured {
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
