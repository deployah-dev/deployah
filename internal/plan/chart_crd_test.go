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
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/extras"
)

func widgetCRDDoc(name, yamlBody string) extras.CRDDoc {
	return extras.CRDDoc{
		Path: "/abs/.deployah/crds/widget.yaml",
		Kind: "CustomResourceDefinition",
		Name: name,
		YAML: []byte(yamlBody),
	}
}

func TestStampChartCRDs_Lifecycle(t *testing.T) {
	t.Parallel()
	yamlBody := "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"
	docs := []extras.CRDDoc{widgetCRDDoc("widgets.example.com", yamlBody)}
	tests := []struct {
		name        string
		upgrade     bool
		skipCRDs    bool
		wantLife    ChartCRDLifecycle
		wantProcess bool
		wantChange  bool
	}{
		{name: "fresh install processes", wantLife: ChartCRDProcess, wantProcess: true, wantChange: true},
		{name: "fresh install skip", skipCRDs: true, wantLife: ChartCRDSkip, wantChange: false},
		{name: "upgrade ignores skip", upgrade: true, skipCRDs: true, wantLife: ChartCRDUpgrade, wantChange: false},
		{name: "upgrade does not process", upgrade: true, wantLife: ChartCRDUpgrade, wantChange: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &Plan{}
			StampChartCRDs(p, docs, tc.upgrade, tc.skipCRDs)
			require.Len(t, p.ChartCRDs, 1)
			got := p.ChartCRDs[0]
			assert.Equal(t, "CustomResourceDefinition", got.Kind)
			assert.Equal(t, "widgets.example.com", got.Name)
			assert.Equal(t, ".deployah/crds/widget.yaml", got.Source)
			assert.Equal(t, tc.wantLife, got.Lifecycle)
			assert.Equal(t, tc.wantProcess, got.WillProcess)
			assert.Equal(t, yamlBody, got.YAML)
			assert.Equal(t, tc.wantChange, p.HasChanges())
		})
	}
}

func TestHasChanges_ChartCRDsDoNotOverrideResourceChanges(t *testing.T) {
	t.Parallel()
	p := &Plan{Changes: []Change{{Action: ActionAdd, Kind: "ConfigMap", Name: "app"}}}
	StampChartCRDs(p, []extras.CRDDoc{widgetCRDDoc("widgets.example.com", "kind: CustomResourceDefinition\n")}, true, false)
	assert.True(t, p.HasChanges())
	assert.Equal(t, ChartCRDUpgrade, p.ChartCRDs[0].Lifecycle)
}

func TestHasChanges_FreshInstallResourcesAndCRDs(t *testing.T) {
	t.Parallel()
	p := &Plan{Changes: []Change{{Action: ActionAdd, Kind: "ConfigMap", Name: "app"}}}
	StampChartCRDs(p, []extras.CRDDoc{widgetCRDDoc("widgets.example.com", "kind: CustomResourceDefinition\n")}, false, false)
	assert.True(t, p.HasChanges())
	assert.True(t, p.ChartCRDs[0].WillProcess)
}

