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
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGolden regenerates every golden file this package compares
// against (render scenarios' expected/*.yaml, plan scenarios'
// golden.txt/golden.json) instead of comparing against them.
var updateGolden = flag.Bool("update", false, "regenerate scenario golden files")

// compareOrUpdateGolden compares actual against the contents of path, or
// overwrites path with actual when *updateGolden is set (`go test -update`).
func compareOrUpdateGolden(t *testing.T, path, actual string) {
	t.Helper()
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(actual), 0o600))
		return
	}

	want, err := os.ReadFile(path) // #nosec G304 -- path is a scenario golden file under test
	require.NoError(t, err)
	assert.Equal(t, string(want), actual, "golden file %s is stale; run with -update to regenerate", path)
}
