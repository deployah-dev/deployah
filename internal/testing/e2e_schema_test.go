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

package testing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

func TestE2ESchema_compiles(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateE2EDocument(map[string]any{
		"env": "dev",
		"steps": []any{
			map[string]any{"deploy": map[string]any{}},
		},
	}))
}

func TestE2ESchema_documents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		doc  string
	}{
		{name: "deploy", doc: "env: dev\nsteps:\n  - deploy: {}\n"},
		{name: "plan", doc: `
env: dev
steps:
  - plan:
      detailedExitCode: true
      output:
        exitCode: 2
      expr:
        - 'plan.summary.create > 0'
`},
		{name: "run", doc: "env: dev\nsteps:\n  - run:\n      task: migrate\n"},
		{name: "logs", doc: "env: dev\nsteps:\n  - logs:\n      component: web\n      contains: ['ready']\n"},
		{name: "delete", doc: "env: dev\nsteps:\n  - delete: {}\n"},
		{name: "assert", doc: `
env: dev
steps:
  - assert:
      - select:
          apiVersion: apps/v1
          kind: Deployment
          name: web
        expect:
          spec:
            replicas: 2
        expr:
          - 'objects.size() == 1'
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateE2EDocument(mustYAML(t, tt.doc)))
		})
	}
}

func TestE2ESchema_rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{name: "unknown property", doc: "env: dev\nextra: true\nsteps:\n  - deploy: {}\n", wantErr: "additional"},
		{name: "unknown operation", doc: "env: dev\nsteps:\n  - bounce: {}\n", wantErr: "additional"},
		{name: "two operations", doc: "env: dev\nsteps:\n  - deploy: {}\n    run:\n      task: x\n", wantErr: "'oneOf' failed"},
		{name: "missing env", doc: "steps:\n  - deploy: {}\n", wantErr: "env"},
		{name: "empty steps", doc: "env: dev\nsteps: []\n", wantErr: "minItems"},
		{name: "empty assert", doc: "env: dev\nsteps:\n  - assert: []\n", wantErr: "minItems"},
		{name: "selector missing kind", doc: `
env: dev
steps:
  - assert:
      - select:
          apiVersion: v1
        expr: ['objects.size() == 0']
`, wantErr: "kind"},
		{name: "missing expect and expr", doc: `
env: dev
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
`, wantErr: "missing property 'expect'"},
		{name: "negative exit", doc: `
env: dev
steps:
  - deploy:
      output:
        exitCode: -1
`, wantErr: "minimum"},
		{name: "stopOnFailure type", doc: "env: dev\nsteps:\n  - deploy: {}\n    stopOnFailure: yes-please\n", wantErr: "stopOnFailure"},
		{name: "empty expr", doc: `
env: dev
steps:
  - plan:
      expr: []
`, wantErr: "minItems"},
		{name: "old resources shorthand", doc: `
env: dev
resources:
  - match:
      apiVersion: v1
      kind: ConfigMap
`, wantErr: "additional"},
		{name: "old stderrContains", doc: "env: dev\nsteps:\n  - deploy: {}\n    stderrContains: Deployed\n", wantErr: "additional"},
		{name: "old minCount", doc: `
env: dev
steps:
  - assert:
      - minCount: 0
        select:
          apiVersion: v1
          kind: ConfigMap
        expr: ['true']
`, wantErr: "additional"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateE2EDocument(mustYAML(t, tt.doc))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestE2ESchema_scenarioFiles(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(TestScenariosDir); err != nil {
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
			require.NoError(t, ValidateE2EDocument(mustYAML(t, string(raw))))
		})
	}
	require.Greater(t, found, 0)
}

func mustYAML(t *testing.T, doc string) any {
	t.Helper()
	raw, err := yaml.YAMLToJSON([]byte(doc))
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestLoadE2EFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(specNginxLatest), 0o600))
	body := []byte(`
env: dev
steps:
  - deploy:
      spec: deployah.yaml
  - assert:
      - select:
          apiVersion: apps/v1
          kind: Deployment
          name: demo-dev
        expr:
          - 'objects.size() == 1'
`)
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	assert.Equal(t, "dev", fx.Env)
	assert.True(t, fx.RunParallel())
	require.Len(t, fx.Steps, 2)
	assert.Equal(t, "deploy", fx.Steps[0].OpName())
	assert.Equal(t, "assert", fx.Steps[1].OpName())
	assert.False(t, fx.Steps[1].Assert[0].StopOnFailureEnabled())
	assert.True(t, fx.Steps[0].StopOnFailureEnabled())
}

func TestLoadE2EFixture_unknownImage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(`
apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: ghcr.io/acme/not-allowed:1
`), 0o600))
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte("env: dev\nsteps:\n  - deploy: {}\n"), 0o600))
	_, err := LoadE2EFixture(path, dir)
	require.ErrorContains(t, err, "allowlist")
}

func TestLoadE2EFixture_invalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte(":\n  - ["), 0o600))
	_, err := LoadE2EFixture(path, dir)
	require.Error(t, err)
	var cfg *ConfigError
	require.ErrorAs(t, err, &cfg)
	assert.Equal(t, path, cfg.File)
}

func TestImageAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		image string
		want  bool
	}{
		{name: "docker hub library prefix", image: "docker.io/library/nginx:latest", want: true},
		{name: "unknown tag", image: "nginx:not-a-real-tag", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, imageAllowed(tt.image))
		})
	}
}
