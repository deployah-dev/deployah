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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/spec"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// TestLoad_MissingDirsEmptyBundle exercises extras package behavior.
func TestLoad_MissingDirsEmptyBundle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	assert.Empty(t, bundle.Manifests)
	assert.Empty(t, bundle.CRDs)
}

// TestLoad_FileSelection exercises extras package behavior.
func TestLoad_FileSelection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.Join(dir, ".deployah", "manifests")
	writeFile(t, filepath.Join(root, "ok.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: ok
`)
	writeFile(t, filepath.Join(root, "README.md"), "# ignore")
	writeFile(t, filepath.Join(root, ".old.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: hidden
`)
	writeFile(t, filepath.Join(root, ".gitkeep"), "")

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
	assert.Equal(t, "ok", bundle.Manifests[0].Obj.GetName())
}

// TestLoad_VisibleNonYAMLFails exercises extras package behavior.
func TestLoad_VisibleNonYAMLFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "notes.txt"), "nope")
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file")
}

// TestLoad_MultiDocAndEmptySkipped exercises extras package behavior.
func TestLoad_MultiDocAndEmptySkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "multi.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: a
---
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: b
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
	require.Len(t, bundle.Manifests, 2)
	names := []string{bundle.Manifests[0].Obj.GetName(), bundle.Manifests[1].Obj.GetName()}
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}

// TestLoad_DuplicateIdentityFails exercises extras package behavior.
func TestLoad_DuplicateIdentityFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cm := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared
`
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "one.yaml"), cm)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "prod", "two.yaml"), cm)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate object")
	assert.Contains(t, err.Error(), "one.yaml")
	assert.Contains(t, err.Error(), "two.yaml")
}

// TestLoad_CRDInManifestsFails exercises extras package behavior.
func TestLoad_CRDInManifestsFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "bad.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
  versions:
    - name: v1
      served: true
      storage: true
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".deployah/crds/")
}

// TestLoad_UnknownEnvDirFails exercises extras package behavior.
func TestLoad_UnknownEnvDirFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "staging", "x.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: x
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown environment directory")
}

// TestLoad_EnvSubdirPrefixMatch exercises extras package behavior.
func TestLoad_EnvSubdirPrefixMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "common.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: common
`)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "review", "pr.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: review-only
`)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "prod", "prod.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: prod-only
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "review/pr-42",
		DeclaredEnvs:     []string{"prod", "review"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	names := make([]string, 0, len(bundle.Manifests))
	for _, m := range bundle.Manifests {
		names = append(names, m.Obj.GetName())
	}
	assert.ElementsMatch(t, []string{"common", "review-only"}, names)
}

// TestLoad_MergesIdentityAndFillsNamespace exercises extras package behavior.
func TestLoad_MergesIdentityAndFillsNamespace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: data
  labels:
    app: mine
    deployah.dev/project: wrong
    deployah.dev/managed-by: impostor
    deployah.dev/version: "9"
  annotations:
    note: keep
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "shop",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
	obj := bundle.Manifests[0].Obj
	assert.Equal(t, "apps", obj.GetNamespace())
	assert.Equal(t, "data", obj.GetName())
	assert.Equal(t, "shop", obj.GetLabels()[spec.LabelProject])
	assert.Equal(t, "prod", obj.GetLabels()[spec.LabelEnvironment])
	assert.Equal(t, "mine", obj.GetLabels()["app"])
	assert.Equal(t, spec.SourceManifests, obj.GetAnnotations()[spec.AnnotationSource])
	assert.Equal(t, "shop", obj.GetAnnotations()[spec.AnnotationProject])
	assert.Equal(t, "keep", obj.GetAnnotations()["note"])
	assert.NotContains(t, obj.GetLabels(), spec.LabelComponent)
	assert.NotContains(t, obj.GetLabels(), spec.LabelManagedBy)
	assert.NotContains(t, obj.GetLabels(), spec.LabelVersion)
}

// TestLoad_NamespaceMismatchFails exercises extras package behavior.
func TestLoad_NamespaceMismatchFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: data
  namespace: other
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differs from release namespace")
}

// TestLoad_ClusterScopedRejectsNamespace exercises extras package behavior.
func TestLoad_ClusterScopedRejectsNamespace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "ns.yaml"), `
apiVersion: v1
kind: Namespace
metadata:
  name: extra
  namespace: oops
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not set metadata.namespace")
}

// TestLoad_CRDsMergedWithoutEnvironment exercises extras package behavior.
func TestLoad_CRDsMergedWithoutEnvironment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "widget.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
  labels:
    deployah.dev/environment: should-strip
    deployah.dev/managed-by: impostor
    keep: custom
