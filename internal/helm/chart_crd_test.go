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

package helm

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/spec"
)

func resolvedForChartCRDs(t *testing.T) *spec.ResolvedSpec {
	t.Helper()
	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "chart-crds",
		Components: map[string]spec.Component{"web": serviceComponent()},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	resolved, _, err := spec.Resolve(manifest, nil, spec.NormalizeEnv("production"), spec.SubstitutionReport{})
	require.NoError(t, err)
	return resolved
}

func prepareChartWithCRDs(t *testing.T, crds []extras.RawFile) (chartDir, backing string) {
	t.Helper()
	cache := NewChartCache(time.Hour)
	resolved := resolvedForChartCRDs(t)
	chartDir, err := PrepareChart(t.Context(), resolved, cache, crds)
	require.NoError(t, err)
	key, err := cache.GenerateKey(resolved.Env.Original, resolved)
	require.NoError(t, err)
	backing, found := cache.get(key)
	require.True(t, found)
	t.Cleanup(func() {
		removeChartDir(t, chartDir)
		removeChartDir(t, backing)
	})
	return chartDir, backing
}

func widgetCRD(name, extraMetadata string) string {
	meta := "  name: " + name + "\n"
	if extraMetadata != "" {
		meta += extraMetadata
	}
	return "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n" + meta +
		"spec:\n  group: example.com\n  names:\n    kind: Widget\n    plural: widgets\n  scope: Namespaced\n  versions:\n  - name: v1\n    served: true\n    storage: true\n    schema:\n      openAPIV3Schema:\n        type: object\n"
}

func TestPrepareChart_OmitsCRDsDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		crds []extras.RawFile
	}{
		{name: "nil slice"},
		{name: "empty slice", crds: []extras.RawFile{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chartDir, backing := prepareChartWithCRDs(t, tc.crds)
			assert.NoDirExists(t, filepath.Join(chartDir, chartCRDsDir))
			assert.NoDirExists(t, filepath.Join(backing, chartCRDsDir))
		})
	}
}

func TestPrepareChart_WritesSourceFiles(t *testing.T) {
	t.Parallel()
	widgets := widgetCRD("widgets.example.com", "")
	gadgets := widgetCRD("gadgets.example.com", "")
	userMeta := "  labels:\n    app: widgets\n    deployah.dev/owned-by: team\n  annotations:\n    note: keep-me\n    deployah.dev/source: user-supplied\n"
	userBody := widgetCRD("widgets.example.com", userMeta)
	tests := []struct {
		name      string
		crds      []extras.RawFile
		wantFiles map[string]string
	}{
		{
			name: "one source file keeps basename",
			crds: []extras.RawFile{{Path: "/abs/.deployah/crds/widgets.yaml", Raw: []byte(widgets)}},
			wantFiles: map[string]string{
				"widgets.yaml": widgets,
			},
		},
		{
			name: "multiple source files keep basenames",
			crds: []extras.RawFile{
				{Path: ".deployah/crds/widgets.yaml", Raw: []byte(widgets)},
				{Path: ".deployah/crds/gadgets.yaml", Raw: []byte(gadgets)},
			},
			wantFiles: map[string]string{
				"widgets.yaml": widgets,
				"gadgets.yaml": gadgets,
			},
		},
		{
			name: "yaml and yml extensions preserved",
			crds: []extras.RawFile{
				{Path: "widgets.yaml", Raw: []byte(widgets)},
				{Path: "gadgets.yml", Raw: []byte(gadgets)},
			},
			wantFiles: map[string]string{
				"widgets.yaml": widgets,
				"gadgets.yml":  gadgets,
			},
		},
		{
			name: "multi-doc stays one file",
			crds: []extras.RawFile{
				{Path: ".deployah/crds/types.yaml", Raw: []byte(widgets + "---\n" + gadgets)},
			},
			wantFiles: map[string]string{
				"types.yaml": widgets + "---\n" + gadgets,
			},
		},
		{
			name: "user metadata preserved without injected keys",
			crds: []extras.RawFile{{Path: "widgets.yaml", Raw: []byte(userBody)}},
			wantFiles: map[string]string{
				"widgets.yaml": userBody,
			},
		},
		{
			name: "comments and unusual indentation stay exact",
			crds: []extras.RawFile{{Path: "widgets.yaml", Raw: []byte("# keep\nkind: CustomResourceDefinition\nmetadata:\n    name: widgets.example.com\n")}},
			wantFiles: map[string]string{
				"widgets.yaml": "# keep\nkind: CustomResourceDefinition\nmetadata:\n    name: widgets.example.com\n",
			},
		},
		{
			name: "comment-only leading document stays in raw file",
			crds: []extras.RawFile{{Path: "widgets.yaml", Raw: []byte("# CRDs used by the widget controller\n---\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n")}},
			wantFiles: map[string]string{
				"widgets.yaml": "# CRDs used by the widget controller\n---\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n",
			},
		},
		{
			name: "malformed yaml stays exact bytes",
			crds: []extras.RawFile{{Path: "broken.yaml", Raw: []byte("not: [valid\n")}},
			wantFiles: map[string]string{
				"broken.yaml": "not: [valid\n",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chartDir, backing := prepareChartWithCRDs(t, tc.crds)
			assert.NoDirExists(t, filepath.Join(backing, chartCRDsDir))
			for name, want := range tc.wantFiles {
				got, err := os.ReadFile(filepath.Join(chartDir, chartCRDsDir, name)) // #nosec G304 -- path under test-controlled chart copy
				require.NoError(t, err)
				assert.Equal(t, want, string(got), name)
			}
		})
	}
}

