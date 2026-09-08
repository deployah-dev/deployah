// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package helm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/spec"
)

func TestPrepareChart_RequiresResolvedSpecSource(t *testing.T) {
	t.Parallel()

	_, err := PrepareChart(t.Context(), &spec.ResolvedSpec{}, NewChartCache(time.Hour))
	require.Error(t, err)
	assert.ErrorContains(t, err, "resolved spec source")
}

func TestPrepareChart_RequiresResolveResult(t *testing.T) {
	t.Parallel()

	_, err := PrepareChart(t.Context(), &spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "shop"},
	}, NewChartCache(time.Hour))
	require.Error(t, err)
	assert.ErrorContains(t, err, "spec.Resolve")
}

func TestPrepareChart_ActiveComponentsComeFromResolved(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api":    serviceComponent(),
			"worker": {Role: spec.ComponentRoleWorker, Image: "worker:1.0.0"},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	resolved, _, err := spec.Resolve(m, nil, spec.NormalizeEnv("production"), spec.SubstitutionReport{})
	require.NoError(t, err)
	require.Contains(t, resolved.Components, "worker")
	delete(resolved.Components, "worker")

	cache := NewChartCache(time.Hour)
	chartDir, err := PrepareChart(t.Context(), resolved, cache)
	require.NoError(t, err)
	t.Cleanup(func() { removeChartDirs(t, cache, resolved, "production", chartDir) })

	assert.DirExists(t, filepath.Join(chartDir, "charts", "api"))
	assert.NoDirExists(t, filepath.Join(chartDir, "charts", "worker"))

	vals := readChartValues(t, chartDir)
	_, hasAPI := vals["api"]
	_, hasWorker := vals["worker"]
	assert.True(t, hasAPI)
	assert.False(t, hasWorker)
}

func readChartValues(t *testing.T, chartDir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(chartDir, "values.yaml")) // #nosec G304 -- chartDir is a test temp path
	require.NoError(t, err)
	vals := map[string]any{}
	require.NoError(t, yaml.Unmarshal(raw, &vals))
	return vals
}
