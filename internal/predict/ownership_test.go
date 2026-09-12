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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/predict"
)

func TestPredict_Ownership(t *testing.T) {
	t.Parallel()

	desired := configMapYAML("app", "prod", "next")

	t.Run("correctly owned install adoption", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(ownedConfigMap("app", "prod", "web"))
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionUpdate, got.Action)
		assert.Empty(t, got.Limitation)
	})

	t.Run("missing managed-by", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		require.NoError(t, unstructured.SetNestedField(live.Object, map[string]any{}, "metadata", "labels"))
		cluster := newFakeCluster()
		cluster.store(live)
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot be imported")
		assert.ErrorContains(t, err, "app.kubernetes.io/managed-by")
	})

	t.Run("wrong managed-by", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		live.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "Helmfile"})
		cluster := newFakeCluster()
		cluster.store(live)
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot be imported")
		assert.ErrorContains(t, err, "app.kubernetes.io/managed-by")
	})

	t.Run("wrong release name", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(ownedConfigMap("app", "prod", "other"))
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "meta.helm.sh/release-name")
	})

	t.Run("wrong release namespace", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("app", "prod", "web")
		ann := live.GetAnnotations()
		ann["meta.helm.sh/release-namespace"] = "other"
		live.SetAnnotations(ann)
		cluster := newFakeCluster()
		cluster.store(live)
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "meta.helm.sh/release-namespace")
	})

	t.Run("foreign existing resource on install", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(foreignConfigMap("app", "prod"))
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot be imported")
	})

	t.Run("foreign new resource on upgrade", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(foreignConfigMap("app", "prod"))
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("old", "prod", "prev"),
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot be imported")
	})

	t.Run("already in previous skips ownership", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(foreignConfigMap("app", "prod"))
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    configMapYAML("app", "prod", "prev"),
			Desired:     desired,
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, predict.ActionUpdate, got.Action)
	})
}

func TestPredict_MetadataStamp(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	desired := configMapYAMLWith("app", "prod", `  labels:
    app.kubernetes.io/managed-by: Other
    app: web
  annotations:
    meta.helm.sh/release-name: stale
    owner: team
`, "next")
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationInstall,
		ReleaseName: "web",
		Namespace:   "prod",
		Desired:     desired,
	})
	require.NoError(t, err)
	got, ok := findResult(results, "app")
	require.True(t, ok)
	require.NotNil(t, got.Predicted)
	assert.Equal(t, "Helm", got.Predicted.GetLabels()["app.kubernetes.io/managed-by"])
	assert.Equal(t, "web", got.Predicted.GetLabels()["app"])
	assert.Equal(t, "web", got.Predicted.GetAnnotations()["meta.helm.sh/release-name"])
	assert.Equal(t, "prod", got.Predicted.GetAnnotations()["meta.helm.sh/release-namespace"])
	assert.Equal(t, "team", got.Predicted.GetAnnotations()["owner"])
	require.Len(t, cluster.applies, 1)
	applied := cluster.applies[0].Obj
	assert.Equal(t, "Helm", applied.GetLabels()["app.kubernetes.io/managed-by"])
	assert.Equal(t, "web", applied.GetAnnotations()["meta.helm.sh/release-name"])
}
