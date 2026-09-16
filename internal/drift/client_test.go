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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

func newTestClient(t *testing.T, liveObjects ...runtime.Object) *Client {
	t.Helper()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), liveObjects...)
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

func TestClientLive_ExistingResource(t *testing.T) {
	t.Parallel()
	live := unstructuredDeployment("web", "default", 5)
	c := newTestClient(t, live)

	liveYAML, err := c.Live(t.Context(), clientTestDeployment)
	require.NoError(t, err)

	var liveDoc map[string]any
	require.NoError(t, sigsyaml.Unmarshal([]byte(liveYAML), &liveDoc))
	liveSpec, ok := liveDoc["spec"].(map[string]any)
	require.True(t, ok)
	assert.InEpsilon(t, float64(5), liveSpec["replicas"], 0)
}

func TestClientLive_ResourceNotFound(t *testing.T) {
	t.Parallel()
	c := newTestClient(t)

	liveYAML, err := c.Live(t.Context(), clientTestDeployment)
	require.NoError(t, err)
	assert.Empty(t, liveYAML)
}

func TestClientLive_GetError(t *testing.T) {
	t.Parallel()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	fakeClient.PrependReactor("get", "deployments", func(_ clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "apps", Resource: "deployments"}, "web", errors.New("cannot get resource"),
		)
	})
	mapper := testrestmapper.TestOnlyStaticRESTMapper(clientgoscheme.Scheme)
	c := newClient(fakeClient, mapper)

	_, err := c.Live(t.Context(), clientTestDeployment)
	assert.Error(t, err)
}

func TestClientLive_DoesNotPatch(t *testing.T) {
	t.Parallel()
	fakeClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), unstructuredDeployment("web", "default", 5))
	mapper := testrestmapper.TestOnlyStaticRESTMapper(clientgoscheme.Scheme)
	c := newClient(fakeClient, mapper)

	_, err := c.Live(t.Context(), clientTestDeployment)
	require.NoError(t, err)
	for _, a := range fakeClient.Actions() {
		assert.NotEqual(t, "patch", a.GetVerb())
		assert.NotEqual(t, "create", a.GetVerb())
		assert.NotEqual(t, "update", a.GetVerb())
		assert.NotEqual(t, "delete", a.GetVerb())
	}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *rest.Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config builds a client",
			cfg:  &rest.Config{Host: "https://example.invalid:6443"},
		},
		{
			name: "invalid config returns error",
			cfg: &rest.Config{
				Host:        "https://example.invalid:6443",
				Username:    "user",
				BearerToken: "tok",
			},
			wantErr:     true,
			errContains: "build dynamic client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, err := NewClient(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, c)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, c)
			assert.NotNil(t, c.dynamicClient)
			assert.NotNil(t, c.mapper)
		})
	}
}

func TestClientLive_DecodeError(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	_, err := c.Live(t.Context(), "not: valid: yaml: [")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode resource")
}

func TestClientLive_UnmappedKind(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	unknownKindYAML := `
apiVersion: totally.unknown/v1
kind: FrobnicatorWidget
metadata:
  name: x
`
	_, err := c.Live(t.Context(), unknownKindYAML)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve resource mapping")
}

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
