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

package extras

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeCRDClient struct {
	mu       sync.Mutex
	objects  map[string]*apiextensionsv1.CustomResourceDefinition
	creates  int
	applies  int
	getCalls int
	// establishAfterNGets makes Established true after N Get calls per name.
	establishAfterNGets int
	getsPerName         map[string]int
	// failGetAfter, when > 0, makes Get return an API error after that many calls.
	failGetAfter int
	lastPatch    []byte
}

func newFakeCRDClient() *fakeCRDClient {
	return &fakeCRDClient{
		objects:     make(map[string]*apiextensionsv1.CustomResourceDefinition),
		getsPerName: make(map[string]int),
	}
}

func (f *fakeCRDClient) Create(_ context.Context, crd *apiextensionsv1.CustomResourceDefinition, _ metav1.CreateOptions) (*apiextensionsv1.CustomResourceDefinition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects[crd.Name]; ok {
		return nil, apierrors.NewAlreadyExists(schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"}, crd.Name)
	}
	cp := crd.DeepCopy()
	f.objects[crd.Name] = cp
	f.creates++
	return cp.DeepCopy(), nil
}

func (f *fakeCRDClient) Apply(_ context.Context, name string, patch []byte) (*apiextensionsv1.CustomResourceDefinition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var obj map[string]any
	if err := json.Unmarshal(patch, &obj); err != nil {
		return nil, err
	}
	if _, hasStatus := obj["status"]; hasStatus {
		return nil, errors.New("apply patch must not include status")
	}
	if meta, ok := obj["metadata"].(map[string]any); ok {
		if _, has := meta["managedFields"]; has {
			return nil, errors.New("apply patch must not include managedFields")
		}
		if _, has := meta["resourceVersion"]; has {
			return nil, errors.New("apply patch must not include resourceVersion")
		}
	}
	f.lastPatch = append([]byte(nil), patch...)
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	f.objects[name] = crd
	f.applies++
	return crd.DeepCopy(), nil
}

func (f *fakeCRDClient) Get(_ context.Context, name string, _ metav1.GetOptions) (*apiextensionsv1.CustomResourceDefinition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.failGetAfter > 0 && f.getCalls > f.failGetAfter {
		return nil, errors.New("api unavailable")
	}
	obj, ok := f.objects[name]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions"}, name)
	}
	cp := obj.DeepCopy()
	f.getsPerName[name]++
	if f.establishAfterNGets <= 0 || f.getsPerName[name] >= f.establishAfterNGets {
		cp.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{{
			Type:   apiextensionsv1.Established,
			Status: apiextensionsv1.ConditionTrue,
		}}
	}
	return cp, nil
}

func sampleCRDObject(t *testing.T, name string) Object {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata": map[string]any{
			"name":              name,
			"resourceVersion":   "99",
			"uid":               "abc",
			"creationTimestamp": "2020-01-01T00:00:00Z",
			"managedFields":     []any{map[string]any{"manager": "other"}},
		},
		"status": map[string]any{
			"conditions": []any{},
		},
		"spec": map[string]any{
			"group": "example.com",
			"scope": "Namespaced",
			"names": map[string]any{
				"kind":   "Widget",
				"plural": "widgets",
			},
			"versions": []any{
				map[string]any{
					"name":    "v1",
					"served":  true,
					"storage": true,
					"schema": map[string]any{
						"openAPIV3Schema": map[string]any{"type": "object"},
					},
				},
			},
		},
	}}
	o := Object{Path: name + ".yaml", Obj: obj}
	raw, err := o.MarshalYAML()
	require.NoError(t, err)
	o.Raw = raw
	return o
}

// TestApplyCRDs_CreateIfMissing exercises extras package behavior.
func TestApplyCRDs_CreateIfMissing(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.establishAfterNGets = 1
	existing := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "existing.example.com", ResourceVersion: "1"},
	}
	client.objects[existing.Name] = existing

	stats, err := applyCRDs(t.Context(), client, []Object{
		sampleCRDObject(t, "existing.example.com"),
		sampleCRDObject(t, "new.example.com"),
	}, PolicyCreate, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 1, client.creates)
	assert.Equal(t, 0, client.applies)
	assert.Equal(t, 1, stats.Created)
	assert.Equal(t, 0, stats.Replaced)
	assert.Equal(t, 2, stats.Ready)
	assert.Contains(t, client.objects, "new.example.com")
}

