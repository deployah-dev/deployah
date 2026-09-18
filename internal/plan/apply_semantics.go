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
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/plan/semantic"
)

func snapshotOf(obj *unstructured.Unstructured) *semantic.ResourceSnapshot {
	if obj == nil {
		return nil
	}
	cp := obj.DeepCopy()
	if cp.Object == nil {
		return &semantic.ResourceSnapshot{}
	}
	return &semantic.ResourceSnapshot{Object: cp.Object}
}

func writeApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Write: &semantic.WriteSemantics{
			Method:         semantic.WriteServerSide,
			FieldManager:   kube.ManagedFieldsManager,
			ForceConflicts: false,
		},
	}
}

func writeCreate() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Write: &semantic.WriteSemantics{
			Method: semantic.WriteCreate,
		},
	}
}

func writeCRDApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Write: &semantic.WriteSemantics{
			Method:         semantic.WriteServerSide,
			FieldManager:   extras.CRDFieldManager,
			ForceConflicts: true,
		},
	}
}

func deleteApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Delete: &semantic.DeleteSemantics{
			Propagation: semantic.PropagationBackground,
		},
	}
}

func resourceRefOf(obj *unstructured.Unstructured) semantic.ResourceRef {
	ref := semantic.ResourceRef{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
	}
	if ref.Name == "" {
		ref.GenerateName = obj.GetGenerateName()
	}
	return ref
}
