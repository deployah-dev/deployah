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
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type recordedApply struct {
	Name      string
	Namespace string
	Obj       *unstructured.Unstructured
}

type recordedJSONPatch struct {
	ID    predict.Identity
	Patch []byte
}

type recordedDelete struct {
	ID predict.Identity
}

type fakeCluster struct {
	objects           map[string]*unstructured.Unstructured
	getErr            map[string]error
	mappingErr        map[schema.GroupVersionKind]error
	clusterScoped     map[schema.GroupVersionKind]bool
	applyErr          error
	applySeq          []error
	applyCalls        int
	jsonPatchErr      error
	jsonPatchCalls    int
	jsonPatchConflict int
	deleteErr         error
	deleteConflict    int
	gets              []predict.Identity
	applies           []recordedApply
	jsonPatches       []recordedJSONPatch
	deletes           []recordedDelete
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{
		objects:       make(map[string]*unstructured.Unstructured),
		getErr:        make(map[string]error),
		mappingErr:    make(map[schema.GroupVersionKind]error),
		clusterScoped: make(map[schema.GroupVersionKind]bool),
	}
}

func objectKey(id predict.Identity) string {
	gv := schema.GroupVersion{Group: id.Group, Version: id.Version}
	return fmt.Sprintf("%s/%s/%s/%s", gv.String(), id.Kind, id.Namespace, id.Name)
}

func identityOf(obj *unstructured.Unstructured) predict.Identity {
	gvk := obj.GroupVersionKind()
	return predict.Identity{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func (f *fakeCluster) store(obj *unstructured.Unstructured) {
	f.objects[objectKey(identityOf(obj))] = obj.DeepCopy()
}

func (f *fakeCluster) Get(_ context.Context, id predict.Identity) (*unstructured.Unstructured, error) {
	f.gets = append(f.gets, id)
	if err, ok := f.getErr[objectKey(id)]; ok {
		return nil, err
	}
	obj, ok := f.objects[objectKey(id)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: strings.ToLower(id.Kind) + "s"}, id.Name)
	}
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) Apply(_ context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	f.applyCalls++
	f.applies = append(f.applies, recordedApply{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Obj:       obj.DeepCopy(),
	})
	if len(f.applySeq) > 0 {
		err := f.applySeq[0]
		f.applySeq = f.applySeq[1:]
		if err != nil {
			return nil, err
		}
	} else if f.applyErr != nil {
		return nil, f.applyErr
	}
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) JSONPatch(_ context.Context, id predict.Identity, patch []byte) error {
	f.jsonPatchCalls++
	f.jsonPatches = append(f.jsonPatches, recordedJSONPatch{ID: id, Patch: append([]byte(nil), patch...)})
	if f.jsonPatchConflict > 0 {
		f.jsonPatchConflict--
		return apierrors.NewConflict(
			schema.GroupResource{Resource: strings.ToLower(id.Kind) + "s"},
			id.Name,
			fmt.Errorf("the object has been modified"),
		)
	}
	return f.jsonPatchErr
}

func (f *fakeCluster) Delete(_ context.Context, id predict.Identity) error {
	f.deletes = append(f.deletes, recordedDelete{ID: id})
	if f.deleteConflict > 0 {
		f.deleteConflict--
		return apierrors.NewConflict(
			schema.GroupResource{Resource: strings.ToLower(id.Kind) + "s"},
			id.Name,
			fmt.Errorf("the object has been modified"),
		)
	}
	return f.deleteErr
}

func (f *fakeCluster) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	if err, ok := f.mappingErr[gvk]; ok {
		return nil, err
	}
	scope := meta.RESTScopeNamespace
	if f.clusterScoped[gvk] {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: strings.ToLower(gvk.Kind) + "s"},
		GroupVersionKind: gvk,
		Scope:            scope,
	}, nil
}

func configMapYAML(name, namespace, data string) string {
	nsLine := ""
	if namespace != "" {
		nsLine = "\n  namespace: " + namespace
	}
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s%s
data:
  key: %s
`, name, nsLine, data)
}

func configMapYAMLWith(name, namespace, extraMeta, data string) string {
	nsLine := ""
	if namespace != "" {
		nsLine = "\n  namespace: " + namespace
	}
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s%s
%s
data:
  key: %s
`, name, nsLine, extraMeta, data)
}

func ownedConfigMap(name, namespace, release string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "Helm",
			},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      release,
				"meta.helm.sh/release-namespace": namespace,
			},
		},
		"data": map[string]any{"key": "live"},
	}}
}

func clusterWidgetGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "ClusterWidget"}
}

func clusterWidgetYAML(name, namespace string) string {
	nsLine := ""
	if namespace != "" {
		nsLine = "\n  namespace: " + namespace
	}
	return fmt.Sprintf(`apiVersion: example.com/v1
kind: ClusterWidget
metadata:
  name: %s%s
`, name, nsLine)
}

func clusterWidget(name, namespace string) *unstructured.Unstructured {
	meta := map[string]any{"name": name}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "ClusterWidget",
		"metadata":   meta,
	}}
}

func foreignConfigMap(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"data": map[string]any{"key": "foreign"},
	}}
}

func findResult(results []predict.Result, name string) (predict.Result, bool) {
	for _, r := range results {
		if r.Identity.Name == name {
			return r, true
		}
	}
	return predict.Result{}, false
}

func resourceQuotaConflict() error {
	return apierrors.NewConflict(
		schema.GroupResource{Resource: "resourcequotas"},
		"default",
		fmt.Errorf("Operation cannot be fulfilled on resourcequotas \"default\": the object has been modified"),
	)
}

func fieldManagerConflict() error {
	return apierrors.NewConflict(
		schema.GroupResource{Resource: "configmaps"},
		"app",
		fmt.Errorf("conflict with \"other-manager\""),
	)
}

func deployahUpdateManagedFields() []metav1.ManagedFieldsEntry {
	return []metav1.ManagedFieldsEntry{{
		Manager:    "deployah",
		Operation:  metav1.ManagedFieldsOperationUpdate,
		APIVersion: "v1",
		FieldsType: "FieldsV1",
		FieldsV1:   metav1.NewFieldsV1(`{"f:data":{"f:key":{}}}`),
	}}
}