// TestApplyCRDs_CreateReplaceAppliesExisting exercises extras package behavior.
func TestApplyCRDs_CreateReplaceAppliesExisting(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.establishAfterNGets = 1
	client.objects["widgets.example.com"] = &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com", ResourceVersion: "7"},
	}

	stats, err := applyCRDs(t.Context(), client, []Object{
		sampleCRDObject(t, "widgets.example.com"),
	}, PolicyCreateReplace, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, client.creates)
	assert.Equal(t, 1, client.applies)
	assert.Equal(t, 0, stats.Created)
	assert.Equal(t, 1, stats.Replaced)
	assert.Equal(t, 1, stats.Ready)
	require.NotEmpty(t, client.lastPatch)
	assert.NotContains(t, string(client.lastPatch), `"status"`)
	assert.NotContains(t, string(client.lastPatch), `"managedFields"`)
	assert.NotContains(t, string(client.lastPatch), `"resourceVersion"`)
}

// TestApplyCRDs_CreateReplaceAppliesMissing exercises extras package behavior.
func TestApplyCRDs_CreateReplaceAppliesMissing(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.establishAfterNGets = 1

	stats, err := applyCRDs(t.Context(), client, []Object{
		sampleCRDObject(t, "new.example.com"),
	}, PolicyCreateReplace, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, client.creates)
	assert.Equal(t, 1, client.applies)
	assert.Equal(t, 1, stats.Created)
	assert.Equal(t, 0, stats.Replaced)
	assert.Contains(t, client.objects, "new.example.com")
}

// TestApplyCRDs_WaitsForEstablished exercises extras package behavior.
func TestApplyCRDs_WaitsForEstablished(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.establishAfterNGets = 3

	_, err := applyCRDs(t.Context(), client, []Object{
		sampleCRDObject(t, "slow.example.com"),
	}, PolicyCreate, 2*time.Second)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, client.getsPerName["slow.example.com"], 3)
}

// TestApplyCRDs_Timeout exercises extras package behavior.
func TestApplyCRDs_Timeout(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.establishAfterNGets = 1_000_000

	_, err := applyCRDs(t.Context(), client, []Object{
		sampleCRDObject(t, "never.example.com"),
	}, PolicyCreate, 300*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

// TestApplyCRDs_WaitAPIError exercises extras package behavior.
func TestApplyCRDs_WaitAPIError(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.objects["broken.example.com"] = &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "broken.example.com"},
	}
	client.failGetAfter = 1

	_, err := applyCRDs(t.Context(), client, []Object{
		sampleCRDObject(t, "broken.example.com"),
	}, PolicyCreate, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wait for CRD")
	assert.NotContains(t, err.Error(), "timed out")
}

// TestApplyCRDs_Canceled exercises extras package behavior.
func TestApplyCRDs_Canceled(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	client.establishAfterNGets = 1_000_000
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := applyCRDs(ctx, client, []Object{
		sampleCRDObject(t, "cancel.example.com"),
	}, PolicyCreate, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled")
	assert.NotContains(t, err.Error(), "timed out")
}

// TestApplyCRDs_EmptyNoop exercises extras package behavior.
func TestApplyCRDs_EmptyNoop(t *testing.T) {
	t.Parallel()
	client := newFakeCRDClient()
	stats, err := applyCRDs(t.Context(), client, nil, PolicyCreate, time.Second)
	require.NoError(t, err)
	assert.Equal(t, 0, client.creates)
	assert.Equal(t, CRDStats{}, stats)
}

// TestSSAPatchFromObject_StripsServerFields exercises extras package behavior.
func TestSSAPatchFromObject_StripsServerFields(t *testing.T) {
	t.Parallel()
	o := sampleCRDObject(t, "widgets.example.com")
	patch, err := ssaPatchFromObject(o)
	require.NoError(t, err)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(patch, &obj))
	_, hasStatus := obj["status"]
	assert.False(t, hasStatus)
	meta, ok := obj["metadata"].(map[string]any)
	require.True(t, ok)
	_, hasMF := meta["managedFields"]
	assert.False(t, hasMF)
	_, hasRV := meta["resourceVersion"]
	assert.False(t, hasRV)
	assert.Equal(t, "widgets.example.com", meta["name"])
	spec, ok := obj["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "example.com", spec["group"])
}
