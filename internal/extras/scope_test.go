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

package extras_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
)

// TestTableResolver_BuiltInAndDefault exercises extras package behavior.
func TestTableResolver_BuiltInAndDefault(t *testing.T) {
	t.Parallel()
	r := &extras.TableResolver{}
	ns, err := r.Namespaced(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
	require.NoError(t, err)
	assert.True(t, ns)

	ns, err = r.Namespaced(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"})
	require.NoError(t, err)
	assert.False(t, ns)

	known, err := r.Known(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	require.NoError(t, err)
	assert.True(t, known)

	// Kind alone is not enough: wrong group is unknown.
	known, err = r.Known(schema.GroupVersionKind{Group: "evil.io", Version: "v1", Kind: "Deployment"})
	require.NoError(t, err)
	assert.False(t, known)

	known, err = r.Known(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	require.NoError(t, err)
	assert.False(t, known)

	// Namespaced still defaults unknown kinds to namespaced when called.
	ns, err = r.Namespaced(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	require.NoError(t, err)
	assert.True(t, ns)
}

// TestTableResolver_OperatorAllowlistUsesRealGroups checks group-aware
// allowlist matching for common operator kinds.
func TestTableResolver_OperatorAllowlistUsesRealGroups(t *testing.T) {
	t.Parallel()
	r := &extras.TableResolver{}
	known, err := r.Known(schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"})
	require.NoError(t, err)
	assert.True(t, known)

	known, err = r.Known(schema.GroupVersionKind{Group: "evil.io", Version: "v1", Kind: "Certificate"})
	require.NoError(t, err)
	assert.False(t, known)

	known, err = r.Known(schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"})
	require.NoError(t, err)
	assert.True(t, known)
}

// TestGroupVersionsFromCRDs extracts group/version pairs from CRD YAML.
func TestGroupVersionsFromCRDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "cert.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: certificates.cert-manager.io
spec:
  group: cert-manager.io
  scope: Namespaced
  names:
    kind: Certificate
    plural: certificates
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	gvs := extras.GroupVersionsFromCRDs(bundle.CRDs)
	_, ok := gvs["cert-manager.io/v1"]
	assert.True(t, ok)
}

// TestTableResolver_KnownFromCRDScope exercises extras package behavior.
func TestTableResolver_KnownFromCRDScope(t *testing.T) {
	t.Parallel()
	r := &extras.TableResolver{CRDScope: map[string]bool{
		"example.com/widget": true,
	}}
	known, err := r.Known(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	require.NoError(t, err)
	assert.True(t, known)
}

// TestTableResolver_CRDScopeOverridesDefault exercises extras package behavior.
func TestTableResolver_CRDScopeOverridesDefault(t *testing.T) {
	t.Parallel()
	r := &extras.TableResolver{CRDScope: map[string]bool{
		"example.com/clusterwidget": false,
	}}
	ns, err := r.Namespaced(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "ClusterWidget"})
	require.NoError(t, err)
	assert.False(t, ns)
}

// TestLoad_UsesCRDScopeForCustomResources exercises extras package behavior.
func TestLoad_UsesCRDScopeForCustomResources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "widget.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusterwidgets.example.com
spec:
  group: example.com
  scope: Cluster
  names:
    kind: ClusterWidget
    plural: clusterwidgets
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cw.yaml"), `
apiVersion: example.com/v1
kind: ClusterWidget
metadata:
  name: one
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
	assert.Empty(t, bundle.Manifests[0].Obj.GetNamespace())
}
