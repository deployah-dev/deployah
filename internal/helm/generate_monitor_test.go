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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/spec"
)

func TestRenderOffline_EnabledMetricsAlwaysRenderMonitors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		metrics   bool
		wantKinds []string
		omitKinds []string
	}{
		{
			name:      "metrics enabled render ServiceMonitor and PodMonitor",
			metrics:   true,
			wantKinds: []string{"ServiceMonitor", "PodMonitor"},
		},
		{
			name:      "metrics disabled omit monitors",
			metrics:   false,
			omitKinds: []string{"ServiceMonitor", "PodMonitor"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := spec.Component{
				Role:     spec.ComponentRoleService,
				Image:    "ghcr.io/acme/api:1.0.0",
				Port:     8080,
				Profiles: []string{"observability"},
			}
			worker := spec.Component{
				Role:     spec.ComponentRoleWorker,
				Image:    "ghcr.io/acme/worker:1.0.0",
				Profiles: []string{"observability"},
			}
			if tc.metrics {
				api.Metrics = &spec.ComponentMetrics{}
				worker.Metrics = &spec.ComponentMetrics{Port: 9090}
			}

			manifest := &spec.Spec{
				APIVersion: spec.CurrentManifestVersion,
				Project:    "shop",
				SpecDir:    t.TempDir(),
				Environments: map[string]spec.Environment{
					"dev": {},
				},
				Components: map[string]spec.Component{
					"api":    api,
					"worker": worker,
				},
			}
			require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
			platform := &spec.PlatformConfig{
				APIVersion: spec.CurrentPlatformVersion,
				Profiles: map[string]spec.PlatformProfile{
					"observability": {
						Metrics: &spec.ProfileMetrics{
							MonitorLabels: map[string]string{"release": "prom"},
						},
					},
				},
				Environments: map[string]spec.PlatformEnvironment{
					"dev": {Context: "kind"},
				},
			}
			resolved, _, err := spec.Resolve(manifest, platform, spec.NormalizeEnv("dev"), spec.SubstitutionReport{})
			require.NoError(t, err)

			client, err := NewClient(WithNamespace("default"))
			require.NoError(t, err)
			result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil, nil)
			require.NoError(t, err)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}

			kinds := renderedResourceKinds(t, result.Manifest)
			for _, kind := range tc.wantKinds {
				assert.Contains(t, kinds, kind)
			}
			for _, kind := range tc.omitKinds {
				assert.NotContains(t, kinds, kind)
			}
		})
	}
}

func renderedResourceKinds(t *testing.T, manifest string) []string {
	t.Helper()
	var kinds []string
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var meta struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil || meta.Kind == "" {
			continue
		}
		kinds = append(kinds, meta.Kind)
	}
	return kinds
}
