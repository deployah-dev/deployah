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

package resolve_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/testing/testpath"
)

func TestResolve_JSONRuntimeValues(t *testing.T) {
	t.Parallel()

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "staging", "--output", "json"}, nabattest.WithDir(testpath.Dir(t, "testdata", "json")))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	var got struct {
		Environment string `json:"environment"`
		Components  map[string]struct {
			FileValues     map[string]string `json:"file_values"`
			ExplicitValues map[string]string `json:"explicit_values"`
		} `json:"components"`
		Tasks map[string]struct {
			FileValues     map[string]string `json:"file_values"`
			ExplicitValues map[string]string `json:"explicit_values"`
		} `json:"tasks"`
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), out.String())

	assert.Equal(t, "staging", got.Environment)
	require.Contains(t, got.Components, "api")
	assert.Equal(t, map[string]string{"REGION": "eu"}, got.Components["api"].FileValues)
	assert.Equal(t, map[string]string{"LOG_LEVEL": "debug"}, got.Components["api"].ExplicitValues)

	require.Contains(t, got.Tasks, "migrate")
	assert.Equal(t, map[string]string{"REGION": "eu"}, got.Tasks["migrate"].FileValues)
	assert.Equal(t, map[string]string{
		"LOG_LEVEL":      "debug",
		"MIGRATION_MODE": "safe",
	}, got.Tasks["migrate"].ExplicitValues)

	require.NotEmpty(t, got.Warnings)
	assert.Contains(t, got.Warnings[0], "platform file not found")
}