spec:
  group: example.com
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
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
	require.Len(t, bundle.CRDs, 1)
	obj := bundle.CRDs[0].Obj
	assert.Equal(t, spec.SourceCRDs, obj.GetAnnotations()[spec.AnnotationSource])
	assert.Equal(t, "demo", obj.GetAnnotations()[spec.AnnotationProject])
	assert.Equal(t, "demo", obj.GetLabels()[spec.LabelProject])
	assert.Equal(t, "custom", obj.GetLabels()["keep"])
	assert.NotContains(t, obj.GetLabels(), spec.LabelEnvironment)
	assert.NotContains(t, obj.GetLabels(), spec.LabelManagedBy)
}

// TestLoad_MissingRequiredFieldsFails exercises extras package behavior.
func TestLoad_MissingRequiredFieldsFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "bad.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata: {}
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required")
}

// TestLoad_DuplicateAfterNamespaceFillFails exercises extras package behavior.
func TestLoad_DuplicateAfterNamespaceFillFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "empty-ns.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared
`)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "filled-ns.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: shared
  namespace: apps
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate object")
}

// TestLoad_UnknownTypeFails exercises extras package behavior.
func TestLoad_UnknownTypeFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "widget.yaml"), `
apiVersion: example.com/v1
kind: Widget
metadata:
  name: one
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown type")
	assert.Contains(t, err.Error(), ".deployah/crds/")
}

// TestLoad_OfflineAllowsUnknownType lets plan --offline load custom
// resources without discovery or an in-repo CRD (scope defaults to namespaced).
func TestLoad_OfflineAllowsUnknownType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "widget.yaml"), `
apiVersion: example.com/v1
kind: Widget
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
		Offline:          true,
	})
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
	assert.Equal(t, "apps", bundle.Manifests[0].Obj.GetNamespace())
}

// TestLoad_CRDWrongAPIVersionFails rejects non-v1 CRD apiVersions at load.
func TestLoad_CRDWrongAPIVersionFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "old.yaml"), `
apiVersion: example.com/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiextensions.k8s.io/v1")
	assert.Contains(t, err.Error(), "example.com/v1")
}

// TestLoad_RequiresScopeAndProject rejects incomplete LoadConfig.
func TestLoad_RequiresScopeAndProject(t *testing.T) {
	t.Parallel()
	_, err := extras.Load(extras.LoadConfig{Project: "demo"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ScopeResolver is required")

	_, err = extras.Load(extras.LoadConfig{Scope: &extras.TableResolver{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Project is required")
}

// TestLoad_NonCRDUnderCRDsFails rejects ordinary objects in .deployah/crds/.
func TestLoad_NonCRDUnderCRDsFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: nope
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only CustomResourceDefinition")
}

// TestLoad_CRDSubdirForbidden rejects nested dirs under .deployah/crds/.
func TestLoad_CRDSubdirForbidden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "nested", "x.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
  versions:
    - name: v1
      served: true
      storage: true
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subdirectories are not allowed")
}

// TestLoad_NestedManifestDirFails rejects nesting under an env subdir.
func TestLoad_NestedManifestDirFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "prod", "nested", "x.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: x
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nested directories are not allowed")
}

// TestLoad_InvalidYAMLFails surfaces parse errors with the file path.
func TestLoad_InvalidYAMLFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "bad.yaml"), "not: [valid")
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad.yaml")
}

// TestLoad_EmptyReleaseNamespaceFails when a namespaced object needs a fill.
func TestLoad_EmptyReleaseNamespaceFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: data
`)
	_, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "",
		Scope:            &extras.TableResolver{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no release namespace")
}

type rejectAllScope struct{}

func (rejectAllScope) Known(schema.GroupVersionKind) (bool, error) { return false, nil }
func (rejectAllScope) Namespaced(schema.GroupVersionKind) (bool, error) {
	return true, nil
}

// TestLoad_ChainedScopeFromCustomResolver covers withCRDScope's default branch.
func TestLoad_ChainedScopeFromCustomResolver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "widget.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "w.yaml"), `
apiVersion: example.com/v1
kind: Widget
metadata:
  name: one
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            rejectAllScope{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
	assert.Equal(t, "apps", bundle.Manifests[0].Obj.GetNamespace())
}

// TestLoad_DiscoveryResolverMergesCRDScope covers the DiscoveryResolver branch
// of withCRDScope (mapper nil falls back to the table + CRD scope).
func TestLoad_DiscoveryResolverMergesCRDScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "widget.yaml"), `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  scope: Namespaced
  names:
    kind: Widget
    plural: widgets
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "w.yaml"), `
apiVersion: example.com/v1
kind: Widget
metadata:
  name: one
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.DiscoveryResolver{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
}

// TestLoadFromSpec_Offline loads extras without a rest.Config.
func TestLoadFromSpec_Offline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "deployah.yaml")
	writeFile(t, specPath, "apiVersion: deployah.dev/v1-alpha.3\nproject: demo\n")
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: data
`)
	bundle, err := extras.LoadFromSpec(specPath, &spec.Spec{Project: "demo"}, nil, "prod", "apps", nil)
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
	assert.NotNil(t, bundle.PostRendererFor())
}
