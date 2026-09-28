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

package plan_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/plan"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	fakedynamic "k8s.io/client-go/dynamic/fake"
)

func TestDynamicLiveReader_GetAndList(t *testing.T) {
	t.Parallel()
	app := liveObject("v1", "ConfigMap", "prod", "app", map[string]string{"deployah.dev/instance": "web"}, nil)
	other := liveObject("v1", "ConfigMap", "prod", "other", map[string]string{"deployah.dev/instance": "else"}, nil)
	ns := liveObject("v1", "Namespace", "", "prod", map[string]string{"deployah.dev/instance": "web"}, nil)
	dyn := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), app, other, ns)
	reader := plan.NewDynamicLiveReader(dyn)

	got, err := reader.Get(t.Context(), configMapMapping(), "prod", "app")
	require.NoError(t, err)
	assert.Equal(t, "app", got.GetName())

	_, err = reader.Get(t.Context(), configMapMapping(), "prod", "missing")
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err))

	cluster, err := reader.Get(t.Context(), namespaceMapping(), "ignored", "prod")
	require.NoError(t, err)
	assert.Equal(t, "prod", cluster.GetName())
	assert.Empty(t, cluster.GetNamespace())

	selector, err := labels.Parse("deployah.dev/instance=web")
	require.NoError(t, err)
	listed, err := reader.List(t.Context(), configMapMapping(), "prod", selector)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "app", listed[0].GetName())

	for _, action := range dyn.Actions() {
		assert.Contains(t, []string{"get", "list"}, action.GetVerb())
	}
}

func TestDynamicLiveReader_RequiresClientAndSelector(t *testing.T) {
	t.Parallel()
	reader := plan.NewDynamicLiveReader(nil)
	_, err := reader.Get(t.Context(), configMapMapping(), "prod", "app")
	require.Error(t, err)
	assert.ErrorContains(t, err, "dynamic client")

	dyn := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme())
	_, err = plan.NewDynamicLiveReader(dyn).List(t.Context(), configMapMapping(), "prod", nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "label selector")
	assert.Empty(t, dyn.Actions())
}

func configMapMapping() *meta.RESTMapping {
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
		Scope:            meta.RESTScopeNamespace,
	}
}

func namespaceMapping() *meta.RESTMapping {
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
		GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "Namespace"},
		Scope:            meta.RESTScopeRoot,
	}
}

func liveObject(apiVersion, kind, namespace, name string, lbl, ann map[string]string) *unstructured.Unstructured {
	meta := map[string]any{"name": name}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	if len(lbl) > 0 {
		labels := make(map[string]any, len(lbl))
		for k, v := range lbl {
			labels[k] = v
		}
		meta["labels"] = labels
	}
	if len(ann) > 0 {
		annotations := make(map[string]any, len(ann))
		for k, v := range ann {
			annotations[k] = v
		}
		meta["annotations"] = annotations
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   meta,
	}}
}
