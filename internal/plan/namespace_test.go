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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCheckTargetNamespaceOwnership(t *testing.T) {
	t.Parallel()
	ns := func(apiVersion, name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       "Namespace",
			"metadata":   map[string]any{"name": name},
		}}
	}
	rejected := []struct {
		name    string
		side    string
		objs    []*unstructured.Unstructured
		target  string
		wantErr string
	}{
		{
			name:    "core v1 target",
			side:    "rendered manifest",
			objs:    []*unstructured.Unstructured{ns("v1", "prod")},
			target:  "prod",
			wantErr: `rendered manifest declares target namespace "prod"`,
		},
		{
			name:    "other core version",
			side:    "previous release revision 3",
			objs:    []*unstructured.Unstructured{ns("v1beta1", "prod")},
			target:  "prod",
			wantErr: `previous release revision 3 declares target namespace "prod"`,
		},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkTargetNamespaceOwnership(tt.side, tt.objs, tt.target)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.ErrorContains(t, err, "must not claim it")
		})
	}

	allowed := []struct {
		name   string
		side   string
		objs   []*unstructured.Unstructured
		target string
	}{
		{
			name:   "other name",
			side:   "rendered manifest",
			objs:   []*unstructured.Unstructured{ns("v1", "extra")},
			target: "prod",
		},
		{
			name:   "grouped namespace is not the target",
			side:   "rendered manifest",
			objs:   []*unstructured.Unstructured{ns("example.com/v1", "prod")},
			target: "prod",
		},
	}
	for _, tt := range allowed {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkTargetNamespaceOwnership(tt.side, tt.objs, tt.target)
			require.NoError(t, err)
		})
	}

	t.Run("list is flattened by the caller", func(t *testing.T) {
		t.Parallel()
		flat, err := flattenManifest("apiVersion: v1\nkind: List\nitems:\n- apiVersion: v1\n  kind: Namespace\n  metadata:\n    name: prod\n")
		require.NoError(t, err)
		err = checkTargetNamespaceOwnership("rendered manifest", flat, "prod")
		require.Error(t, err)
		assert.ErrorContains(t, err, "target namespace")
	})
}
