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

package predict_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPredict_Actions(t *testing.T) {
	t.Parallel()

	t.Run("missing live is create", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionCreate, got.Action)
		assert.Nil(t, got.Live)
		require.NotNil(t, got.Predicted)
	})

	t.Run("changed live is update", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(ownedConfigMap("app", "prod", "web"))
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionUpdate, got.Action)
	})

	t.Run("identical normalized state is noop", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		require.NoError(t, unstructured.SetNestedField(live.Object, "next", "data", "key"))
		live.SetResourceVersion("9")
		live.SetUID("uid-1")
		live.SetManagedFields(deployahUpdateManagedFields())
		require.NoError(t, unstructured.SetNestedField(live.Object, map[string]any{"ready": true}, "status"))
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("app", "prod", "next"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionNoOp, got.Action)
		require.NotNil(t, got.Live)
		require.NotNil(t, got.Predicted)
	})

	t.Run("removed previous is delete", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(ownedConfigMap("old", "prod", "web"))
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("old", "prod", "prev"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "old")
		require.True(t, ok)
		assert.Equal(t, predict.ActionDelete, got.Action)
		assert.NotNil(t, got.Live)
		assert.Nil(t, got.Predicted)
	})

	t.Run("removed previous already gone is noop", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("old", "prod", "prev"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "old")
		require.True(t, ok)
		assert.Equal(t, predict.ActionNoOp, got.Action)
		assert.Nil(t, got.Live)
		assert.Nil(t, got.Predicted)
		assert.Empty(t, cluster.deletes)
	})
}

func TestPredict_Normalize(t *testing.T) {
	t.Parallel()

	t.Run("status only is noop", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		require.NoError(t, unstructured.SetNestedField(live.Object, "next", "data", "key"))
		require.NoError(t, unstructured.SetNestedField(live.Object, map[string]any{"phase": "Ready"}, "status"))
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("app", "prod", "next"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionNoOp, got.Action)
	})

	t.Run("resourceVersion uid managedFields only is noop", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		require.NoError(t, unstructured.SetNestedField(live.Object, "next", "data", "key"))
		live.SetResourceVersion("42")
		live.SetUID("abc")
		live.SetGeneration(7)
		live.SetCreationTimestamp(metav1.Now())
		live.SetManagedFields(deployahUpdateManagedFields())
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("app", "prod", "next"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionNoOp, got.Action)
	})

	t.Run("external annotation stays update", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		require.NoError(t, unstructured.SetNestedField(live.Object, "next", "data", "key"))
		ann := live.GetAnnotations()
		ann["deployah.dev/external"] = "keep-me"
		live.SetAnnotations(ann)
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("app", "prod", "next"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionUpdate, got.Action)
	})

	t.Run("does not mutate live or predicted", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		require.NoError(t, unstructured.SetNestedField(live.Object, "next", "data", "key"))
		live.SetResourceVersion("9")
		require.NoError(t, unstructured.SetNestedField(live.Object, map[string]any{"ready": true}, "status"))
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("app", "prod", "next"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, "9", got.Live.GetResourceVersion())
		_, found, nestErr := unstructured.NestedFieldNoCopy(got.Live.Object, "status")
		require.NoError(t, nestErr)
		assert.True(t, found)
		_, found, nestErr = unstructured.NestedFieldNoCopy(got.Predicted.Object, "status")
		require.NoError(t, nestErr)
		assert.False(t, found)
	})
}

func TestPredict_CreateOnlyResourceQuotaRetry(t *testing.T) {
	t.Parallel()

	t.Run("create retries quota conflict", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.applySeq = []error{resourceQuotaConflict(), nil}
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionCreate, got.Action)
		assert.GreaterOrEqual(t, cluster.applyCalls, 2)
	})

	t.Run("update does not retry quota conflict", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(ownedConfigMap("app", "prod", "web"))
		cluster.applyErr = resourceQuotaConflict()
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.Error(t, err)
		assert.True(t, apierrors.IsConflict(err), "Predict() = %v, want conflict", err)
		assert.Equal(t, 1, cluster.applyCalls)
	})

	t.Run("field manager conflict is an error", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.applyErr = fieldManagerConflict()
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.Error(t, err)
		assert.True(t, apierrors.IsConflict(err), "Predict() = %v, want conflict", err)
	})
}

