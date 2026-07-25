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
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	sigsyaml "sigs.k8s.io/yaml"
)

// Identity uniquely identifies a Kubernetes object for collision checks.
type Identity struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

// String returns a human-readable identity for error messages.
func (id Identity) String() string {
	ns := id.Namespace
	if ns == "" {
		ns = "(cluster)"
	}
	return fmt.Sprintf("%s/%s %s/%s", id.APIVersion, id.Kind, ns, id.Name)
}

// Key returns a stable map key for identity comparisons.
func (id Identity) Key() string {
	return strings.Join([]string{id.APIVersion, id.Kind, id.Namespace, id.Name}, "\x00")
}

// Object is one Kubernetes document loaded from an extras file.
type Object struct {
	Path string
	Raw  []byte
	Obj  *unstructured.Unstructured
}

// Identity returns the object's identity, using an empty namespace for
// cluster-scoped resources that have none set.
func (o *Object) Identity() Identity {
	return Identity{
		APIVersion: o.Obj.GetAPIVersion(),
		Kind:       o.Obj.GetKind(),
		Namespace:  o.Obj.GetNamespace(),
		Name:       o.Obj.GetName(),
	}
}

// GVK returns the object's GroupVersionKind.
func (o *Object) GVK() schema.GroupVersionKind {
	return o.Obj.GroupVersionKind()
}

// MarshalYAML serializes the object back to YAML bytes.
func (o *Object) MarshalYAML() ([]byte, error) {
	out, err := sigsyaml.Marshal(o.Obj.Object)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", o.Identity(), err)
	}
	return out, nil
}

// getMetaStringMap returns metadata.<field> as map[string]string.
func getMetaStringMap(obj *unstructured.Unstructured, field string) map[string]string {
	meta, ok := obj.Object["metadata"].(map[string]any)
	if !ok || meta == nil {
		return nil
	}
	raw, ok := meta[field].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, isString := v.(string); isString {
			out[k] = s
		}
	}
	return out
}
