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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
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
	for _, m := range bundle.Manifests {
		assert.Equal(t, "review", m.Obj.GetLabels()[spec.LabelEnvironment])
		assert.NotEqual(t, "review-pr-42", m.Obj.GetLabels()[spec.LabelEnvironment])
		assert.Equal(t, helm.GenerateReleaseName("demo", "review/pr-42"), m.Obj.GetLabels()[spec.LabelInstance])
		assert.Equal(t, "review/pr-42", m.Obj.GetAnnotations()[spec.AnnotationEnvironmentInstance])
		assert.NotContains(t, m.Obj.GetLabels(), "app.kubernetes.io/instance")
	}
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
    deployah.dev/task: impostor
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
	assert.Equal(t, helm.GenerateReleaseName("shop", "prod"), obj.GetLabels()[spec.LabelInstance])
	assert.Equal(t, "prod", obj.GetAnnotations()[spec.AnnotationEnvironmentInstance])
	assert.Equal(t, "mine", obj.GetLabels()["app"])
	assert.Equal(t, spec.SourceManifests, obj.GetAnnotations()[spec.AnnotationSource])
	assert.Equal(t, "shop", obj.GetAnnotations()[spec.AnnotationProject])
	assert.Equal(t, "keep", obj.GetAnnotations()["note"])
	assert.NotContains(t, obj.GetLabels(), spec.LabelComponent)
	assert.NotContains(t, obj.GetLabels(), spec.LabelTask)
	assert.NotContains(t, obj.GetLabels(), spec.LabelManagedBy)
	assert.NotContains(t, obj.GetLabels(), spec.LabelVersion)
}

func TestLoad_PreservesUserHelmInstanceLabel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: data
  labels:
    app.kubernetes.io/instance: my-custom-app
`)
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "shop",
		Environment:      "review/pr-123",
		DeclaredEnvs:     []string{"review"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Manifests, 1)
	obj := bundle.Manifests[0].Obj
	assert.Equal(t, "my-custom-app", obj.GetLabels()["app.kubernetes.io/instance"])
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), obj.GetLabels()[spec.LabelInstance])
	assert.Equal(t, "review", obj.GetLabels()[spec.LabelEnvironment])
	assert.Equal(t, "review/pr-123", obj.GetAnnotations()[spec.AnnotationEnvironmentInstance])
}

func TestLoad_ClusterScopedKeepsInstance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "ns.yaml"), `
apiVersion: v1
kind: Namespace
metadata:
  name: extra
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
	assert.Equal(t, helm.GenerateReleaseName("shop", "prod"), obj.GetLabels()[spec.LabelInstance])
	assert.Empty(t, obj.GetNamespace())
	assert.NotContains(t, obj.GetLabels(), "app.kubernetes.io/instance")
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

const widgetCRDBody = `
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
`

func TestLoad_CRDsKeepExactSourceBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      string
		wantNames []string
	}{
		{name: "valid crd", body: widgetCRDBody, wantNames: []string{"widgets.example.com"}},
		{
			name: "user metadata unchanged",
			body: `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
  namespace: foo
  labels:
    keep: custom
    deployah.dev/source: user-source
  annotations:
    note: keep
    deployah.dev/project: user-project
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
`,
			wantNames: []string{"widgets.example.com"},
		},
		{
			name:      "multi-document file stays one RawFile",
			body:      widgetCRDBody + "---\napiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: gadgets.example.com\n",
			wantNames: []string{"widgets.example.com", "gadgets.example.com"},
		},
		{
			name:      "comments quotes and helm-looking text",
			body:      "# keep comments\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n  annotations:\n    note: \"{{ .Release.Name }}\"\n",
			wantNames: []string{"widgets.example.com"},
		},
		{
			name:      "comment-only leading document stays in raw file",
			body:      "# CRDs used by the widget controller\n---\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n",
			wantNames: []string{"widgets.example.com"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, ".deployah", "crds", "widget.yaml")
			writeFile(t, path, tc.body)
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
			assert.Equal(t, path, bundle.CRDs[0].Path)
			assert.Equal(t, tc.body, string(bundle.CRDs[0].Raw))
			require.Len(t, bundle.CRDDocs, len(tc.wantNames))
			for i, wantName := range tc.wantNames {
				assert.Equal(t, "CustomResourceDefinition", bundle.CRDDocs[i].Kind)
				assert.Equal(t, wantName, bundle.CRDDocs[i].Name)
			}
		})
	}
}

func TestLoad_CRDSnapshotIgnoresLaterDiskWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".deployah", "crds", "widget.yaml")
	writeFile(t, path, widgetCRDBody)
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
	writeFile(t, path, widgetCRDBody+"# mutated\n")
	assert.Equal(t, widgetCRDBody, string(bundle.CRDs[0].Raw))
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
	assert.Contains(t, err.Error(), "install that API on the cluster first")
}

// TestLoad_OfflineAllowsUnknownType lets plan --offline load custom
// resources without discovery (scope defaults to namespaced).
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

// TestLoad_CRDsRejectSubdirectories keeps the flat .deployah/crds/ contract.
func TestLoad_CRDsRejectSubdirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "nested", "x.yaml"), widgetCRDBody)
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

// TestLoad_RequiresScopeAndProject rejects incomplete LoadConfig.
func TestLoad_RequiresScopeAndProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     extras.LoadConfig
		wantErr string
	}{
		{
			name:    "missing scope",
			cfg:     extras.LoadConfig{Project: "demo"},
			wantErr: "ScopeResolver is required",
		},
		{
			name:    "missing project",
			cfg:     extras.LoadConfig{Scope: &extras.TableResolver{}},
			wantErr: "Project is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := extras.Load(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
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

func TestLoad_CustomResolverIgnoresCRDFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "widget.yaml"), widgetCRDBody)
	writeFile(t, filepath.Join(dir, ".deployah", "manifests", "w.yaml"), `
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
		Scope:            rejectAllScope{},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown type")
}

// TestLoadFromSpec_Offline loads extras without a rest.Config.
func TestLoadFromSpec_Offline(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	specPath := filepath.Join(dir, "deployah.yaml")
	writeFile(t, specPath, "apiVersion: deployah.dev/v1-alpha.4\nproject: demo\n")
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

func TestLoad_RejectsHelmHookAnnotations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{name: "hook", key: v1.HookAnnotation},
		{name: "hook-weight", key: v1.HookWeightAnnotation},
		{name: "hook-delete-policy", key: v1.HookDeleteAnnotation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".deployah", "manifests", "hook.yaml"), fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: hooked
  annotations:
    %s: pre-install
`, tt.key))
			_, err := extras.Load(extras.LoadConfig{
				SpecDir:          dir,
				Project:          "demo",
				Environment:      "prod",
				DeclaredEnvs:     []string{"prod"},
				ReleaseNamespace: "apps",
				Scope:            &extras.TableResolver{},
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.key)
			assert.ErrorContains(t, err, "not supported on custom manifests")
		})
	}

	t.Run("plain configmap", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".deployah", "manifests", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: plain
data:
  k: v
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
	})
}

func TestLoad_AllowsCRDWithHelmHookAnnotation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "widget.yaml"), fmt.Sprintf(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
  annotations:
    %s: pre-install
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
`, v1.HookAnnotation))
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "apps",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.CRDs, 1)
}
