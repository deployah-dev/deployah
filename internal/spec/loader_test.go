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

package spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// TestResolveEnvironment verifies the registry rules: the platform config
// owns which environment names are valid when present, the spec map is the
// fallback registry, and spec entries act as optional overrides.
func TestResolveEnvironment(t *testing.T) {
	t.Parallel()

	specEnvs := map[string]Environment{
		"staging": {EnvFile: ".env.staging"},
	}
	platform := &PlatformConfig{
		Environments: map[string]PlatformEnvironment{
			"local":      {Context: "kind-deployah"},
			"production": {Context: "prod-eks"},
		},
	}
	singleEnvPlatform := &PlatformConfig{
		Environments: map[string]PlatformEnvironment{
			"local": {Context: "kind-deployah"},
		},
	}

	tests := []struct {
		name         string
		environments map[string]Environment
		platform     *PlatformConfig
		desired      string
		wantName     string
		wantEnvFile  string
		wantErr      string
	}{
		{
			name:     "platform registry accepts registered env without spec entry",
			platform: platform,
			desired:  "production",
			wantName: "production",
		},
		{
			name:         "platform registry rejects env only in spec",
			environments: specEnvs,
			platform:     platform,
			desired:      "staging",
			wantErr:      "not found in the platform file",
		},
		{
			name:         "spec entry supplies overrides for registered env",
			environments: map[string]Environment{"production": {EnvFile: ".env.prod"}},
			platform:     platform,
			desired:      "production",
			wantName:     "production",
			wantEnvFile:  ".env.prod",
		},
		{
			name:     "wildcard deploy matches platform key by prefix",
			platform: platform,
			desired:  "production/eu",
			wantName: "production",
		},
		{
			name:     "single platform env auto-selects when none specified",
			platform: singleEnvPlatform,
			desired:  "",
			wantName: "local",
		},
		{
			name:     "multiple platform envs require an explicit choice",
			platform: platform,
			desired:  "",
			wantErr:  "multiple environments found in the platform file",
		},
		{
			name:         "spec registry applies when no platform file",
			environments: specEnvs,
			desired:      "staging",
			wantName:     "staging",
			wantEnvFile:  ".env.staging",
		},
		{
			name:         "spec registry rejects unknown env when no platform file",
			environments: specEnvs,
			desired:      "production",
			wantErr:      "not found in the spec",
		},
		{
			name:        "no registry accepts any name as free-form",
			desired:     "qa",
			wantName:    "qa",
			wantEnvFile: "",
		},
		{
			name:        "no registry and no desired env yields synthetic default",
			desired:     "",
			wantName:    "default",
			wantEnvFile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, env, err := ResolveEnvironment(tt.environments, tt.platform, tt.desired)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, name)
			require.NotNil(t, env)
			assert.Equal(t, tt.wantEnvFile, env.EnvFile)
		})
	}
}

// TestLoad_NoEnvironmentsSection verifies a spec without an environments
// section loads: the section is optional now that the platform file owns
// the registry, and an entry only adds developer overrides.
func TestLoad_NoEnvironmentsSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, "deployah.yaml")
	doc := `apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: nginx:1.27
    port: 8080
`
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))

	m, err := Load(t.Context(), path, "", nil)
	require.NoError(t, err)
	assert.Empty(t, m.Environments)

	// An explicit free-form name is accepted when no registry exists.
	m, err = Load(t.Context(), path, "qa", nil)
	require.NoError(t, err)
	require.NotNil(t, m)
}

// TestSanitizeEnvName verifies sanitizeEnvName removes path separators and
// wildcards from environment names.
func TestSanitizeEnvName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name unchanged",
			input:    "production",
			expected: "production",
		},
		{
			name:     "wildcard suffix removed",
			input:    "review/*",
			expected: "review",
		},
		{
			name:     "forward slash removed",
			input:    "feature/branch",
			expected: "featurebranch",
		},
		{
			name:     "backslash removed",
			input:    "windows\\path",
			expected: "windowspath",
		},
		{
			name:     "asterisk removed",
			input:    "stage*",
			expected: "stage",
		},
		{
			name:     "question mark removed",
			input:    "test?env",
			expected: "testenv",
		},
		{
			name:     "multiple special chars removed",
			input:    "dev/*/test?env",
			expected: "devtestenv",
		},
		{
			name:     "complex wildcard pattern",
			input:    "feature/pr-123/*",
			expected: "featurepr-123",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeEnvName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestLoad_EnvFileRelativeToSpecDir loads a spec whose .env.dev lives next
// to the spec file, while the process cwd is elsewhere (no [os.Chdir]).
func TestLoad_EnvFileRelativeToSpecDir(t *testing.T) {
	t.Parallel()

	specDir := t.TempDir()
	specYAML := `apiVersion: v1-alpha.5
project: withdir
components:
  web:
    image: ${IMAGE}
    port: 80
    environments: [dev]
environments:
  dev:
    variables:
      IMAGE: nginx:1.27
`
	require.NoError(t, os.WriteFile(filepath.Join(specDir, "deployah.yaml"), []byte(specYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(specDir, ".env.dev"), []byte("DPY_VAR_IMAGE=ignored\n"), 0o600))

	got, err := Load(t.Context(), filepath.Join(specDir, "deployah.yaml"), "dev", nil)
	require.NoError(t, err)
	require.Contains(t, got.Components, "web")
	assert.Equal(t, "nginx:1.27", got.Components["web"].Image)
}

// TestSave_WritesParseableYAML verifies Save writes a spec that round-trips
// through YAML unmarshaling.
func TestSave_WritesParseableYAML(t *testing.T) {
	t.Chdir(t.TempDir())

	s := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]Component{
			"web": {Image: "nginx:latest"},
		},
	}

	require.NoError(t, Save(s, "deployah.yaml"))

	data, err := os.ReadFile("deployah.yaml")
	require.NoError(t, err)

	var got Spec
	require.NoError(t, yaml.Unmarshal(data, &got))
	assert.Equal(t, s.Project, got.Project)
	assert.Equal(t, s.APIVersion, got.APIVersion)
	assert.Contains(t, got.Components, "web")
}

