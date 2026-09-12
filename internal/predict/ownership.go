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
	"errors"
	"fmt"
	"maps"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	appManagedByLabel              = "app.kubernetes.io/managed-by"
	appManagedByHelm               = "Helm"
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"
)

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

func checkOwnership(obj *unstructured.Unstructured, releaseName, releaseNamespace string) error {
	var errs []error
	if err := requireValue(obj.GetLabels(), appManagedByLabel, appManagedByHelm); err != nil {
		errs = append(errs, fmt.Errorf("label validation error: %w", err))
	}
	if err := requireValue(obj.GetAnnotations(), helmReleaseNameAnnotation, releaseName); err != nil {
		errs = append(errs, fmt.Errorf("annotation validation error: %w", err))
	}
	if err := requireValue(obj.GetAnnotations(), helmReleaseNamespaceAnnotation, releaseNamespace); err != nil {
		errs = append(errs, fmt.Errorf("annotation validation error: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid ownership metadata; %w", errors.Join(errs...))
	}
	return nil
}

func requireValue(meta map[string]string, k, v string) error {
	actual, ok := meta[k]
	if !ok {
		return fmt.Errorf("missing key %q: must be set to %q", k, v)
	}
	if actual != v {
		return fmt.Errorf("key %q must equal %q: current value is %q", k, v, actual)
	}
	return nil
}

func resourceString(obj *unstructured.Unstructured) string {
	return fmt.Sprintf("%s %q in namespace %q", obj.GetKind(), obj.GetName(), obj.GetNamespace())
}

func ownershipConflict(obj *unstructured.Unstructured, err error) error {
	return fmt.Errorf("%s exists and cannot be imported into the current release: %w",
		resourceString(obj), err)
}
