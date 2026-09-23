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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/testing/testpath"
)

func TestResolve_HookCycleIsWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
	}{
		{name: "two-node without platform", fixture: "hook-cycle"},
		{name: "two-node with platform", fixture: "hook-cycle-platform"},
		{name: "self-cycle without platform", fixture: "self-cycle"},
		{name: "self-cycle with platform", fixture: "self-cycle-platform"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, out, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, []string{"resolve", "staging"}, nabattest.WithDir(testpath.Dir(t, "testdata", tt.fixture)))
			require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

			got := out.String()
			assert.Contains(t, got, "hook ordering")
			assert.Contains(t, strings.ToLower(got), "cycle")
			assert.Contains(t, got, "unresolved tasks")
			assert.Contains(t, got, "LOG_LEVEL: debug")
		})
	}
}

func TestResolve_HookCycleJSONWarning(t *testing.T) {
	t.Parallel()

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "staging", "--output", "json"}, nabattest.WithDir(testpath.Dir(t, "testdata", "hook-cycle-platform")))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	var got struct {
		Components map[string]struct {
			ExplicitValues map[string]string `json:"explicit_values"`
		} `json:"components"`
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), out.String())
	require.NotEmpty(t, got.Warnings)
	joined := strings.Join(got.Warnings, "\n")
	assert.Contains(t, joined, "hook ordering")
	assert.Contains(t, strings.ToLower(joined), "cycle")
	assert.Contains(t, joined, "unresolved tasks")
	require.Contains(t, got.Components, "api")
	assert.Equal(t, "debug", got.Components["api"].ExplicitValues["LOG_LEVEL"])
}

func TestResolve_UnrelatedGraphErrorsRemainHard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		wantErr string
	}{
		{name: "missing after target", fixture: "missing-after", wantErr: "does not name a task"},
		{name: "after in different hook phase", fixture: "after-other-phase", wantErr: "not in the same on phase"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, []string{"resolve", "staging"}, nabattest.WithDir(testpath.Dir(t, "testdata", tt.fixture)))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Contains(t, errOut.String(), tt.wantErr)
		})
	}
}

func TestResolve_HookCyclePlusBlockedWarning(t *testing.T) {
	t.Parallel()

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "staging"}, nabattest.WithDir(testpath.Dir(t, "testdata", "hook-cycle-blocked")))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	got := out.String()
	assert.Contains(t, got, "after contains a cycle")
	assert.Contains(t, got, `unresolved tasks: "migrate", "seed", "smoke"`)
	assert.NotContains(t, got, "cycle among")
}
