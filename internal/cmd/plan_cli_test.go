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

package cmd_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"

	planCmd "deployah.dev/deployah/internal/cmd/plan"
)

func TestPlanFlagsRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "output text", args: []string{"plan", "dev", "--output", "text"}},
		{name: "short output text", args: []string{"plan", "dev", "-o", "text"}},
		{name: "drift", args: []string{"plan", "dev", "--drift"}},
		{name: "raw", args: []string{"plan", "dev", "--raw"}},
		{name: "yaml", args: []string{"plan", "dev", "--yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, err := runPlanCLI(t, tc.args...)
			require.Error(t, err)
			assert.Contains(t, stderr, "error:")
		})
	}
}

func TestPlanFlagsAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
	}{
		{name: "output human", args: []string{"plan", "dev", "--output", "human", "--cwd", dir}},
		{name: "output json", args: []string{"plan", "dev", "--output", "json", "--cwd", dir}},
		{name: "short json", args: []string{"plan", "dev", "-o", "json", "--cwd", dir}},
		{name: "show secrets", args: []string{"plan", "dev", "--show-secrets", "--cwd", dir}},
		{name: "show secrets json", args: []string{"plan", "dev", "--show-secrets", "-o", "json", "--cwd", dir}},
		{name: "detailed exit code", args: []string{"plan", "dev", "--detailed-exitcode", "--cwd", dir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := runPlanCLI(t, tc.args...)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "unknown flag")
			assert.NotContains(t, err.Error(), "must be one of")
		})
	}
}

func TestPlanHelp(t *testing.T) {
	t.Parallel()
	stdout, _, err := runPlanCLI(t, "plan", "--help")
	require.NoError(t, err)
	tests := []struct {
		text string
		want bool
	}{
		{text: "--output, -o <human|json>", want: true},
		{text: "default: human", want: true},
		{text: "--show-secrets", want: true},
		{text: "--detailed-exitcode", want: true},
		{text: "--drift"},
		{text: "--raw"},
		{text: "--yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, strings.Contains(stdout, tc.text))
		})
	}
}

func TestPlanErrorBanner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		contains string
		empty    bool
	}{
		{name: "changes present", err: fmt.Errorf("x: %w", planCmd.ErrChangesPresent), empty: true},
		{name: "ordinary error", err: fmt.Errorf("boom"), contains: "error:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			app.MustCommand("boom", nabat.WithRun(func(*nabat.Context) error { return tc.err }))
			err := nabattest.RunParallel(t, app, []string{"boom"})
			require.Error(t, err)
			stderr := errOut.String()
			if tc.empty {
				assert.Empty(t, strings.TrimSpace(stderr))
			}
			if tc.contains != "" {
				assert.Contains(t, stderr, tc.contains)
			}
		})
	}
}

func runPlanCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err = nabattest.RunParallel(t, app, args)
	return out.String(), errOut.String(), err
}
