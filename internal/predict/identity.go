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
	"bytes"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
)

type resourceObj struct {
	id  Identity
	obj *unstructured.Unstructured
}

// GroupVersionKind returns the API group, version, and kind of id.
func (id Identity) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: id.Group, Version: id.Version, Kind: id.Kind}
}

func identityOf(obj *unstructured.Unstructured) Identity {
	gvk := obj.GroupVersionKind()
	return Identity{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

// objectKey matches Helm pkg/action/upgrade.go: groupVersion/kind/ns/name.
func objectKey(id Identity) string {
	gv := schema.GroupVersion{Group: id.Group, Version: id.Version}
	return fmt.Sprintf("%s/%s/%s/%s", gv.String(), id.Kind, id.Namespace, id.Name)
}

// sameResource matches Helm isMatchingInfo: name, namespace, GroupKind.
func sameResource(a, b Identity) bool {
	return a.Name == b.Name && a.Namespace == b.Namespace && a.Group == b.Group && a.Kind == b.Kind
}

func flattenManifest(manifest string) ([]*unstructured.Unstructured, error) {
	if strings.TrimSpace(manifest) == "" {
		return nil, nil
	}
	infos, err := resource.NewLocalBuilder().
		ContinueOnError().
		Flatten().
		Unstructured().
		Stream(bytes.NewBufferString(manifest), "manifest").
		Do().Infos()
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	out := make([]*unstructured.Unstructured, 0, len(infos))
	for _, info := range infos {
		u, convErr := asUnstructured(info.Object)
		if convErr != nil {
			return nil, convErr
		}
		out = append(out, u)
	}
	return out, nil
}

func asUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.DeepCopy(), nil
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("convert object to unstructured: %w", err)
	}
	return &unstructured.Unstructured{Object: m}, nil
}

func loadResources(cluster Cluster, manifest, defaultNamespace string) ([]resourceObj, error) {
	objs, err := flattenManifest(manifest)
	if err != nil {
		return nil, err
	}
	out := make([]resourceObj, 0, len(objs))
	for _, obj := range objs {
		if _, genErr := generateNameState(obj); genErr != nil {
			return nil, genErr
		}
		mapping, mapErr := cluster.Mapping(obj.GroupVersionKind())
		if mapErr != nil {
			return nil, fmt.Errorf("resolve resource mapping for %s %s: %w",
				obj.GroupVersionKind(), obj.GetName(), mapErr)
		}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace && obj.GetNamespace() == "" {
			obj.SetNamespace(defaultNamespace)
		}
		out = append(out, resourceObj{id: identityOf(obj), obj: obj})
	}
	return out, nil
}

// generateNameState mirrors Helm validateNameAndGenerateName. A generateName
// without a name skips ownership. Name and generateName together is an error.
func generateNameState(obj *unstructured.Unstructured) (bool, error) {
	name := obj.GetName()
	generateName := obj.GetGenerateName()
	if name == "" && generateName != "" {
		return true, nil
	}
	if name != "" && generateName != "" {
		return false, errors.New("metadata.name and metadata.generateName cannot both be set")
	}
	return false, nil
}
