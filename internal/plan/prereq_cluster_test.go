// Copyright 2026 The Deployah Authors
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
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type prereqFake struct {
	mappingErr map[schema.GroupVersionKind]error
	gets       []predict.Identity
	applies    []*unstructured.Unstructured
}

func (f *prereqFake) Get(_ context.Context, id predict.Identity) (*unstructured.Unstructured, error) {
	f.gets = append(f.gets, id)
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: strings.ToLower(id.Kind) + "s"}, id.Name)
}

func (f *prereqFake) Create(_ context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return obj.DeepCopy(), nil
}

func (f *prereqFake) Apply(_ context.Context, obj *unstructured.Unstructured, _ predict.ApplyOptions) (*unstructured.Unstructured, error) {
	f.applies = append(f.applies, obj.DeepCopy())
	return obj.DeepCopy(), nil
}

func (f *prereqFake) JSONPatch(context.Context, predict.Identity, []byte) error { return nil }

func (f *prereqFake) Delete(context.Context, predict.Identity) error { return nil }

func (f *prereqFake) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	if err, ok := f.mappingErr[gvk]; ok {
		return nil, err
	}
	scope := meta.RESTScopeNamespace
	if gvk.Kind == "Namespace" && gvk.Group == "" {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: strings.ToLower(gvk.Kind) + "s"},
		GroupVersionKind: gvk,
		Scope:            scope,
	}, nil
}

func TestPrereqCluster_GenerateNameApplyWithoutGet(t *testing.T) {
	t.Parallel()
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	inner := &prereqFake{mappingErr: map[schema.GroupVersionKind]error{
		gvk: &meta.NoKindMatchError{GroupKind: gvk.GroupKind()},
	}}
	surface := newCRDSurface()
	require.NoError(t, surface.add(crdAPI{
		Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "widgets", Versions: []string{"v1"},
	}, true))
	wrapped := newPrereqCluster(inner, true, "prod", surface)
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"generateName": "app-",
			"namespace":    "prod",
		},
	}}
	require.True(t, wrapped.needsSyntheticApply(obj))
	got, err := wrapped.Apply(t.Context(), obj, predict.ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, "app-", got.GetGenerateName())
	assert.Empty(t, inner.applies)
	assert.Empty(t, inner.gets)
}

func TestPrereqCluster_SyntheticGetIsNotFound(t *testing.T) {
	t.Parallel()
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	inner := &prereqFake{mappingErr: map[schema.GroupVersionKind]error{
		gvk: &meta.NoKindMatchError{GroupKind: gvk.GroupKind()},
	}}
	surface := newCRDSurface()
	require.NoError(t, surface.add(crdAPI{
		Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "widgets", Versions: []string{"v1"},
	}, true))
	wrapped := newPrereqCluster(inner, false, "prod", surface)
	_, err := wrapped.Get(t.Context(), predict.Identity{Group: "example.com", Version: "v1", Kind: "Widget", Name: "app", Namespace: "prod"})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err))
	assert.Empty(t, inner.gets)
}
