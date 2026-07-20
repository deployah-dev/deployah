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

package drift

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	sigsyaml "sigs.k8s.io/yaml"
)

const clientTestDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
`

// interceptApply installs a "patch" reactor that decodes the incoming apply
// body and returns it as-is as the "predicted" object without writing it
// back into the tracker, since [k8s.io/client-go/testing.ObjectTracker]
// (unlike a real API server) ignores metav1.PatchOptions.DryRun and would
// otherwise silently overwrite the seeded live object.
func interceptApply(client *dynamicfake.FakeDynamicClient, resource string) {
	client.PrependReactor("patch", resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(clienttesting.PatchActionImpl)
		if !ok {
			return false, nil, nil
		}
		if patchAction.GetPatchType() != types.ApplyPatchType {
			return false, nil, nil
		}
		obj := &unstructured.Unstructured{Object: map[string]any{}}
		if err := sigsyaml.Unmarshal(patchAction.GetPatch(), &obj.Object); err != nil {
			return true, nil, err
		}
		obj.SetName(patchAction.GetName())
		obj.SetNamespace(patchAction.GetNamespace())
		return true, obj, nil
	})
}

func newTestClient(t *testing.T, liveObjects ...runtime.Object) *Client {
	t.Helper()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), liveObjects...)
	interceptApply(fakeClient, "deployments")
	mapper := testrestmapper.TestOnlyStaticRESTMapper(clientgoscheme.Scheme)
	return newClient(fakeClient, mapper)
}

func unstructuredDeployment(name, namespace string, replicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"replicas": replicas,
		},
	}}
}

// TestClientPredict_ExistingResource_ReturnsPredictedAndLive verifies
// [Client.Predict] resolves the resource's GVK through the REST mapper,
// sends a server-side apply dry-run PATCH, and fetches the live object
// separately, returning both as YAML.
func TestClientPredict_ExistingResource_ReturnsPredictedAndLive(t *testing.T) {
	t.Parallel()
	live := unstructuredDeployment("web", "default", 5)
	c := newTestClient(t, live)

	predicted, liveYAML, err := c.Predict(context.Background(), clientTestDeployment)
	require.NoError(t, err)

	var predictedDoc, liveDoc map[string]any
	require.NoError(t, sigsyaml.Unmarshal([]byte(predicted), &predictedDoc))
	require.NoError(t, sigsyaml.Unmarshal([]byte(liveYAML), &liveDoc))

	predictedSpec, ok := predictedDoc["spec"].(map[string]any)
	require.True(t, ok)
	liveSpec, ok := liveDoc["spec"].(map[string]any)
	require.True(t, ok)
	assert.InEpsilon(t, float64(2), predictedSpec["replicas"], 0, "predicted must reflect the desired manifest, not the live object")
	assert.InEpsilon(t, float64(5), liveSpec["replicas"], 0, "live must reflect the pre-existing cluster object, unaffected by the dry-run patch")
}

// TestClientPredict_ResourceNotFound_ReturnsEmptyLive verifies a resource
// that does not exist yet returns live="" with no error, matching the
// [Predictor] contract that [ComputeDrift] relies on to skip resources
// with no baseline to compare against.
func TestClientPredict_ResourceNotFound_ReturnsEmptyLive(t *testing.T) {
	t.Parallel()
	c := newTestClient(t) // no live objects seeded

	predicted, liveYAML, err := c.Predict(context.Background(), clientTestDeployment)
	require.NoError(t, err)
	assert.Empty(t, liveYAML)
	assert.NotEmpty(t, predicted)
}

// TestClientPredict_GetError_PropagatesAsError verifies a failure other
// than "not found" while fetching the live object (e.g. an RBAC denial)
// surfaces as an error rather than being treated as a missing resource.
func TestClientPredict_GetError_PropagatesAsError(t *testing.T) {
	t.Parallel()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	interceptApply(fakeClient, "deployments")
	fakeClient.PrependReactor("get", "deployments", func(_ clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "apps", Resource: "deployments"}, "web", errors.New("cannot get resource"),
		)
	})
	mapper := testrestmapper.TestOnlyStaticRESTMapper(clientgoscheme.Scheme)
	c := newClient(fakeClient, mapper)

	_, _, err := c.Predict(context.Background(), clientTestDeployment)
	assert.Error(t, err)
}

// TestNewClient verifies [NewClient] builds a [Client] from a REST config
// without contacting a server (dynamic and discovery client construction
// is purely local), and that an invalid config surfaces as an error.
func TestNewClient(t *testing.T) {
	t.Parallel()

	t.Run("valid config builds a client", func(t *testing.T) {
		t.Parallel()

		c, err := NewClient(&rest.Config{Host: "https://example.invalid:6443"})
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.NotNil(t, c.dynamicClient)
		assert.NotNil(t, c.mapper)
	})

	t.Run("invalid config returns error", func(t *testing.T) {
		t.Parallel()

		// Username/password and a bearer token are mutually exclusive
		// auth methods; client-go rejects the config before ever dialing
		// a server.
		c, err := NewClient(&rest.Config{
			Host:        "https://example.invalid:6443",
			Username:    "user",
			BearerToken: "tok",
		})
		require.Error(t, err)
		assert.Nil(t, c)
		assert.Contains(t, err.Error(), "build dynamic client")
	})
}

// TestClientPredict_DecodeError verifies malformed resourceYAML surfaces a
// decode error instead of a panic or a silent no-op.
func TestClientPredict_DecodeError(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	_, _, err := c.Predict(context.Background(), "not: valid: yaml: [")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode resource")
}

// TestClientPredict_UnmappedKind_ReturnsError verifies a resource whose
// GroupVersionKind the REST mapper does not recognize surfaces a clear
// "resolve resource mapping" error rather than a bare mapper error.
func TestClientPredict_UnmappedKind_ReturnsError(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	unknownKindYAML := `
apiVersion: totally.unknown/v1
kind: FrobnicatorWidget
metadata:
  name: x
