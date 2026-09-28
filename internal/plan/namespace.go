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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// checkTargetNamespaceOwnership returns an error if objs include the
// release Namespace. side names the manifest. Helm install creates that
// namespace outside the release, so the release must not claim it.
// Other Namespace names stay. Nothing is read live or rewritten.
func checkTargetNamespaceOwnership(side string, objs []*unstructured.Unstructured, targetNamespace string) error {
	for _, obj := range objs {
		if !declaresTargetNamespace(obj, targetNamespace) {
			continue
		}
		return fmt.Errorf("%s declares target namespace %q: the target namespace is an execution prerequisite created by helm install outside the release, and the release must not claim it", side, targetNamespace)
	}
	return nil
}

func declaresTargetNamespace(obj *unstructured.Unstructured, targetNamespace string) bool {
	if obj == nil {
		return false
	}
	gvk := obj.GroupVersionKind()
	return gvk.Group == "" && gvk.Kind == "Namespace" && obj.GetName() == targetNamespace
}
