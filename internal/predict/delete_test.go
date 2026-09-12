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
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestPredict_KeepPolicy(t *testing.T) {
	t.Parallel()

	t.Run("keep on live snapshots predicted", func(t *testing.T) {
		t.Parallel()
		live := ownedConfigMap("old", "prod", "web")
		ann := live.GetAnnotations()
		ann[kube.ResourcePolicyAnno] = kube.KeepPolicy
		live.SetAnnotations(ann)
		cluster := newFakeCluster()
		cluster.store(live)
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
		require.NotNil(t, got.Live)
		require.NotNil(t, got.Predicted)
		assert.NotSame(t, got.Live, got.Predicted)
		assert.Equal(t, got.Live.GetAnnotations()[kube.ResourcePolicyAnno], kube.KeepPolicy)
		assert.Equal(t, got.Predicted.GetAnnotations()[kube.ResourcePolicyAnno], kube.KeepPolicy)
		assert.Empty(t, cluster.deletes)
	})

	t.Run("keep only on previous does not skip", func(t *testing.T) {
		t.Parallel()
		cluster := newFakeCluster()
		cluster.store(ownedConfigMap("old", "prod", "web"))
		previous := configMapYAMLWith("old", "prod", `  annotations:
    helm.sh/resource-policy: keep
`, "prev")
		results, err := predict.Predict(t.Context(), cluster, predict.Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: "web",
			Namespace:   "prod",
			Previous:    previous,
			Desired:     configMapYAML("app", "prod", "next"),
		})
		require.NoError(t, err)
		got, ok := findResult(results, "old")
		require.True(t, ok)
		assert.Equal(t, predict.ActionDelete, got.Action)
		require.Len(t, cluster.deletes, 1)
	})
}

func TestPredict_PruneGetErrorFailsClosed(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	id := predict.Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "old"}
	cluster.getErr[objectKey(id)] = apierrors.NewForbidden(
		schema.GroupResource{Resource: "configmaps"}, "old", errors.New("denied"))
	_, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationUpgrade,
		ReleaseName: "web",
		Namespace:   "prod",
		Previous:    configMapYAML("old", "prod", "prev"),
		Desired:     configMapYAML("app", "prod", "next"),
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "could not get information about the resource")
	assert.True(t, apierrors.IsForbidden(err), "Predict() prune GET = %v, want forbidden", err)
	assert.Empty(t, cluster.deletes)
}

func TestPredict_DeleteDryRunFailure(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("old", "prod", "web"))
	cluster.deleteErr = apierrors.NewForbidden(
		schema.GroupResource{Resource: "configmaps"}, "old", errors.New("denied"))
	_, err := predict.Predict(t.Context(), cluster, predict.Input{
		Operation:   helm.OperationUpgrade,
		ReleaseName: "web",
		Namespace:   "prod",
		Previous:    configMapYAML("old", "prod", "prev"),
		Desired:     configMapYAML("app", "prod", "next"),
	})
	require.Error(t, err)
	assert.True(t, apierrors.IsForbidden(err), "Predict() delete = %v, want forbidden", err)
}
