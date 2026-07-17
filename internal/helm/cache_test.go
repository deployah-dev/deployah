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
	"testing"

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
	manifest := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "cache-test",
		Components: map[string]spec.Component{"web": serviceComponent()},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, "v1-alpha.2"))

	returnedPath, err := PrepareChart(context.Background(), manifest, "production", nil)
	require.NoError(t, err)

	key, err := GenerateCacheKey(manifest, nil)
	require.NoError(t, err)

	cachedPath, found := GetCachedChart(key)
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

	_, stillFound := GetCachedChart(key)
	assert.True(t, stillFound, "the cache entry must survive the caller cleaning up its own returned copy")
}
