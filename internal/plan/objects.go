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
	"bytes"
	"errors"
	"fmt"
	"maps"
	"strings"

	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"
)

const (
	appManagedByLabel              = "app.kubernetes.io/managed-by"
	appManagedByHelm               = "Helm"
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"
)

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
		if u, ok := info.Object.(*unstructured.Unstructured); ok {
			out = append(out, u.DeepCopy())
			continue
		}
		m, convErr := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if convErr != nil {
			return nil, fmt.Errorf("convert object to unstructured: %w", convErr)
		}
		out = append(out, &unstructured.Unstructured{Object: m})
	}
	return out, nil
}

func resourceMatchKey(id ResourceIdentity) string {
	return id.Group + "\x00" + id.Kind + "\x00" + id.Namespace + "\x00" + id.Name
}

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

func stampMetadata(obj *unstructured.Unstructured, releaseName, releaseNamespace string) {
	obj.SetLabels(mergeStrStrMaps(obj.GetLabels(), map[string]string{
		appManagedByLabel: appManagedByHelm,
	}))
	obj.SetAnnotations(mergeStrStrMaps(obj.GetAnnotations(), map[string]string{
		helmReleaseNameAnnotation:      releaseName,
		helmReleaseNamespaceAnnotation: releaseNamespace,
	}))
}

func mergeStrStrMaps(current, desired map[string]string) map[string]string {
	result := make(map[string]string, len(current)+len(desired))
	maps.Copy(result, current)
	maps.Copy(result, desired)
	return result
}

func stampCopy(obj *unstructured.Unstructured, releaseName, releaseNamespace string) *unstructured.Unstructured {
	cp := obj.DeepCopy()
	stampMetadata(cp, releaseName, releaseNamespace)
	return cp
}

func bookkeepingCopy(obj *unstructured.Unstructured) *unstructured.Unstructured {
	cp := obj.DeepCopy()
	unstructured.RemoveNestedField(cp.Object, "status")
	unstructured.RemoveNestedField(cp.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(cp.Object, "metadata", "uid")
	unstructured.RemoveNestedField(cp.Object, "metadata", "generation")
	unstructured.RemoveNestedField(cp.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(cp.Object, "metadata", "managedFields")
	return cp
}

func stripOwnership(obj *unstructured.Unstructured) *unstructured.Unstructured {
	cp := bookkeepingCopy(obj)
	labels := maps.Clone(cp.GetLabels())
	delete(labels, appManagedByLabel)
	if len(labels) == 0 {
		cp.SetLabels(nil)
	} else {
		cp.SetLabels(labels)
	}
	ann := maps.Clone(cp.GetAnnotations())
	delete(ann, helmReleaseNameAnnotation)
	delete(ann, helmReleaseNamespaceAnnotation)
	if len(ann) == 0 {
		cp.SetAnnotations(nil)
	} else {
		cp.SetAnnotations(ann)
	}
	return cp
}

func equalRawIntent(a, b *unstructured.Unstructured) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return equality.Semantic.DeepEqual(stripOwnership(a).Object, stripOwnership(b).Object)
}

func equalObjects(a, b *unstructured.Unstructured) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return equality.Semantic.DeepEqual(bookkeepingCopy(a).Object, bookkeepingCopy(b).Object)
}

func liveKeepPolicy(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	return obj.GetAnnotations()[kube.ResourcePolicyAnno] == kube.KeepPolicy
}

func normalizeScope(obj *unstructured.Unstructured, mapping *meta.RESTMapping, defaultNamespace string) {
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if obj.GetNamespace() == "" {
			obj.SetNamespace(defaultNamespace)
		}
		return
	}
	obj.SetNamespace("")
}

func normalizeScopeFromDesc(obj *unstructured.Unstructured, d apiDesc, defaultNamespace string) {
	if d.cluster {
		obj.SetNamespace("")
		return
	}
	if obj.GetNamespace() == "" {
		obj.SetNamespace(defaultNamespace)
	}
}
