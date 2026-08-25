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

package testing

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const specNginxLatest = `
apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: nginx:latest
`

func TestLoadE2EFixture(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		spec         string
		e2e          string
		wantEnv      string
		wantParallel bool
		wantNRes     int
		wantCount    int
	}{
		{
			name: "simple deployment",
			spec: specNginxLatest,
			e2e: `
env: dev
resources:
  - match:
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: demo-dev
`,
			wantEnv:      "dev",
			wantParallel: true,
			wantNRes:     1,
			wantCount:    1,
		},
		{
			name: "docker.io library prefix allowed",
			spec: `
apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: docker.io/library/nginx:latest
`,
			e2e: `
env: dev
resources:
  - match:
      apiVersion: apps/v1
      kind: Deployment
      metadata: {name: x}
`,
			wantEnv:      "dev",
			wantParallel: true,
			wantNRes:     1,
			wantCount:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(tt.spec), 0o600))
			path := filepath.Join(dir, "e2e.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.e2e), 0o600))

			fx, err := LoadE2EFixture(path, dir)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnv, fx.Env)
			assert.Equal(t, tt.wantParallel, fx.RunParallel())
			require.Len(t, fx.Resources, tt.wantNRes)
			assert.Equal(t, tt.wantCount, fx.Resources[0].Count())
		})
	}
}

func TestLoadE2EFixture_Stepped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		spec         string
		e2e          string
		wantEnv      string
		wantParallel bool
		wantOps      []string
		wantCount    int
	}{
		{
			name: "four closed ops",
			spec: specNginxLatest,
			e2e: `
env: staging
parallel: false
steps:
  - deploy:
      spec: deployah.yaml
      args: [--crds, create]
    stderrContains: already present
  - run:
      task: backfill
  - logs:
      component: web
      contains: listening
  - delete: {}
    resources:
      - minCount: 0
        match:
          apiVersion: batch/v1
          kind: Job
          metadata:
            labels:
              app: demo
`,
			wantEnv:      "staging",
			wantParallel: false,
			wantOps:      []string{"deploy", "run", "logs", "delete"},
			wantCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(tt.spec), 0o600))
			path := filepath.Join(dir, "e2e.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.e2e), 0o600))

			fx, err := LoadE2EFixture(path, dir)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnv, fx.Env)
			assert.Equal(t, tt.wantParallel, fx.RunParallel())
			require.Len(t, fx.Steps, len(tt.wantOps))
			for i, op := range tt.wantOps {
				assert.Equal(t, op, fx.Steps[i].OpName())
			}
			require.NotEmpty(t, fx.Steps[len(fx.Steps)-1].Resources)
			assert.Equal(t, tt.wantCount, fx.Steps[len(fx.Steps)-1].Resources[0].Count())
		})
	}
}

func TestLoadE2EFixture_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    string
		e2e     string
		wantErr string
	}{
		{
			name: "unknown image",
			spec: `
apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: ghcr.io/acme/not-allowed:1
`,
			e2e: `
env: dev
resources:
  - match:
      kind: Deployment
      apiVersion: apps/v1
      metadata:
        name: x
`,
			wantErr: "allowlist",
		},
		{
			name: "mixed resources and steps",
			e2e: `
env: dev
resources:
  - match:
      kind: Deployment
      apiVersion: apps/v1
      metadata: {name: x}
steps:
  - deploy: {}
`,
			wantErr: "exactly one of resources or steps",
		},
		{
			name: "missing env",
			e2e: `
resources:
  - match:
      apiVersion: apps/v1
      kind: Deployment
      metadata: {name: x}
`,
			wantErr: "env is required",
		},
		{
			name: "match missing kind",
			e2e: `
env: dev
resources:
  - match:
      apiVersion: apps/v1
      metadata: {name: x}
`,
			wantErr: "match requires apiVersion and kind",
		},
		{
			name: "named match with minCount 2",
			e2e: `
env: dev
resources:
  - minCount: 2
    match:
      apiVersion: apps/v1
      kind: Deployment
      metadata: {name: x}
`,
			wantErr: "minCount > 1 cannot be used with metadata.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.spec != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(tt.spec), 0o600))
			}
			path := filepath.Join(dir, "e2e.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.e2e), 0o600))

			_, err := LoadE2EFixture(path, dir)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestImageAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		img  string
		want bool
	}{
		{name: "nginx latest", img: "nginx:latest", want: true},
		{name: "docker.io library prefix", img: "docker.io/library/nginx:latest", want: true},
		{name: "docker.io prefix", img: "docker.io/nginx:latest", want: true},
		{name: "redis alpine", img: "redis:7-alpine", want: true},
		{name: "unknown registry", img: "ghcr.io/acme/not-allowed:1", want: false},
		{name: "unknown tag", img: "nginx:not-a-real-tag", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, imageAllowed(tt.img))
		})
	}
}

func TestE2ESchema_validatesScenarioFiles(t *testing.T) {
	t.Parallel()

	schemaBytes, err := os.ReadFile("e2e.schema.json")
	require.NoError(t, err)

	compiler := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	require.NoError(t, err)
	require.NoError(t, compiler.AddResource("e2e.schema.json", doc))
	compiled, err := compiler.Compile("e2e.schema.json")
	require.NoError(t, err)

	if _, statErr := os.Stat(TestScenariosDir); statErr != nil {
		t.Skip("scenarios directory not found")
	}
	scenarios, err := DiscoverScenarios(TestScenariosDir)
	require.NoError(t, err)
	found := 0
	for _, sc := range scenarios {
		if !sc.HasE2EFixture {
			continue
		}
		found++
		t.Run(sc.Name, func(t *testing.T) {
			t.Parallel()
			raw, readErr := os.ReadFile(sc.E2EFixturePath)
			require.NoError(t, readErr)
			jsonBytes, yamlErr := yaml.YAMLToJSON(raw)
			require.NoError(t, yamlErr)
			var obj any
			require.NoError(t, json.Unmarshal(jsonBytes, &obj))
			require.NoError(t, compiled.Validate(obj))
		})
	}
	require.Greater(t, found, 0, "expected at least one scenarios/*/e2e.yaml")
}