func TestPredict_ManagedFieldsMigration(t *testing.T) {
	t.Parallel()

	seedAdopted := func() *unstructured.Unstructured {
		live := ownedConfigMap("app", "prod", "web")
		live.SetResourceVersion("7")
		live.SetManagedFields(deployahUpdateManagedFields())
		return live
	}

	t.Run("empty patch is exact ssa", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		live.SetManagedFields([]metav1.ManagedFieldsEntry{{
			Manager:    "deployah",
			Operation:  metav1.ManagedFieldsOperationApply,
			APIVersion: "v1",
			FieldsType: "FieldsV1",
			FieldsV1:   metav1.NewFieldsV1(`{"f:data":{"f:key":{}}}`),
		}})
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Empty(t, got.Limitation)
		assert.Empty(t, cluster.jsonPatches)
		assert.Equal(t, predict.ActionUpdate, got.Action)
	})

	t.Run("migration dry-run failure is an error", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(seedAdopted())
		cluster.jsonPatchErr = apierrors.NewForbidden(
			schema.GroupResource{Resource: "configmaps"}, "app", errors.New("denied"))
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to patch object to upgrade CSA field manager")
		assert.True(t, apierrors.IsForbidden(err), "Predict() = %v, want forbidden", err)
	})

	t.Run("migration success and ssa success keeps limitation", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(seedAdopted())
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.LimitationManagedFieldsMigration, got.Limitation)
		assert.Equal(t, predict.ActionUpdate, got.Action)
		require.NotNil(t, got.Predicted)
		require.NotEmpty(t, cluster.jsonPatches)
		assert.NotEmpty(t, cluster.jsonPatches[0].Patch)
	})

	t.Run("migration success and ssa failure is not a deploy error", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(seedAdopted())
		cluster.applyErr = fieldManagerConflict()
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.LimitationManagedFieldsMigration, got.Limitation)
		assert.Equal(t, predict.ActionUpdate, got.Action)
		assert.Nil(t, got.Predicted)
		assert.NotNil(t, got.Live)
	})

	t.Run("migration success skips noop", func(t *testing.T) {
		t.Parallel()
		live := seedAdopted()
		require.NoError(t, unstructured.SetNestedField(live.Object, "next", "data", "key"))
		cluster := newFakeCluster()
		cluster.store(live)
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionUpdate, got.Action)
		assert.Equal(t, predict.LimitationManagedFieldsMigration, got.Limitation)
	})

	t.Run("retry on conflict re-gets", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(seedAdopted())
		cluster.jsonPatchConflict = 1
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.LimitationManagedFieldsMigration, got.Limitation)
		assert.GreaterOrEqual(t, cluster.jsonPatchCalls, 2)
	})

	t.Run("upgrade does not migrate", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(seedAdopted())
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("other", "prod", "prev"),
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Empty(t, got.Limitation)
		assert.Empty(t, cluster.jsonPatches)
	})
}

func TestPredict_DesiredGetError(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	id := predict.Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}
	cluster.getErr[objectKey(id)] = apierrors.NewForbidden(
		schema.GroupResource{Resource: "configmaps"}, "app", errors.New("denied"))
	_, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   "prod",
		Desired:     configMapYAML("app", "prod", "next"),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "could not get information about the resource")
	assert.True(t, apierrors.IsForbidden(err), "Predict() desired GET = %v, want forbidden", err)
	assert.Empty(t, cluster.applies)
}

func TestPredict_EmptyManifests(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   "prod",
	})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestPredict_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	_, err := predict.Predict(t.Context(), cluster, predict.Input{
		ReleaseName: "web",
		Namespace:   "prod",
		Desired:     configMapYAML("app", "prod", "next"),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid helm operation")

	_, err = predict.Predict(t.Context(), cluster, predict.Input{
		Operation: helm.OperationInstall,
		Namespace: "prod",
		Desired:   configMapYAML("app", "prod", "next"),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "release name is required")

	_, err = predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Desired:     configMapYAML("app", "prod", "next"),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "release namespace is required")
}
