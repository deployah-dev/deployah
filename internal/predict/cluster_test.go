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

package predict

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
)

func interceptApplyJSON(client *dynamicfake.FakeDynamicClient) {
	client.PrependReactor("patch", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(clienttesting.PatchActionImpl)
		if !ok {
			return false, nil, nil
		}
		if patchAction.GetPatchType() != types.ApplyPatchType {
			return false, nil, nil
		}
		obj := &unstructured.Unstructured{Object: map[string]any{}}
		if err := json.Unmarshal(patchAction.GetPatch(), &obj.Object); err != nil {
			return true, nil, err
		}
		obj.SetName(patchAction.GetName())
		obj.SetNamespace(patchAction.GetNamespace())
		return true, obj, nil
	})
}

func testRESTCluster(t *testing.T) (*restCluster, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	interceptApplyJSON(fakeClient)
	mapper := testrestmapper.TestOnlyStaticRESTMapper(clientgoscheme.Scheme)
	return newRESTCluster(fakeClient, mapper), fakeClient
}

func testConfigMap(name, namespace, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"data": map[string]any{"key": value},
	}}
}

func TestRESTCluster_ApplyRequestShape(t *testing.T) {
	t.Parallel()
	cluster, fakeClient := testRESTCluster(t)
	obj := testConfigMap("app", "prod", "next")

	got, err := cluster.Apply(t.Context(), obj)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "app", got.GetName())
	assert.Equal(t, "prod", got.GetNamespace())

	patch := lastPatch(t, fakeClient)
	assert.Equal(t, types.ApplyPatchType, patch.GetPatchType())
	assert.Equal(t, "app", patch.GetName())
	assert.Equal(t, "prod", patch.GetNamespace())
	require.True(t, json.Valid(patch.GetPatch()), "apply body must be JSON")

	opts := patch.GetPatchOptions()
	assert.Equal(t, []string{metav1.DryRunAll}, opts.DryRun)
	assert.Equal(t, kube.ManagedFieldsManager, opts.FieldManager)
	require.NotNil(t, opts.Force)
	assert.False(t, *opts.Force)
	assert.Equal(t, metav1.FieldValidationStrict, opts.FieldValidation)
}

func TestRESTCluster_JSONPatchRequestShape(t *testing.T) {
	t.Parallel()
	cluster, fakeClient := testRESTCluster(t)
	id := Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}
	patchBody := []byte(`[{"op":"replace","path":"/metadata/managedFields","value":[]}]`)
	require.NoError(t, fakeClient.Tracker().Add(testConfigMap("app", "prod", "live")))

	require.NoError(t, cluster.JSONPatch(t.Context(), id, patchBody))

	patch := lastPatch(t, fakeClient)
	assert.Equal(t, types.JSONPatchType, patch.GetPatchType())
	assert.Equal(t, patchBody, []byte(patch.GetPatch()))
	opts := patch.GetPatchOptions()
	assert.Equal(t, []string{metav1.DryRunAll}, opts.DryRun)
	assert.Equal(t, kube.ManagedFieldsManager, opts.FieldManager)
	assert.Nil(t, opts.Force)
	assert.Equal(t, metav1.FieldValidationStrict, opts.FieldValidation)
}

func TestRESTCluster_DeleteRequestShape(t *testing.T) {
	t.Parallel()
	cluster, fakeClient := testRESTCluster(t)
	id := Identity{Version: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}

	// Seed so the fake tracker can delete.
	require.NoError(t, fakeClient.Tracker().Add(testConfigMap("app", "prod", "live")))
	require.NoError(t, cluster.Delete(t.Context(), id))

	var del clienttesting.DeleteActionImpl
	found := false
	for _, action := range fakeClient.Actions() {
		if d, ok := action.(clienttesting.DeleteActionImpl); ok {
			del = d
			found = true
		}
	}
	require.True(t, found, "expected a delete action")
	assert.Equal(t, []string{metav1.DryRunAll}, del.DeleteOptions.DryRun)
	require.NotNil(t, del.DeleteOptions.PropagationPolicy)
	assert.Equal(t, metav1.DeletePropagationBackground, *del.DeleteOptions.PropagationPolicy)
	assert.Nil(t, del.DeleteOptions.Preconditions)
	assert.Nil(t, del.DeleteOptions.GracePeriodSeconds)
}

func lastPatch(t *testing.T, fakeClient *dynamicfake.FakeDynamicClient) clienttesting.PatchActionImpl {
	t.Helper()
	for _, action := range slices.Backward(fakeClient.Actions()) {
		if p, ok := action.(clienttesting.PatchActionImpl); ok {
			return p
		}
	}
	t.Fatal("expected a patch action")
	return clienttesting.PatchActionImpl{}
}