// TestSave_CreatesParentDirectory verifies Save creates missing parent
// directories before writing.
func TestSave_CreatesParentDirectory(t *testing.T) {
	t.Chdir(t.TempDir())

	s := &Spec{APIVersion: CurrentManifestVersion, Project: "shop"}
	path := filepath.Join("nested", "dir", "deployah.yaml")

	require.NoError(t, Save(s, path))

	_, err := os.Stat(path)
	require.NoError(t, err)
}

// TestSave_AtomicNoLeftoverTempFiles verifies Save writes via an atomic
// rename, leaving no leftover temp files behind.
func TestSave_AtomicNoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	first := &Spec{APIVersion: CurrentManifestVersion, Project: "first-project"}
	require.NoError(t, Save(first, "deployah.yaml"))

	second := &Spec{APIVersion: CurrentManifestVersion, Project: "second-project"}
	require.NoError(t, Save(second, "deployah.yaml"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected only the final deployah.yaml, no leftover temp files")
	assert.Equal(t, "deployah.yaml", entries[0].Name())

	data, err := os.ReadFile("deployah.yaml")
	require.NoError(t, err)
	var got Spec
	require.NoError(t, yaml.Unmarshal(data, &got))
	assert.Equal(t, "second-project", got.Project, "second save must fully replace, not append to, the first")
}

// TestParseManifest_ProfilesArray verifies the profiles array is parsed.
func TestParseManifest_ProfilesArray(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.yaml")
	content := `
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: nginx:1.0.0
    port: 80
    profiles: [public-web, high-security]
environments:
  production: {}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	s, _, err := ParseManifest(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"public-web", "high-security"}, s.Components["web"].Profiles)
}

// TestLoad_OldProfileStringRejected verifies the singular profile field is
// rejected by the manifest schema.
func TestLoad_OldProfileStringRejected(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	content := `
apiVersion: v1-alpha.5
project: shop
components:
  web:
    image: nginx:1.0.0
    port: 80
    profile: public-web
environments:
  production: {}
`
	require.NoError(t, os.WriteFile("deployah.yaml", []byte(content), 0o600))
	_, err := Load(t.Context(), "deployah.yaml", "production", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile")
}

const hookCycleSpecYAML = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
    env:
      LOG_LEVEL: debug
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [seed]
    command: [migrate]
  seed:
    from: api
    "on": preDeploy
    after: [migrate]
    command: [seed]
environments:
  staging: {}
`

func TestLoad_HookCycleIsHardError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(path, []byte(hookCycleSpecYAML), 0o600))

	_, err := Load(t.Context(), path, "staging", nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "cycle")
}

func TestLoad_AllowHookCycleForDisplay_DefersCycle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(path, []byte(hookCycleSpecYAML), 0o600))

	got, err := Load(t.Context(), path, "staging", nil, AllowHookCycleForDisplay())
	require.NoError(t, err)
	require.Contains(t, got.Tasks, "migrate")
	require.Contains(t, got.Tasks, "seed")
}

func TestLoad_AllowHookCycleForDisplay_DefersSelfCycle(t *testing.T) {
	t.Parallel()

	const specYAML = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [migrate]
    command: [migrate]
environments:
  staging: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(path, []byte(specYAML), 0o600))

	got, err := Load(t.Context(), path, "staging", nil, AllowHookCycleForDisplay())
	require.NoError(t, err)
	require.Contains(t, got.Tasks, "migrate")
}

func TestLoad_AllowHookCycleForDisplay_StillRejectsInvalidAfter(t *testing.T) {
	t.Parallel()

	const specYAML = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [missing]
    command: [migrate]
environments:
  staging: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(path, []byte(specYAML), 0o600))

	_, err := Load(t.Context(), path, "staging", nil, AllowHookCycleForDisplay())
	require.Error(t, err)
	assert.ErrorContains(t, err, "does not name a task")
}

func TestLoad_AllowHookCycleForDisplay_StillRejectsWrongAfterPhase(t *testing.T) {
	t.Parallel()

	const specYAML = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [seed]
    command: [migrate]
  seed:
    from: api
    "on": postDeploy
    command: [seed]
environments:
  staging: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(path, []byte(specYAML), 0o600))

	_, err := Load(t.Context(), path, "staging", nil, AllowHookCycleForDisplay())
	require.Error(t, err)
	assert.ErrorContains(t, err, "not in the same on phase")
}
