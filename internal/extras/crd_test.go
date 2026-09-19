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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func sampleCRDObject(t *testing.T, name string) Object {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata": map[string]any{
			"name":              name,
			"resourceVersion":   "99",
			"uid":               "abc",
			"creationTimestamp": "2020-01-01T00:00:00Z",
			"managedFields":     []any{map[string]any{"manager": "other"}},
		},
		"status": map[string]any{
			"conditions": []any{},
		},
		"spec": map[string]any{
			"group": "example.com",
			"scope": "Namespaced",
			"names": map[string]any{
				"kind":   "Widget",
				"plural": "widgets",
			},
			"versions": []any{
				map[string]any{
					"name":    "v1",
					"served":  true,
					"storage": true,
					"schema": map[string]any{
						"openAPIV3Schema": map[string]any{"type": "object"},
					},
				},
			},
		},
	}}
	o := Object{Path: name + ".yaml", Obj: obj}
	raw, err := o.MarshalYAML()
	require.NoError(t, err)
	o.Raw = raw
	return o
}

func TestDecodeCRD_RejectsInvalidYAMLAndEmptyName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		obj     Object
		wantErr string
	}{
		{
			name:    "invalid yaml",
			obj:     Object{Path: "bad.yaml", Raw: []byte("not: [valid")},
			wantErr: "decode CRD",
		},
		{
			name: "empty metadata.name",
			obj: Object{Path: "noname.yaml", Raw: []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {}
spec:
  group: example.com
`)},
			wantErr: "metadata.name is empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeCRD(tc.obj)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestDecodeCRD_FirstDocumentOnly(t *testing.T) {
	t.Parallel()
	raw := []byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gadgets.example.com
spec:
  group: example.com
  names:
    kind: Gadget
    plural: gadgets
`)
	crd, err := DecodeCRD(Object{Path: "multi.yaml", Raw: raw})
	require.NoError(t, err)
	assert.Equal(t, "widgets.example.com", crd.Name)
}

func TestApplyObject_StripsServerFields(t *testing.T) {
	t.Parallel()

	o := sampleCRDObject(t, "widgets.example.com")
	u, err := ApplyObject(o)
	require.NoError(t, err)
	got, err := json.Marshal(u.Object)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind": "CustomResourceDefinition",
		"metadata": {"name": "widgets.example.com"},
		"spec": {
			"group": "example.com",
			"scope": "Namespaced",
			"names": {"kind": "Widget", "plural": "widgets"},
			"versions": [{
				"name": "v1",
				"served": true,
				"storage": true,
				"schema": {"openAPIV3Schema": {"type": "object"}}
			}]
		}
	}`, string(got))
}

func TestCreateObject_UsesTypedDecode(t *testing.T) {
	t.Parallel()

	o := sampleCRDObject(t, "widgets.example.com")
	typed, err := DecodeCRD(o)
	require.NoError(t, err)
	u, err := CreateObject(o)
	require.NoError(t, err)
	assert.Equal(t, "apiextensions.k8s.io/v1", u.GetAPIVersion())
	assert.Equal(t, "CustomResourceDefinition", u.GetKind())
	assert.Equal(t, typed.Name, u.GetName())
	assert.Equal(t, "deployah", CRDFieldManager)
}
