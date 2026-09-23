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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/testing/testpath"
)

func TestResolve_Substitution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		fixture         string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:    "variables and env file",
			fixture: "substituted",
			wantContains: []string{
				"LOG_LEVEL: debug",
				"REGION: eu",
				"DPY_VAR_LEVEL: from-dotenv",
			},
			wantNotContains: []string{"LOG_LEVEL: ${LEVEL}"},
		},
		{
			name:    "dotenv DPY_VAR does not substitute",
			fixture: "dotenv-skips-subst",
			wantContains: []string{
				"LOG_LEVEL: from-variables",
				"DPY_VAR_LEVEL: from-dotenv",
			},
			wantNotContains: []string{"LOG_LEVEL: from-dotenv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, out, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, []string{"resolve", "production"}, nabattest.WithDir(testpath.Dir(t, "testdata", tt.fixture)))
			require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

			got := out.String()
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
			for _, skip := range tt.wantNotContains {
				assert.NotContains(t, got, skip)
			}
		})
	}
}

func TestResolve_ProcessDPYVarSubstitutes(t *testing.T) {
	// t.Setenv is process-wide, so this test cannot run in parallel.
	t.Setenv("DPY_VAR_LEVEL", "from-process")

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.Run(t, app, []string{"resolve", "production"}, nabattest.WithDir(testpath.Dir(t, "testdata", "process-env")))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	got := out.String()
	assert.Contains(t, got, "LOG_LEVEL: from-process")
	assert.NotContains(t, got, "LOG_LEVEL: from-variables")
}