func TestRenderText_ChartCRDs(t *testing.T) {
	t.Parallel()
	yamlBody := "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"
	docs := []extras.CRDDoc{widgetCRDDoc("widgets.example.com", yamlBody)}
	tests := []struct {
		name        string
		upgrade     bool
		skipCRDs    bool
		contains    []string
		notContains []string
	}{
		{
			name:     "fresh install shows identity and yaml",
			contains: []string{"+ CustomResourceDefinition/widgets.example.com", "Helm install will process this chart CRD", "kind: CustomResourceDefinition", "name: widgets.example.com"},
			notContains: []string{
				"will definitely be created",
				"No changes.",
			},
		},
		{
			name:        "skip does not show pending apply",
			skipCRDs:    true,
			contains:    []string{"CustomResourceDefinition/widgets.example.com", "install-time CRD processing disabled", "No changes."},
			notContains: []string{"+ CustomResourceDefinition/", "Helm install will process this chart CRD"},
		},
		{
			name:        "upgrade is not a kubernetes change",
			upgrade:     true,
			contains:    []string{"CustomResourceDefinition/widgets.example.com", "Helm upgrade will not process this chart CRD", "No changes."},
			notContains: []string{"+ CustomResourceDefinition/", yamlBody},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &Plan{Header: Header{Project: "web", Environment: "prod", Release: "web", FreshInstall: !tc.upgrade}}
			StampChartCRDs(p, docs, tc.upgrade, tc.skipCRDs)
			var buf strings.Builder
			require.NoError(t, RenderText(&buf, p, TextOptions{}))
			got := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, got, s)
			}
			for _, s := range tc.notContains {
				assert.NotContains(t, got, s)
			}
		})
	}
}

func TestRenderText_MultipleChartCRDs(t *testing.T) {
	t.Parallel()
	p := &Plan{Header: Header{FreshInstall: true}}
	StampChartCRDs(p, []extras.CRDDoc{
		{Path: "types.yaml", Index: 0, Kind: "CustomResourceDefinition", Name: "one.example.com", YAML: []byte("kind: CustomResourceDefinition\nmetadata:\n  name: one.example.com\n")},
		{Path: "types.yaml", Index: 1, Kind: "CustomResourceDefinition", Name: "two.example.com", YAML: []byte("kind: CustomResourceDefinition\nmetadata:\n  name: two.example.com\n")},
	}, false, false)
	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))
	got := buf.String()
	assert.Contains(t, got, "+ CustomResourceDefinition/one.example.com")
	assert.Contains(t, got, "+ CustomResourceDefinition/two.example.com")
	assert.Contains(t, got, "name: one.example.com")
	assert.Contains(t, got, "name: two.example.com")
}

func TestRenderJSON_ChartCRDs(t *testing.T) {
	t.Parallel()
	docs := []extras.CRDDoc{widgetCRDDoc("widgets.example.com", "kind: CustomResourceDefinition\n")}
	tests := []struct {
		name        string
		upgrade     bool
		skipCRDs    bool
		wantLife    string
		wantProcess bool
	}{
		{name: "fresh install", wantLife: "process", wantProcess: true},
		{name: "skip", skipCRDs: true, wantLife: "skip"},
		{name: "upgrade", upgrade: true, wantLife: "upgrade"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &Plan{Header: Header{Project: "web"}}
			StampChartCRDs(p, docs, tc.upgrade, tc.skipCRDs)
			var buf strings.Builder
			require.NoError(t, RenderJSON(&buf, p))
			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(buf.String()), &doc))
			assert.Equal(t, "1.1", doc["format_version"])
			crds, ok := doc["chart_crds"].([]any)
			require.True(t, ok)
			require.Len(t, crds, 1)
			entry, ok := crds[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "CustomResourceDefinition", entry["kind"])
			assert.Equal(t, "widgets.example.com", entry["name"])
			assert.Equal(t, tc.wantLife, entry["lifecycle"])
			assert.Equal(t, tc.wantProcess, entry["will_process"])
			assert.NotContains(t, entry, "action")
			assert.NotContains(t, entry, "api_version")
			assert.NotContains(t, buf.String(), "Helm install will process")
		})
	}
}

func TestRenderText_UnknownChartCRDLifecycleDoesNotClaimProcess(t *testing.T) {
	t.Parallel()
	p := &Plan{ChartCRDs: []ChartCRD{{
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
	}}}
	var buf strings.Builder
	require.NoError(t, RenderText(&buf, p, TextOptions{}))
	got := buf.String()
	assert.Contains(t, got, "CustomResourceDefinition/widgets.example.com")
	assert.Contains(t, got, "chart CRD lifecycle is not known")
	assert.NotContains(t, got, "Helm install will process this chart CRD")
}
