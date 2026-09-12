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
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
)

func TestPredict_ObjectKeyIncludesVersion(t *testing.T) {
	t.Parallel()
	// Same name/namespace/kind, different API version is new-to-release.
	cluster := newFakeCluster()
	live := foreignConfigMap("app", "prod")
	live.SetAPIVersion("example.com/v2")
	cluster.store(live)
	previous := `apiVersion: example.com/v1
kind: ConfigMap
metadata:
  name: app
  namespace: prod
data:
  key: prev
`
	desired := `apiVersion: example.com/v2
kind: ConfigMap
metadata:
  name: app
  namespace: prod
data:
  key: next
`
	_, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationUpgrade,
		ReleaseName: "web",
		Namespace:   "prod",
		Previous:    previous,
		Desired:     desired,
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot be imported")
}

func TestPredict_GroupKindPruneIgnoresVersion(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	previous := `apiVersion: example.com/v1
kind: Widget
metadata:
  name: app
  namespace: prod
`
	desired := `apiVersion: example.com/v2
kind: Widget
metadata:
  name: app
  namespace: prod
`
	results, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationUpgrade,
		ReleaseName: "web",
		Namespace:   "prod",
		Previous:    previous,
		Desired:     desired,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, predict.ActionCreate, results[0].Action)
	assert.Equal(t, "v2", results[0].Identity.Version)
	assert.Empty(t, cluster.deletes)
}

func TestPredict_GenerateName(t *testing.T) {
	t.Parallel()

	t.Run("skips ownership get", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		desired := `apiVersion: v1
kind: ConfigMap
metadata:
  generateName: app-
  namespace: prod
data:
  key: next
`
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, predict.ActionCreate, results[0].Action)
		assert.Empty(t, cluster.gets)
		require.Len(t, cluster.applies, 1)
		assert.Equal(t, "app-", cluster.applies[0].Obj.GetGenerateName())
	})

	t.Run("name and generateName error", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		desired := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  generateName: app-
  namespace: prod
data:
  key: next
`
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "metadata.name and metadata.generateName cannot both be set")
	})
}

func TestPredict_Flatten(t *testing.T) {
	t.Parallel()

	t.Run("multi-document yaml", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		desired := configMapYAML("one", "prod", "a") + "---\n" + configMapYAML("two", "prod", "b")
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.NoError(t, err)
		require.Len(t, results, 2)
		_, ok := findResult(results, "one")
		assert.True(t, ok)
		_, ok = findResult(results, "two")
		assert.True(t, ok)
	})

	t.Run("list expands to items", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		desired := `apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: one
    namespace: prod
  data:
    key: a
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: two
    namespace: prod
  data:
    key: b
`
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.NoError(t, err)
		require.Len(t, results, 2)
		for _, r := range results {
			assert.NotEqual(t, "List", r.Identity.Kind)
			assert.Equal(t, predict.ActionCreate, r.Action)
		}
	})

	t.Run("list items own independently", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(foreignConfigMap("two", "prod"))
		desired := `apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: one
    namespace: prod
  data:
    key: a
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: two
    namespace: prod
  data:
    key: b
`
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot be imported")
	})

	t.Run("list items prune independently", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		live := ownedConfigMap("two", "prod", "web")
		cluster.store(live)
		previous := `apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: one
    namespace: prod
  data:
    key: a
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: two
    namespace: prod
  data:
    key: b
`
		desired := configMapYAML("one", "prod", "a")
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    previous,
			Desired:     desired,
		})
		require.NoError(t, err)
		pruned, ok := findResult(results, "two")
		require.True(t, ok)
		assert.Equal(t, predict.ActionDelete, pruned.Action)
		require.Len(t, cluster.deletes, 1)
		assert.Equal(t, "two", cluster.deletes[0].ID.Name)
	})

	t.Run("mapping error on flattened item", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
		cluster.mappingErr[gvk] = &apimeta.NoKindMatchError{GroupKind: gvk.GroupKind()}
		desired := `apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: one
    namespace: prod
- apiVersion: example.com/v1
  kind: Widget
  metadata:
    name: two
    namespace: prod
`
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.Error(t, err)
		assert.True(t, apimeta.IsNoMatchError(err), "Predict() mapping error = %v, want IsNoMatchError", err)
	})

	t.Run("defaults empty namespace", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		desired := configMapYAML("app", "", "next")
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired:     desired,
		})
		require.NoError(t, err)
		got, ok := findResult(results, "app")
		require.True(t, ok)
		assert.Equal(t, "prod", got.Identity.Namespace)
		require.Len(t, cluster.applies, 1)
		assert.Equal(t, "prod", cluster.applies[0].Namespace)
	})
}

func TestPredict_PrerequisiteErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing namespace not found", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.applyErr = apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, "missing")
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "missing",
			Desired:     configMapYAML("app", "missing", "next"),
		})
		require.Error(t, err)
		assert.True(t, apierrors.IsNotFound(err), "Predict() = %v, want IsNotFound", err)
	})

	t.Run("no match mapping", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
		cluster.mappingErr[gvk] = &apimeta.NoKindMatchError{GroupKind: gvk.GroupKind()}
		_, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationInstall,
			ReleaseName: "web",
			Namespace:   "prod",
			Desired: `apiVersion: example.com/v1
kind: Widget
metadata:
  name: app
  namespace: prod
`,
		})
		require.Error(t, err)
		assert.True(t, apimeta.IsNoMatchError(err), "Predict() = %v, want IsNoMatchError", err)
	})
}