func TestPrepareChart_TemplateTextStaysLiteralAndOffTemplates(t *testing.T) {
	t.Parallel()
	body := widgetCRD("widgets.example.com", "  annotations:\n    note: \"{{ .Release.Name }}\"\n")
	chartDir, _ := prepareChartWithCRDs(t, []extras.RawFile{
		{Path: "widgets.yaml", Raw: []byte(body)},
	})
	got, err := os.ReadFile(filepath.Join(chartDir, chartCRDsDir, "widgets.yaml")) // #nosec G304 -- path under test-controlled chart copy
	require.NoError(t, err)
	assert.Equal(t, body, string(got))
	assert.Contains(t, string(got), "{{ .Release.Name }}")

	ch, err := loader.Load(chartDir)
	require.NoError(t, err)
	for _, tmpl := range ch.Templates {
		assert.NotContains(t, tmpl.Name, "crds/", "CRD file %s must not be a chart template", tmpl.Name)
	}
}

func TestPrepareChart_HelmCRDObjects(t *testing.T) {
	t.Parallel()
	widgets := widgetCRD("widgets.example.com", "")
	gadgets := widgetCRD("gadgets.example.com", "")
	tests := []struct {
		name     string
		crds     []extras.RawFile
		wantYAML map[string]string
	}{
		{
			name: "one file per source basename",
			crds: []extras.RawFile{
				{Path: "widgets.yaml", Raw: []byte(widgets)},
				{Path: "gadgets.yml", Raw: []byte(gadgets)},
			},
			wantYAML: map[string]string{
				"crds/widgets.yaml": widgets,
				"crds/gadgets.yml":  gadgets,
			},
		},
		{
			name: "multi-doc is one helm crd file",
			crds: []extras.RawFile{
				{Path: "types.yaml", Raw: []byte(widgets + "---\n" + gadgets)},
			},
			wantYAML: map[string]string{
				"crds/types.yaml": widgets + "---\n" + gadgets,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chartDir, _ := prepareChartWithCRDs(t, tc.crds)
			ch, err := loader.Load(chartDir)
			require.NoError(t, err)
			got := ch.CRDObjects()
			require.Len(t, got, len(tc.wantYAML))
			names := make([]string, 0, len(got))
			for _, crd := range got {
				names = append(names, crd.Name)
				want, ok := tc.wantYAML[crd.Name]
				require.True(t, ok, crd.Name)
				assert.Equal(t, want, string(crd.File.Data), crd.Name)
			}
			wantNames := make([]string, 0, len(tc.wantYAML))
			for name := range tc.wantYAML {
				wantNames = append(wantNames, name)
			}
			assert.ElementsMatch(t, wantNames, names)
		})
	}
}

