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

func TestPlatformPath_ExplicitWinsOverEnvAndAdjacent(t *testing.T) {
	specPath, adjacentPath := specAndAdjacent(t, minimalSpecYAML, platformYAML("adjacent.example"))
	explicitPath := filepath.Join(t.TempDir(), "explicit.platform.yaml")
	envPath := filepath.Join(t.TempDir(), "env.platform.yaml")
	writeFile(t, explicitPath, platformYAML("explicit.example"))
	writeFile(t, envPath, platformYAML("env.example"))
	t.Setenv(spec.PlatformEnvVar, envPath)

	w := workspace.New(workspace.Config{
		SpecPath:     specPath,
		PlatformPath: explicitPath,
	})
	assert.Equal(t, explicitPath, w.PlatformPath())
	assert.NotEqual(t, envPath, w.PlatformPath())
	assert.NotEqual(t, adjacentPath, w.PlatformPath())
}

func TestPlatformPath_EnvWinsOverAdjacent(t *testing.T) {
	specPath, adjacentPath := specAndAdjacent(t, minimalSpecYAML, platformYAML("adjacent.example"))
	envPath := filepath.Join(t.TempDir(), "env.platform.yaml")
	writeFile(t, envPath, platformYAML("env.example"))
	t.Setenv(spec.PlatformEnvVar, envPath)

	w := workspace.New(workspace.Config{SpecPath: specPath})
	assert.Equal(t, envPath, w.PlatformPath())
	assert.NotEqual(t, adjacentPath, w.PlatformPath())
}

func TestPlatformPath_AdjacentFallback(t *testing.T) {
	t.Setenv(spec.PlatformEnvVar, "")
	specPath := filepath.Join(t.TempDir(), "project", "deployah.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(specPath), 0o700))
	writeFile(t, specPath, minimalSpecYAML)

	w := workspace.New(workspace.Config{SpecPath: specPath})
	assert.Equal(t, filepath.Join(filepath.Dir(specPath), spec.DefaultPlatformPath), w.PlatformPath())
}

func TestPlatformSource_SnapshotsEnvAtConstruction(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "a.platform.yaml")
	pathB := filepath.Join(t.TempDir(), "b.platform.yaml")
	writeFile(t, pathA, platformYAML("a.example"))
	writeFile(t, pathB, platformYAML("b.example"))

	t.Setenv(spec.PlatformEnvVar, pathA)
	w := workspace.New(workspace.Config{})
	t.Setenv(spec.PlatformEnvVar, pathB)

	assert.Equal(t, pathA, w.PlatformPath())
	p, err := w.Platform()
	require.NoError(t, err)
	requireProductionDomain(t, p, "a.example")
}

func TestPlatform_ExplicitMissingIsError(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.platform.yaml")
	w := workspace.New(workspace.Config{PlatformPath: missing})
	p, err := w.Platform()
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "platform file not found")
}

func TestPlatform_EnvMissingIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-env.platform.yaml")
	t.Setenv(spec.PlatformEnvVar, missing)

	w := workspace.New(workspace.Config{})
	p, err := w.Platform()
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "platform file not found")
}

func TestPlatform_AdjacentMissingIsOptional(t *testing.T) {
	t.Setenv(spec.PlatformEnvVar, "")
	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	writeFile(t, specPath, minimalSpecYAML)

	w := workspace.New(workspace.Config{SpecPath: specPath})
	p, err := w.Platform()
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestPlatform_LoadsExplicitFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	writeFile(t, path, platformYAML("explicit.example"))
	w := workspace.New(workspace.Config{PlatformPath: path})
	p, err := w.Platform()
	require.NoError(t, err)
	requireProductionDomain(t, p, "explicit.example")
}

func TestPlatform_LoadsEnvSelectedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.platform.yaml")
	writeFile(t, path, platformYAML("env.example"))
	t.Setenv(spec.PlatformEnvVar, path)

	w := workspace.New(workspace.Config{})
	p, err := w.Platform()
	require.NoError(t, err)
	requireProductionDomain(t, p, "env.example")
}

func TestPlatform_LoadsAdjacentFile(t *testing.T) {
	t.Setenv(spec.PlatformEnvVar, "")
	specPath, _ := specAndAdjacent(t, minimalSpecYAML, platformYAML("adjacent.example"))

	w := workspace.New(workspace.Config{SpecPath: specPath})
	p, err := w.Platform()
	require.NoError(t, err)
	requireProductionDomain(t, p, "adjacent.example")
}

func TestPlatform_SuccessfulLoadIsMemoized(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	writeFile(t, path, platformYAML("cached.example"))
	w := workspace.New(workspace.Config{PlatformPath: path})

	first, err := w.Platform()
	require.NoError(t, err)
	requireProductionDomain(t, first, "cached.example")

	require.NoError(t, os.Remove(path))
	writeFile(t, path, platformYAML("changed.example"))

	second, err := w.Platform()
	require.NoError(t, err)
	assert.Same(t, first, second)
	requireProductionDomain(t, second, "cached.example")
}

func TestPlatform_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	writeFile(t, path, platformYAML("race.example"))
	w := workspace.New(workspace.Config{PlatformPath: path})

	const n = 32
	var got [n]*spec.PlatformConfig
	var errs [n]error
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			got[i], errs[i] = w.Platform()
		})
	}
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		requireProductionDomain(t, got[i], "race.example")
		assert.Same(t, got[0], got[i])
	}
}
