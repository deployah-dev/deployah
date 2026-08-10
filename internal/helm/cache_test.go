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

package helm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
)

// TestPrepareChart_CacheSurvivesCallerCleanup verifies a cache-miss
// PrepareChart call never hands the caller its own cache-backing
// directory: the caller is expected to eventually remove whatever path it
// gets back, and if that path were the cache's own entry, the very next
// PrepareChart call with the same key would find its cache entry pointing
// at a deleted directory and be forced to fully regenerate instead of
// hitting the cache.
func TestPrepareChart_CacheSurvivesCallerCleanup(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(time.Hour)
	manifest := &spec.Spec{
		APIVersion: "v1-alpha.4",
		Project:    "cache-test",
		Components: map[string]spec.Component{"web": serviceComponent()},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, "v1-alpha.4"))

	returnedPath, err := PrepareChart(t.Context(), manifest, "production", nil, cache)
	require.NoError(t, err)

	key, err := cache.GenerateKey(manifest, "production", nil)
	require.NoError(t, err)

	cachedPath, found := cache.get(key)
	require.True(t, found, "PrepareChart must register a cache entry on a miss")
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(cachedPath); removeErr != nil {
			t.Logf("cleanup: remove cached chart dir: %v", removeErr)
		}
	})

	assert.NotEqual(t, returnedPath, cachedPath,
		"PrepareChart must return a copy on a cache miss, not the cache's own backing directory")

	// Simulate the caller cleaning up the directory PrepareChart gave it,
	// exactly as InstallApp/RenderManifests's deferred cleanup does.
	require.NoError(t, os.RemoveAll(returnedPath))

	_, stillFound := cache.get(key)
	assert.True(t, stillFound, "the cache entry must survive the caller cleaning up its own returned copy")
}

// TestPrepareChart_RequiresCache verifies PrepareChart rejects a nil cache.
func TestPrepareChart_RequiresCache(t *testing.T) {
	t.Parallel()
	_, err := PrepareChart(t.Context(), &spec.Spec{Project: "x"}, "prod", nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "chart cache is required")
}

// TestChartCache_Isolation verifies two ChartCache instances do not share
// entries.
func TestChartCache_Isolation(t *testing.T) {
	t.Parallel()
	a := NewChartCache(time.Hour)
	b := NewChartCache(time.Hour)

	a.set("key", t.TempDir())
	_, foundA := a.get("key")
	_, foundB := b.get("key")
	assert.True(t, foundA)
	assert.False(t, foundB, "caches must not share entries")
}

// TestNewClient_RejectsNilChartCache verifies WithChartCache(nil) fails fast.
func TestNewClient_RejectsNilChartCache(t *testing.T) {
	t.Parallel()
	_, err := NewClient(WithStorageDriver("memory"), WithChartCache(nil))
	require.Error(t, err)
	assert.ErrorContains(t, err, "chart cache is required")
}

// TestNewChartCache_NonPositiveTTLUsesDefault verifies a non-positive TTL
// falls back to the package default rather than expiring immediately.
func TestNewChartCache_NonPositiveTTLUsesDefault(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(0)
	dir := t.TempDir()
	cache.set("k", dir)
	path, found := cache.get("k")
	require.True(t, found)
	assert.Equal(t, dir, path)
}

// TestChartCache_GetMissExpiredAndMissingDir verifies get returns false for
// unknown keys, TTL expiry, and deleted backing directories.
func TestChartCache_GetMissExpiredAndMissingDir(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(time.Millisecond)

	_, found := cache.get("missing")
	assert.False(t, found)

	dir := t.TempDir()
	cache.set("expired", dir)
	require.Eventually(t, func() bool {
		_, ok := cache.get("expired")
		return !ok
	}, 50*time.Millisecond, time.Millisecond)
	assert.Equal(t, 1, cache.entryCount(), "expired entries remain until cleanup")

	gone := filepath.Join(t.TempDir(), "removed")
	cache.set("gone", gone)
	_, found = cache.get("gone")
	assert.False(t, found)
}

// TestChartCache_CleanupExpired removes expired entries and their directories.
func TestChartCache_CleanupExpired(t *testing.T) {
	t.Parallel()
	cache := NewChartCache(time.Millisecond)
	dir := t.TempDir()
	keep := t.TempDir()
	cache.set("old", dir)
	cache.set("fresh", keep)

	require.Eventually(t, func() bool {
		_, ok := cache.get("old")
		return !ok
	}, 50*time.Millisecond, time.Millisecond)

	// Refresh the fresh entry so only "old" is past TTL when cleanup runs.
	cache.set("fresh", keep)
	cache.cleanupExpired()

	assert.Equal(t, 1, cache.entryCount())
	_, found := cache.get("fresh")
	assert.True(t, found)
	_, err := os.Stat(dir)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestCreateChartCopy_CopiesFiles verifies createChartCopy duplicates file
// contents into a new temp directory, including nested paths.
func TestCreateChartCopy_CopiesFiles(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "templates"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "values.yaml"), []byte("x: 1"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(src, "templates", "app.yaml"), []byte("kind: Pod"), 0o600))

	dst, err := createChartCopy(src)
	require.NoError(t, err)
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(dst); removeErr != nil {
			t.Logf("cleanup: remove chart copy: %v", removeErr)
		}
	})

	got, err := os.ReadFile(filepath.Join(dst, "values.yaml")) // #nosec G304 -- path under test-controlled temp dir
	require.NoError(t, err)
	assert.Equal(t, []byte("x: 1"), got)
	nested, err := os.ReadFile(filepath.Join(dst, "templates", "app.yaml")) // #nosec G304 -- path under test-controlled temp dir
	require.NoError(t, err)
	assert.Equal(t, []byte("kind: Pod"), nested)
	assert.NotEqual(t, src, dst)
}

// TestPrepareChart_CanceledContextReturnsImmediately verifies PrepareChart
// honors an already-canceled context before doing chart work.
func TestPrepareChart_CanceledContextReturnsImmediately(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := PrepareChart(ctx, &spec.Spec{Project: "x"}, "prod", nil, NewChartCache(time.Hour))
	require.ErrorIs(t, err, context.Canceled)
}