`
	_, _, err := c.Predict(context.Background(), unknownKindYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve resource mapping")
}

// TestToYAML_MarshalError verifies an object that cannot be marshaled to
// JSON (e.g. a channel value smuggled into the map) surfaces a wrapped
// error rather than a panic.
func TestToYAML_MarshalError(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{Object: map[string]any{
		"badField": make(chan int),
	}}
	s, err := toYAML(obj)
	require.Error(t, err)
	assert.Empty(t, s)
	assert.Contains(t, err.Error(), "encode resource to YAML")
}

// TestClientPredict_DryRunAndForceOptionsAreSet verifies every predict
// PATCH is sent with the field manager, dry-run, and force-ownership
// options: without Force, a field another controller (e.g. an HPA driving
// spec.replicas) already owns would make the dry-run fail with a conflict
// instead of returning a prediction.
func TestClientPredict_DryRunAndForceOptionsAreSet(t *testing.T) {
	t.Parallel()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), unstructuredDeployment("web", "default", 5))
	var captured *metav1.PatchOptions
	fakeClient.PrependReactor("patch", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(clienttesting.PatchActionImpl)
		if !ok {
			return false, nil, nil
		}
		opts := patchAction.GetPatchOptions()
		captured = &opts
		if patchAction.GetPatchType() != types.ApplyPatchType {
			return false, nil, nil
		}
		obj := &unstructured.Unstructured{Object: map[string]any{}}
		if err := sigsyaml.Unmarshal(patchAction.GetPatch(), &obj.Object); err != nil {
			return true, nil, err
		}
		obj.SetName(patchAction.GetName())
		obj.SetNamespace(patchAction.GetNamespace())
		return true, obj, nil
	})
	mapper := testrestmapper.TestOnlyStaticRESTMapper(clientgoscheme.Scheme)
	c := newClient(fakeClient, mapper)

	_, _, err := c.Predict(context.Background(), clientTestDeployment)
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, FieldManager, captured.FieldManager)
	assert.Equal(t, []string{metav1.DryRunAll}, captured.DryRun)
	require.NotNil(t, captured.Force)
	assert.True(t, *captured.Force)
}
