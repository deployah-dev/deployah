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

package extras_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/extras"
)

func TestLoad_CRDPresentationIdentity(t *testing.T) {
	t.Parallel()
	commented := "# keep comments\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n  annotations:\n    note: \"{{ .Release.Name }}\"\n"
	indented := "kind: CustomResourceDefinition\nmetadata:\n    name: gadgets.example.com\n"
	multi := "kind: CustomResourceDefinition\nmetadata:\n  name: one.example.com\n---\n---\nkind: CustomResourceDefinition\nmetadata:\n  name: two.example.com\n"
	nonsense := "kind: CustomResourceDefinition\nmetadata:\n  name: example\nnonsense: whatever\n"
	tests := []struct {
		name      string
		body      string
		wantNames []string
		wantKind  string
	}{
		{name: "valid single crd", body: widgetCRDBody, wantNames: []string{"widgets.example.com"}, wantKind: "CustomResourceDefinition"},
		{name: "comments quotes and helm-looking text", body: commented, wantNames: []string{"widgets.example.com"}, wantKind: "CustomResourceDefinition"},
		{name: "unusual indentation", body: indented, wantNames: []string{"gadgets.example.com"}, wantKind: "CustomResourceDefinition"},
		{name: "multi-doc with empty separator", body: multi, wantNames: []string{"one.example.com", "two.example.com"}, wantKind: "CustomResourceDefinition"},
		{name: "spec is not inspected", body: nonsense, wantNames: []string{"example"}, wantKind: "CustomResourceDefinition"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, ".deployah", "crds", "types.yaml")
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
			assert.NotContains(t, string(bundle.CRDs[0].Raw), "deployah.dev/source: manifests")
			require.Len(t, bundle.CRDDocs, len(tc.wantNames))
			for i, wantName := range tc.wantNames {
				assert.Equal(t, tc.wantKind, bundle.CRDDocs[i].Kind)
				assert.Equal(t, wantName, bundle.CRDDocs[i].Name)
				assert.Equal(t, i, bundle.CRDDocs[i].Index)
				assert.Equal(t, path, bundle.CRDDocs[i].Path)
				assert.NotContains(t, string(bundle.CRDDocs[i].YAML), "deployah.dev/")
			}
		})
	}
}

func TestLoad_CRDSourceContractErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "malformed yaml", body: "not: [valid\n", wantErr: "parse YAML"},
		{name: "missing kind", body: "metadata:\n  name: widgets.example.com\n", wantErr: "missing required kind"},
		{name: "missing metadata.name", body: "kind: CustomResourceDefinition\nmetadata: {}\n", wantErr: "missing required metadata.name"},
		{name: "non-crd kind", body: "kind: ConfigMap\nmetadata:\n  name: nope\n", wantErr: "document 1: only CustomResourceDefinition"},
		{name: "non-crd kind in second document", body: "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n---\nkind: ConfigMap\nmetadata:\n  name: nope\n", wantErr: "document 2: only CustomResourceDefinition"},
		{name: "malformed second document", body: "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n---\nnot: [valid\n", wantErr: "parse YAML"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".deployah", "crds", "bad.yaml"), tc.body)
			_, err := extras.Load(extras.LoadConfig{
				SpecDir:          dir,
				Project:          "demo",
				Environment:      "prod",
				DeclaredEnvs:     []string{"prod"},
				ReleaseNamespace: "default",
				Scope:            &extras.TableResolver{},
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoad_DuplicateCRDNamesAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "a.yaml"), "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n")
	writeFile(t, filepath.Join(dir, ".deployah", "crds", "b.yaml"), "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n")
	bundle, err := extras.Load(extras.LoadConfig{
		SpecDir:          dir,
		Project:          "demo",
		Environment:      "prod",
		DeclaredEnvs:     []string{"prod"},
		ReleaseNamespace: "default",
		Scope:            &extras.TableResolver{},
	})
	require.NoError(t, err)
	require.Len(t, bundle.CRDs, 2)
	require.Len(t, bundle.CRDDocs, 2)
	assert.Equal(t, "widgets.example.com", bundle.CRDDocs[0].Name)
	assert.Equal(t, "widgets.example.com", bundle.CRDDocs[1].Name)
}

func TestCRDDoc_HasNoSpecFields(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeFor[extras.CRDDoc]()
	_, hasScope := rt.FieldByName("Scope")
	assert.False(t, hasScope)
	_, hasSpec := rt.FieldByName("Spec")
	assert.False(t, hasSpec)
	_, hasObj := rt.FieldByName("Obj")
	assert.False(t, hasObj)
	_, hasGroup := rt.FieldByName("Group")
	assert.False(t, hasGroup)
}

func TestCRDDisplayPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{path: "/abs/.deployah/crds/foo.yaml", want: ".deployah/crds/foo.yaml"},
		{path: "bar.yml", want: ".deployah/crds/bar.yml"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, extras.CRDDisplayPath(tc.path))
		})
	}
}