func TestPrepareChart_RejectsInvalidCRDs(t *testing.T) {
	t.Parallel()
	body := widgetCRD("widgets.example.com", "")
	tests := []struct {
		name    string
		crds    []extras.RawFile
		wantErr string
	}{
		{
			name:    "parent directory",
			crds:    []extras.RawFile{{Path: "..", Raw: []byte(body)}},
			wantErr: "unsafe file name",
		},
		{
			name:    "missing extension",
			crds:    []extras.RawFile{{Path: "widgets", Raw: []byte(body)}},
			wantErr: "file name must end in .yaml or .yml",
		},
		{
			name:    "non yaml extension",
			crds:    []extras.RawFile{{Path: "widgets.txt", Raw: []byte(body)}},
			wantErr: "file name must end in .yaml or .yml",
		},
		{
			name: "destination collision",
			crds: []extras.RawFile{
				{Path: "a/widgets.yaml", Raw: []byte(body)},
				{Path: "b/widgets.yaml", Raw: []byte(widgetCRD("gadgets.example.com", ""))},
			},
			wantErr: "collides",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cache := NewChartCache(time.Hour)
			resolved := resolvedForChartCRDs(t)
			_, err := PrepareChart(t.Context(), resolved, cache, tc.crds)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
			key, keyErr := cache.GenerateKey(resolved.Env.Original, resolved)
			require.NoError(t, keyErr)
			backing, found := cache.get(key)
			if !found {
				return
			}
			t.Cleanup(func() { removeChartDir(t, backing) })
			assert.NoDirExists(t, filepath.Join(backing, chartCRDsDir))
		})
	}
}

func TestPrepareChart_ChangedCRDBytesNewCopyBackingStaysClean(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(time.Hour)
	resolved := resolvedForChartCRDs(t)
	firstBody := widgetCRD("widgets.example.com", "  labels:\n    v: one\n")
	secondBody := widgetCRD("widgets.example.com", "  labels:\n    v: two\n")

	first, err := PrepareChart(t.Context(), resolved, cache, []extras.RawFile{{Path: "widgets.yaml", Raw: []byte(firstBody)}})
	require.NoError(t, err)
	second, err := PrepareChart(t.Context(), resolved, cache, []extras.RawFile{{Path: "widgets.yaml", Raw: []byte(secondBody)}})
	require.NoError(t, err)
	assert.NotEqual(t, first, second)

	key, err := cache.GenerateKey(resolved.Env.Original, resolved)
	require.NoError(t, err)
	backing, found := cache.get(key)
	require.True(t, found)
	t.Cleanup(func() {
		removeChartDir(t, first)
		removeChartDir(t, second)
		removeChartDir(t, backing)
	})

	assert.NoDirExists(t, filepath.Join(backing, chartCRDsDir))
	gotFirst, err := os.ReadFile(filepath.Join(first, chartCRDsDir, "widgets.yaml")) // #nosec G304 -- path under test-controlled chart copy
	require.NoError(t, err)
	gotSecond, err := os.ReadFile(filepath.Join(second, chartCRDsDir, "widgets.yaml")) // #nosec G304 -- path under test-controlled chart copy
	require.NoError(t, err)
	assert.Equal(t, firstBody, string(gotFirst))
	assert.Equal(t, secondBody, string(gotSecond))
}

