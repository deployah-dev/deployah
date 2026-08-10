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

package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
)

func TestRequiredAPIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		manifest    *spec.Spec
		environment string
		resolved    *spec.ResolvedSpec
		wantGVs     []string
		wantEmpty   bool
	}{
		{
			name: "metrics enabled",
			manifest: &spec.Spec{
				Components: map[string]spec.Component{
					"worker": {
						Role:    spec.ComponentRoleWorker,
						Metrics: &spec.ComponentMetrics{Port: 9090},
					},
				},
			},
			environment: "production",
			wantGVs:     []string{"monitoring.coreos.com/v1"},
		},
		{
			name: "expose requires networking",
			manifest: &spec.Spec{
				Components: map[string]spec.Component{
					"api": {Expose: &spec.Expose{}},
				},
			},
			environment: "production",
			wantGVs:     []string{"networking.k8s.io/v1"},
		},
		{
			name: "inactive environment filter skips requirements",
			manifest: &spec.Spec{
				Components: map[string]spec.Component{
					"api": {
						Environments: []string{"staging"},
						Metrics:      &spec.ComponentMetrics{},
					},
				},
			},
			environment: "production",
			wantEmpty:   true,
		},
		{
			name: "cert-manager from resolved TLS mode",
			manifest: &spec.Spec{
				Components: map[string]spec.Component{
					"api": {Expose: &spec.Expose{}},
				},
			},
			environment: "production",
			resolved: &spec.ResolvedSpec{
				Components: map[string]spec.ResolvedComponent{
					"api": {TLSMode: spec.TLSModeCertManager},
				},
			},
			wantGVs: []string{"networking.k8s.io/v1", "cert-manager.io/v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reqs := RequiredAPIs(tt.manifest, tt.environment, tt.resolved)
			if tt.wantEmpty {
				assert.Empty(t, reqs)
				return
			}
			got := map[string]struct{}{}
			for _, req := range reqs {
				for _, gv := range req.GroupVersions {
					got[gv] = struct{}{}
				}
			}
			for _, gv := range tt.wantGVs {
				_, ok := got[gv]
				require.True(t, ok, "missing GroupVersion %s in %#v", gv, reqs)
			}
		})
	}
}
