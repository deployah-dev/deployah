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
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scenarioFiles() map[string]string {
	return map[string]string{
		"deployah.yaml":            "apiVersion: v1-alpha.5\nproject: demo\n",
		"deployah.platform.yaml":   "apiVersion: platform/v1-alpha.3\n",
		"deployah-replicas-2.yaml": "replicas: 2\n",
		"e2e.yaml":                 "env: dev\n",
		".env":                     "REGION=eu\n",
		".env.production":          "REGION=us\n",
		filepath.Join(".deployah", "crds", "example.yaml"):               "kind: CustomResourceDefinition\n",
		filepath.Join(".deployah", "manifests", "example.yaml"):          "kind: ConfigMap\n",
		filepath.Join(".deployah", "manifests", "nested", "config.yaml"): "kind: ConfigMap\nname: nested\n",
	}
}

func writeScenarioFixtureAt(tb testing.TB, root string) {
	tb.Helper()
	for rel, body := range scenarioFiles() {
		path := filepath.Join(root, rel)
		require.NoError(tb, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(tb, os.WriteFile(path, []byte(body), 0o600))
	}
}

func TestNewScenarioWorkspace_CopiesFixture(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	writeScenarioFixtureAt(t, src)
	first, err := NewScenarioWorkspace(t, src)
	require.NoError(t, err)
	second, err := NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	require.True(t, filepath.IsAbs(first))
	require.NotEqual(t, src, first)
	require.NotEqual(t, first, second)
	for _, dst := range []string{first, second} {
		for rel, body := range scenarioFiles() {
			got, readErr := os.ReadFile(filepath.Join(dst, rel)) // #nosec G304 -- path under t.TempDir
			require.NoError(t, readErr)
			assert.Equal(t, body, string(got), rel)
		}
		f, openErr := os.OpenFile(filepath.Join(dst, "deployah.yaml"), os.O_RDWR, 0) // #nosec G304 -- path under t.TempDir
		require.NoError(t, openErr)
		require.NoError(t, f.Close())
	}
}

func TestNewScenarioWorkspace_IsolatesCopies(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	writeScenarioFixtureAt(t, src)
	first, err := NewScenarioWorkspace(t, src)
	require.NoError(t, err)
	second, err := NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	spec := filepath.Join(first, "deployah.yaml")
	require.NoError(t, os.WriteFile(spec, []byte("changed\n"), 0o600))
	got, err := os.ReadFile(filepath.Join(src, "deployah.yaml")) // #nosec G304 -- path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, scenarioFiles()["deployah.yaml"], string(got))
	other, err := os.ReadFile(filepath.Join(second, "deployah.yaml")) // #nosec G304 -- path under t.TempDir
	require.NoError(t, err)
	assert.Equal(t, scenarioFiles()["deployah.yaml"], string(other))

	require.NoError(t, os.Remove(filepath.Join(first, ".env")))
	_, err = os.Stat(filepath.Join(src, ".env"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(second, ".env"))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(first, "extra.txt"), []byte("only-first\n"), 0o600))
	_, err = os.Stat(filepath.Join(second, "extra.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(src, "extra.txt"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestNewScenarioWorkspace_Parallel(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	writeScenarioFixtureAt(t, src)
	var mu sync.Mutex
	dirs := map[string]string{}

	for _, name := range []string{"one", "two"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, err := NewScenarioWorkspace(t, src)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600))
			got, readErr := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- path under t.TempDir
			require.NoError(t, readErr)
			assert.Equal(t, name, string(got))
			_, statErr := os.Stat(filepath.Join(src, name))
			require.ErrorIs(t, statErr, os.ErrNotExist)

			mu.Lock()
			dirs[name] = dir
			mu.Unlock()
		})
	}

	t.Cleanup(func() {
		require.Len(t, dirs, 2)
		assert.NotEqual(t, dirs["one"], dirs["two"])
	})
}

func TestNewScenarioWorkspace_RejectsSource(t *testing.T) {
	t.Parallel()

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "missing")
		dir, err := NewScenarioWorkspace(t, missing)
		require.Empty(t, dir)
		require.ErrorIs(t, err, os.ErrNotExist)
		require.ErrorContains(t, err, missing)
	})

	t.Run("file", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "deployah.yaml")
		require.NoError(t, os.WriteFile(file, []byte("x\n"), 0o600))
		dir, err := NewScenarioWorkspace(t, file)
		require.Empty(t, dir)
		require.ErrorContains(t, err, "not a directory")
		require.ErrorContains(t, err, file)
	})
}

func TestNewScenarioWorkspace_RejectsSymlink(t *testing.T) {
	t.Parallel()

	const secret = "secret\n"
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte(secret), 0o600))

	tests := []struct {
		name    string
		srcRel  string
		linkRel string
		target  string
	}{
		{
			name:    "source directory",
			srcRel:  "link",
			linkRel: "link",
			target:  "real",
		},
		{
			name:    "relative file inside",
			srcRel:  "real",
			linkRel: filepath.Join("real", "alias.yaml"),
			target:  "deployah.yaml",
		},
		{
			name:    "directory inside",
			srcRel:  "real",
			linkRel: filepath.Join("real", "linked-manifests"),
			target:  filepath.Join(".deployah", "manifests"),
		},
		{
			name:    "absolute file outside",
			srcRel:  "real",
			linkRel: filepath.Join("real", "escape.yaml"),
			target:  outside,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			real := filepath.Join(root, "real")
			writeScenarioFixtureAt(t, real)
			link := filepath.Join(root, tt.linkRel)
			require.NoError(t, os.Symlink(tt.target, link))

			dir, err := NewScenarioWorkspace(t, filepath.Join(root, tt.srcRel))
			require.Empty(t, dir)
			require.ErrorContains(t, err, "symbolic link")

			got, err := os.ReadFile(filepath.Join(real, "deployah.yaml")) // #nosec G304 -- path under t.TempDir
			require.NoError(t, err)
			assert.Equal(t, scenarioFiles()["deployah.yaml"], string(got))
			info, err := os.Lstat(link)
			require.NoError(t, err)
			assert.NotZero(t, info.Mode()&os.ModeSymlink)
			outsideGot, err := os.ReadFile(outside) // #nosec G304 -- path under t.TempDir
			require.NoError(t, err)
			assert.Equal(t, secret, string(outsideGot))
		})
	}
}

func TestNewScenarioWorkspace_RejectsSpecialFile(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	writeScenarioFixtureAt(t, src)
	pipe := filepath.Join(src, "pipe")
	require.NoError(t, syscall.Mkfifo(pipe, 0o644))

	dir, err := NewScenarioWorkspace(t, src)
	require.Empty(t, dir)
	require.ErrorIs(t, err, os.ErrInvalid)
}