func TestPrepareChart_DifferentCRDSetsDoNotLeak(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(time.Hour)
	resolved := resolvedForChartCRDs(t)

	withWidgets, err := PrepareChart(t.Context(), resolved, cache, []extras.RawFile{
		{Path: "widgets.yaml", Raw: []byte(widgetCRD("widgets.example.com", ""))},
	})
	require.NoError(t, err)
	withGadgets, err := PrepareChart(t.Context(), resolved, cache, []extras.RawFile{
		{Path: "gadgets.yaml", Raw: []byte(widgetCRD("gadgets.example.com", ""))},
	})
	require.NoError(t, err)

	key, err := cache.GenerateKey(resolved.Env.Original, resolved)
	require.NoError(t, err)
	backing, found := cache.get(key)
	require.True(t, found)
	t.Cleanup(func() {
		removeChartDir(t, withWidgets)
		removeChartDir(t, withGadgets)
		removeChartDir(t, backing)
	})

	assert.FileExists(t, filepath.Join(withWidgets, chartCRDsDir, "widgets.yaml"))
	assert.NoFileExists(t, filepath.Join(withWidgets, chartCRDsDir, "gadgets.yaml"))
	assert.FileExists(t, filepath.Join(withGadgets, chartCRDsDir, "gadgets.yaml"))
	assert.NoFileExists(t, filepath.Join(withGadgets, chartCRDsDir, "widgets.yaml"))
	assert.NoDirExists(t, filepath.Join(backing, chartCRDsDir))
}

func TestPrepareChart_MaterializeFailureRemovesCopyLeavesBacking(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(time.Hour)
	resolved := resolvedForChartCRDs(t)
	ok, err := PrepareChart(t.Context(), resolved, cache, []extras.RawFile{
		{Path: "widgets.yaml", Raw: []byte(widgetCRD("widgets.example.com", ""))},
	})
	require.NoError(t, err)
	removeChartDir(t, ok)

	key, err := cache.GenerateKey(resolved.Env.Original, resolved)
	require.NoError(t, err)
	backing, found := cache.get(key)
	require.True(t, found)
	t.Cleanup(func() { removeChartDir(t, backing) })

	marker := "marker-" + t.Name() + "-" + rand.Text()
	require.NoError(t, os.WriteFile(filepath.Join(backing, "marker.txt"), []byte(marker), 0o600))

	_, err = PrepareChart(t.Context(), resolved, cache, []extras.RawFile{
		{Path: "..", Raw: []byte(widgetCRD("widgets.example.com", ""))},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsafe file name")

	_, stillFound := cache.get(key)
	assert.True(t, stillFound)
	assert.DirExists(t, backing)
	assert.NoDirExists(t, filepath.Join(backing, chartCRDsDir))

	matches, globErr := filepath.Glob(filepath.Join(os.TempDir(), "deployah-chart-copy-*"))
	require.NoError(t, globErr)
	leftovers := 0
	for _, dir := range matches {
		data, readErr := os.ReadFile(filepath.Join(dir, "marker.txt")) // #nosec G304 -- glob of process temp copies
		if readErr == nil && string(data) == marker {
			leftovers++
		}
	}
	assert.Zero(t, leftovers, "failed materialization must RemoveAll the chart copy")
}

func TestRenderOffline_ChartCRDObjectsMatchPassedFiles(t *testing.T) {
	t.Parallel()
	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	resolved := resolvedForChartCRDs(t)
	crds := []extras.RawFile{
		{Path: "widgets.yaml", Raw: []byte(widgetCRD("widgets.example.com", ""))},
		{Path: "types.yaml", Raw: []byte(widgetCRD("gadgets.example.com", ""))},
	}
	result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil, crds)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)
	require.NotEmpty(t, result.ChartPath)

	ch, err := loader.Load(result.ChartPath)
	require.NoError(t, err)
	got := ch.CRDObjects()
	require.Len(t, got, 2)
	byName := make(map[string]string, len(got))
	for _, crd := range got {
		byName[crd.Name] = string(crd.File.Data)
	}
	assert.Equal(t, widgetCRD("widgets.example.com", ""), byName["crds/widgets.yaml"])
	assert.Equal(t, widgetCRD("gadgets.example.com", ""), byName["crds/types.yaml"])
}

func TestCrdFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "yaml basename", path: ".deployah/crds/widgets.yaml", want: "widgets.yaml"},
		{name: "yml basename", path: "gadgets.yml", want: "gadgets.yml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := crdFileName(tc.path)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCrdFileName_Rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "parent directory", path: "..", wantErr: "unsafe file name"},
		{name: "dot path", path: ".", wantErr: "unsafe file name"},
		{name: "non yaml extension", path: "widgets.txt", wantErr: "file name must end in .yaml or .yml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := crdFileName(tc.path)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
