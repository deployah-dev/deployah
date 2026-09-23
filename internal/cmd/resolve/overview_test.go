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

package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"deployah.dev/deployah/internal/spec"
)

func TestBuildEnvironmentOverview(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rawSpec    *spec.Spec
		platform   *spec.PlatformConfig
		currentCtx string
		want       []envOverviewRow
	}{
		{
			name: "platform and spec environments",
			rawSpec: &spec.Spec{
				Environments: map[string]spec.Environment{
					"production": {Variables: spec.StringMap{"TAG": "v1"}},
					"qa":         {EnvFile: ".env.qa"},
				},
			},
			platform: &spec.PlatformConfig{
				Environments: map[string]spec.PlatformEnvironment{
					"local": {
						Context: "kind-deployah",
						Domains: map[string]spec.PlatformDomain{
							"public": {BaseDomain: "127.0.0.1.nip.io"},
						},
					},
					"production": {},
				},
			},
			currentCtx: "minikube",
			want: []envOverviewRow{
				{
					Name:       "local",
					Source:     "platform",
					Deployable: true,
					Context:    "kind-deployah",
					Domains:    []string{"public"},
				},
				{
					Name:            "production",
					Source:          "platform",
					Deployable:      true,
					ContextFallback: "minikube",
					Overrides:       []string{"variables"},
				},
				{
					Name:      "qa",
					Source:    "spec-only",
					Overrides: []string{"envFile"},
				},
			},
		},
		{
			name: "no platform file",
			rawSpec: &spec.Spec{
				Environments: map[string]spec.Environment{"staging": {}},
			},
			currentCtx: "minikube",
			want: []envOverviewRow{
				{
					Name:            "staging",
					Source:          "spec",
					Deployable:      true,
					ContextFallback: "minikube",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, buildEnvironmentOverview(tt.rawSpec, tt.platform, tt.currentCtx))
		})
	}
}
