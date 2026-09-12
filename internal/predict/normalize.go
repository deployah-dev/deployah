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
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func equalBookkeeping(live, predicted *unstructured.Unstructured) bool {
	if live == nil || predicted == nil {
		return false
	}
	return equality.Semantic.DeepEqual(bookkeepingCopy(live).Object, bookkeepingCopy(predicted).Object)
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
