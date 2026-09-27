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

package helmfixture_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/testing/helmfixture"
)

func TestRender_FreshInstall(t *testing.T) {
	t.Parallel()

	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    t.TempDir(),
		Environments: map[string]spec.Environment{
			"dev": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Port:  8080,
			},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	resolved, _, err := spec.Resolve(manifest, nil, spec.NormalizeEnv("dev"), spec.SubstitutionReport{})
	require.NoError(t, err)

	result, err := helmfixture.Render(t, "default", resolved, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, helm.GenerateReleaseName("shop", "dev"), result.ReleaseName)
	assert.Equal(t, "default", result.Namespace)
	assert.False(t, result.IsUpgrade)
	assert.Equal(t, 1, result.Revision)
	_, statErr := os.Stat(result.ChartPath)
	require.NoError(t, statErr)
	assert.Contains(t, result.Manifest, "kind: Deployment")
}

func TestRender_RejectsUnresolved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resolved *spec.ResolvedSpec
		wantErr  string
	}{
		{name: "nil", wantErr: "render requires resolved spec"},
		{name: "empty", resolved: &spec.ResolvedSpec{}, wantErr: "render requires resolved spec source"},
		{
			name:     "missing resolve",
			resolved: &spec.ResolvedSpec{Spec: &spec.Spec{Project: "shop"}},
			wantErr:  "spec.Resolve",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := helmfixture.Render(t, "default", tt.resolved, nil, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
