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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func implicitNamespace(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   name,
			"labels": map[string]any{"name": name},
		},
	}}
}

func namespaceLocator(name string, cluster ClusterReader) (ResourceLocator, error) {
	id := ResourceIdentity{Version: "v1", Kind: "Namespace", Name: name}
	mapping, err := cluster.Mapping(id.GroupVersionKind())
	if err != nil {
		return ResourceLocator{}, fmt.Errorf("resolve namespace mapping for %s: %w", name, err)
	}
	return locatorFromMapping(id, mapping), nil
}

func planNamespace(ctx context.Context, cluster ClusterReader, op helm.Operation, name string) (semantic.ResourceChange, bool, error) {
	if op != helm.OperationInstall {
		return semantic.ResourceChange{}, false, nil
	}
	loc, err := namespaceLocator(name, cluster)
	if err != nil {
		return semantic.ResourceChange{}, false, err
	}

	live, err := cluster.Get(ctx, loc)
	if apierrors.IsNotFound(err) {
		live = nil
		err = nil
	}
	if err != nil {
		return semantic.ResourceChange{}, false, fmt.Errorf("get namespace %s: %w", name, err)
	}

	desired := implicitNamespace(name)
	ref := semantic.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: name}
	origin := semantic.ResourceOrigin{Kind: semantic.OriginNamespace}
	if live == nil {
		return semantic.ResourceChange{
			Resource:   ref,
			Origin:     origin,
			Action:     semantic.Create,
			After:      snapshotOf(desired),
			Apply:      writeApply(),
			ApplyOrder: 1,
		}, true, nil
	}
	projected, projErr := semantic.ProjectOntoDeclared(live.Object, desired.Object)
	if projErr != nil {
		return semantic.ResourceChange{}, false, fmt.Errorf("namespace %s: %w", name, projErr)
	}
	before := &unstructured.Unstructured{Object: projected}
	if equalObjects(before, desired) {
		return semantic.ResourceChange{}, false, nil
	}
	return semantic.ResourceChange{
		Resource:   ref,
		Origin:     origin,
		Action:     semantic.Update,
		Before:     snapshotOf(before),
		After:      snapshotOf(desired),
		Apply:      writeApply(),
		ApplyOrder: 1,
	}, true, nil
}

func chartContainsTargetNamespace(manifest, namespace string) (bool, error) {
	objs, err := flattenManifest(manifest)
	if err != nil {
		return false, err
	}
	for _, obj := range objs {
		if obj.GetAPIVersion() == "v1" && obj.GetKind() == "Namespace" && obj.GetName() == namespace {
			return true, nil
		}
	}
	return false, nil
}
