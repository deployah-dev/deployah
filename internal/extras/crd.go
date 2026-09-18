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

package extras

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

// Policy is the CRD mutation mode used by the semantic plan predictor.
// It does not control Helm install or upgrade.
type Policy string

const (
	// PolicyCreate predicts a CRD create only when the object is missing.
	PolicyCreate Policy = "create"
	// PolicyCreateReplace predicts a create of a missing CRD or a
	// server-side apply over an existing one.
	PolicyCreateReplace Policy = "create-replace"

	// CRDFieldManager is the Kubernetes field manager the semantic
	// predictor uses for CRD apply options.
	CRDFieldManager = "deployah"
)

// DecodeCRD decodes o as a typed CustomResourceDefinition for the
// semantic plan predictor. It unmarshals the first YAML document only;
// later documents in a multi-doc file are ignored. Helm still receives
// the whole source file. It does not mutate chart CRD source bytes.
func DecodeCRD(o Object) (*apiextensionsv1.CustomResourceDefinition, error) {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := sigsyaml.Unmarshal(o.Raw, &crd); err != nil {
		return nil, fmt.Errorf("%s: decode CRD: %w", o.Path, err)
	}
	if crd.Name == "" {
		return nil, fmt.Errorf("%s: CRD metadata.name is empty", o.Path)
	}
	return &crd, nil
}

// CreateObject returns the unstructured body the semantic predictor uses
// for a policy=create Kubernetes CREATE. It decodes through the typed CRD
// so the dry-run request matches the typed object.
func CreateObject(o Object) (*unstructured.Unstructured, error) {
	crd, err := DecodeCRD(o)
	if err != nil {
		return nil, err
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(crd)
	if err != nil {
		return nil, fmt.Errorf("%s: convert CRD for create: %w", o.Path, err)
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	return u, nil
}

// ApplyObject returns the unstructured body the semantic predictor uses
// for create-replace server-side apply. Status and server-managed
// metadata are stripped so the patch matches the intended document
// rather than a typed round-trip full of null fields.
func ApplyObject(o Object) (*unstructured.Unstructured, error) {
	var obj map[string]any
	if err := sigsyaml.Unmarshal(o.Raw, &obj); err != nil {
		return nil, fmt.Errorf("%s: decode for apply: %w", o.Path, err)
	}
	delete(obj, "status")
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
		delete(meta, "resourceVersion")
		delete(meta, "uid")
		delete(meta, "creationTimestamp")
		delete(meta, "generation")
		delete(meta, "selfLink")
	}
	return &unstructured.Unstructured{Object: obj}, nil
}
