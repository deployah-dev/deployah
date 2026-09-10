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

package workspace_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/workspace"
)

const minimalSpecYAML = `apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: nginx:1.27
    port: 8080
`

const specWithStagingYAML = `apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: nginx:1.27
    port: 8080
environments:
  staging: {}
`

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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func platformYAML(baseDomain string) string {
	return `apiVersion: platform/v1-alpha.3
environments:
  production:
    domains:
      main:
        baseDomain: ` + baseDomain + `
`
}

func requireProductionDomain(t *testing.T, p *spec.PlatformConfig, want string) {
	t.Helper()
	require.NotNil(t, p)
	require.Contains(t, p.Environments, "production")
	require.Contains(t, p.Environments["production"].Domains, "main")
	require.Equal(t, want, p.Environments["production"].Domains["main"].BaseDomain)
}

func specAndAdjacent(t *testing.T, specBody, adjacentBody string) (specPath, adjacentPath string) {
	t.Helper()
	dir := t.TempDir()
	specPath = filepath.Join(dir, "deployah.yaml")
	adjacentPath = filepath.Join(dir, spec.DefaultPlatformPath)
	writeFile(t, specPath, specBody)
	if adjacentBody != "" {
		writeFile(t, adjacentPath, adjacentBody)
	}
	return specPath, adjacentPath
}

func TestSpecPath_DefaultWhenEmpty(t *testing.T) {
	t.Parallel()

	w := workspace.New(workspace.Config{})
	assert.Equal(t, spec.DefaultSpecPath, w.SpecPath())
}

func TestSpecPath_Explicit(t *testing.T) {
	t.Parallel()

	const path = "/tmp/custom/deployah.yaml"
	w := workspace.New(workspace.Config{SpecPath: path})
	assert.Equal(t, path, w.SpecPath())
}

func TestParseManifest_UsesEffectiveSpecPath(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	writeFile(t, specPath, minimalSpecYAML)
	w := workspace.New(workspace.Config{SpecPath: specPath})

	got, err := w.ParseManifest()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "demo", got.Project)
}

func TestParseManifest_DefaultPathInTempDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, spec.DefaultSpecPath, minimalSpecYAML)

	w := workspace.New(workspace.Config{})
	got, err := w.ParseManifest()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "demo", got.Project)
}

func TestParseManifest_MissingFile(t *testing.T) {
	t.Parallel()

	w := workspace.New(workspace.Config{
		SpecPath: filepath.Join(t.TempDir(), "missing.yaml"),
	})
	got, err := w.ParseManifest()
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "failed to read spec")
}

func TestParseManifest_MalformedYAML(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	writeFile(t, specPath, "not: [valid")
	w := workspace.New(workspace.Config{SpecPath: specPath})

	got, err := w.ParseManifest()
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestLoadSpec_UsesWorkspacePlatform(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	writeFile(t, specPath, specWithStagingYAML)
	writeFile(t, platformPath, platformYAML("platform.example"))

	w := workspace.New(workspace.Config{
		SpecPath:     specPath,
		PlatformPath: platformPath,
	})
	got, report, err := w.LoadSpec(t.Context(), "production")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "demo", got.Project)
	require.NotNil(t, report.DynamicSubdomains)
}

func TestLoadSpec_OptionalMissingPlatform(t *testing.T) {
	t.Setenv(spec.PlatformEnvVar, "")
	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	writeFile(t, specPath, specWithStagingYAML)

	w := workspace.New(workspace.Config{SpecPath: specPath})
	got, _, err := w.LoadSpec(t.Context(), "staging")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "demo", got.Project)
}

func TestLoadSpec_RequiredPlatformFailure(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	writeFile(t, specPath, specWithStagingYAML)
	w := workspace.New(workspace.Config{
		SpecPath:     specPath,
		PlatformPath: filepath.Join(t.TempDir(), "missing.platform.yaml"),
	})

	got, report, err := w.LoadSpec(t.Context(), "staging")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Equal(t, spec.SubstitutionReport{}, report)
	assert.Contains(t, err.Error(), "failed to load platform file")
	assert.NotContains(t, err.Error(), "failed to load spec")
}

func TestLoadSpec_ForwardsLoadOption(t *testing.T) {
	t.Setenv(spec.PlatformEnvVar, "")
	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	writeFile(t, specPath, hookCycleSpecYAML)
	w := workspace.New(workspace.Config{SpecPath: specPath})

	_, report, err := w.LoadSpec(t.Context(), "staging")
	require.Error(t, err)
	assert.Equal(t, spec.SubstitutionReport{}, report)
	assert.Contains(t, err.Error(), "cycle")

	got, _, err := w.LoadSpec(t.Context(), "staging", spec.AllowHookCycleForDisplay())
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Contains(t, got.Tasks, "migrate")
	require.Contains(t, got.Tasks, "seed")
}

func TestWorkspace_ConcurrentReads(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	writeFile(t, specPath, specWithStagingYAML)
	writeFile(t, platformPath, platformYAML("race.example"))
	w := workspace.New(workspace.Config{
		SpecPath:     specPath,
		PlatformPath: platformPath,
	})
	ctx := t.Context()

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_ = w.SpecPath()
			_ = w.PlatformPath()
			if _, err := w.Platform(); err != nil {
				t.Errorf("Platform: %v", err)
			}
			if _, err := w.ParseManifest(); err != nil {
				t.Errorf("ParseManifest: %v", err)
			}
			if _, _, err := w.LoadSpec(ctx, "production"); err != nil {
				t.Errorf("LoadSpec: %v", err)
			}
		})
	}
	wg.Wait()
}
