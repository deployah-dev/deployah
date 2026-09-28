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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestLoadDeclared_IdentityAndPairingKey(t *testing.T) {
	t.Parallel()
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: prod
---
apiVersion: v1
kind: ConfigMap
metadata:
  generateName: job-
  namespace: prod
`
	objs, err := flattenManifest(manifest)
	require.NoError(t, err)
	decls, err := declareAll(testRESTMapper{}, objs, "prod", "rendered manifest")
	require.NoError(t, err)
	require.Len(t, decls, 2)

	assert.NotNil(t, decls[0].identity)
	assert.Nil(t, decls[0].pairKey)
	assert.Equal(t, "app", decls[0].identity.Name)
	assert.Equal(t, "prod", decls[0].effectiveNamespace)
	assert.Equal(t, "prod", decls[0].obj.GetNamespace())

	assert.Nil(t, decls[1].identity)
	assert.NotNil(t, decls[1].pairKey)
	assert.Equal(t, "job-", decls[1].pairKey.GenerateName)
	assert.Equal(t, "prod", decls[1].pairKey.Namespace)
}

func TestLoadDeclared_EffectiveNamespaceDoesNotMutateObject(t *testing.T) {
	t.Parallel()
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
data:
  key: v1
`
	objs, err := flattenManifest(manifest)
	require.NoError(t, err)
	decls, err := declareAll(testRESTMapper{}, objs, "prod", "rendered manifest")
	require.NoError(t, err)
	require.Len(t, decls, 1)
	assert.Equal(t, "prod", decls[0].effectiveNamespace)
	assert.Empty(t, decls[0].obj.GetNamespace())
	_, found, nestErr := unstructured.NestedFieldNoCopy(decls[0].obj.Object, "metadata", "namespace")
	require.NoError(t, nestErr)
	assert.False(t, found)
}

func TestComparisonCopy_OnlyFillsOmittedNamespacedNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		manifest      string
		wantCopyNS    string
		wantObjectNS  string
		wantEffective string
	}{
		{
			name:          "omitted namespaced namespace",
			manifest:      "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\ndata:\n  key: v1\n",
			wantCopyNS:    "prod",
			wantEffective: "prod",
		},
		{
			name: "cluster scoped namespace stays declared",
			manifest: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: view
  namespace: stray
rules: []
`,
			wantCopyNS:   "stray",
			wantObjectNS: "stray",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objs, err := flattenManifest(tt.manifest)
			require.NoError(t, err)
			decls, err := declareAll(testRESTMapper{}, objs, "prod", "rendered manifest")
			require.NoError(t, err)
			require.Len(t, decls, 1)
			copied := comparisonCopy(decls[0])
			ns, found, nestErr := unstructured.NestedString(copied, "metadata", "namespace")
			require.NoError(t, nestErr)
			require.True(t, found)
			assert.Equal(t, tt.wantCopyNS, ns)
			assert.Equal(t, tt.wantObjectNS, decls[0].obj.GetNamespace())
			assert.Equal(t, tt.wantEffective, decls[0].effectiveNamespace)
		})
	}
}

func TestDiffDeclared_ListAndOrder(t *testing.T) {
	t.Parallel()
	previous := `apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: b
    namespace: prod
  data:
    key: old
- apiVersion: v1
  kind: ConfigMap
  metadata:
    name: a
    namespace: prod
`
	desired := `apiVersion: v1
kind: ConfigMap
metadata:
  name: a
  namespace: prod
data:
  key: new
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: c
  namespace: prod
`
	prevObjs, err := flattenManifest(previous)
	require.NoError(t, err)
	prevDecls, err := declareAll(testRESTMapper{}, prevObjs, "prod", "previous release")
	require.NoError(t, err)
	desiredObjs, err := flattenManifest(desired)
	require.NoError(t, err)
	desiredDecls, err := declareAll(testRESTMapper{}, desiredObjs, "prod", "rendered manifest")
	require.NoError(t, err)
	changes, err := diffDeclared(prevDecls, desiredDecls)
	require.NoError(t, err)
	require.Len(t, changes, 3)
	assert.Equal(t, semantic.Update, changes[0].Action)
	assert.Equal(t, "a", changes[0].Resource.Name)
	assert.Equal(t, semantic.Create, changes[1].Action)
	assert.Equal(t, "c", changes[1].Resource.Name)
	assert.Equal(t, semantic.Delete, changes[2].Action)
	assert.Equal(t, "b", changes[2].Resource.Name)
}

type testRESTMapper struct{}

func (testRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}
	scope := meta.RESTScopeNamespace
	if gk.Kind == "ClusterRole" {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gk.Group, Version: version, Resource: "items"},
		GroupVersionKind: gk.WithVersion(version),
		Scope:            scope,
	}, nil
}
