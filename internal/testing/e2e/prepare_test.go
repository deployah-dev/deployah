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

package e2e

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareE2EFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
env: dev
timeout: 1m
steps:
  - deploy:
      output:
        stdout:
          contains: ['ok']
        stderr:
          contains: ['warn']
  - plan:
      expr:
        - 'plan.changes.size() == 0'
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
        expr:
          - 'objects.size() == 0'
        stopOnFailure: true
  - logs:
      component: web
      contains: ['ready']
`), 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	prepared, err := PrepareE2EFixture("e2e.yaml", fx)
	require.NoError(t, err)
	require.Len(t, prepared.Steps, 4)
	assert.Equal(t, "1m0s", prepared.Steps[0].Timeout.String())
	require.NotNil(t, prepared.Steps[0].Output)
	assert.Len(t, prepared.Steps[0].Output.Stdout, 1)
	require.NotNil(t, prepared.Steps[1].Plan)
	assert.Len(t, prepared.Steps[1].Plan.Programs, 1)
	assert.True(t, prepared.Steps[2].Assert[0].StopOnFailure)
	assert.Len(t, prepared.Steps[3].Logs.Patterns, 1)
}

func TestPrepareE2EFixture_errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
env: dev
timeout: nope
steps:
  - plan:
      expr:
        - '1'
        - 'missing.field'
    timeout: -1s
  - logs:
      component: web
      contains: ['[']
  - deploy:
      output:
        stdout:
          contains: ['(']
`), 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	before := fx
	_, err = PrepareE2EFixture("scenario/e2e.yaml", fx)
	require.Error(t, err)
	assert.Equal(t, before, fx)
	for _, want := range []string{
		"timeout",
		"not bool",
		"undeclared reference",
		"logs.contains",
		"output.stdout.contains",
	} {
		assert.ErrorContains(t, err, want)
	}
	var cfg *ConfigError
	require.ErrorAs(t, err, &cfg)
	assert.Equal(t, "scenario/e2e.yaml", cfg.File)
}

func TestCheckOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
env: dev
steps:
  - run:
      task: migrate
      output:
        exitCode: 1
        stdout:
          contains: ['done', 'ok']
        stderr:
          contains: ['task.*not found']
`), 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	prepared, err := PrepareE2EFixture("e2e.yaml", fx)
	require.NoError(t, err)
	require.NoError(t, CheckOutput("done and ok extra", "task foo not found", 1, prepared.Steps[0].Output))
}

func TestCheckOutput_excerpt(t *testing.T) {
	t.Parallel()
	err := CheckOutput(strings.Repeat("a", outputExcerptLimit+10), "", 1, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "...")
}

func TestCheckOutput_nilExitZero(t *testing.T) {
	t.Parallel()
	require.NoError(t, CheckOutput("", "", 0, nil))
}

func TestCheckOutput_errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
env: dev
steps:
  - run:
      task: migrate
      output:
        exitCode: 1
        stdout:
          contains: ['done', 'ok']
        stderr:
          contains: ['task.*not found']
`), 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	prepared, err := PrepareE2EFixture("e2e.yaml", fx)
	require.NoError(t, err)
	exp := prepared.Steps[0].Output

	tests := []struct {
		name    string
		stdout  string
		stderr  string
		code    int
		exp     *PreparedOutput
		wantErr string
	}{
		{name: "missing stdout pattern", stdout: "done", stderr: "task foo not found", code: 1, exp: exp, wantErr: "stdout missing pattern"},
		{name: "missing stderr pattern", stdout: "done and ok", stderr: "nope", code: 1, exp: exp, wantErr: "stderr missing pattern"},
		{name: "unexpected exit", stdout: "done and ok", stderr: "task foo not found", code: 0, exp: exp, wantErr: "exit 0, want 1"},
		{name: "nil expectation rejects nonzero", code: 2, wantErr: "exit 2, want 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotErr := CheckOutput(tt.stdout, tt.stderr, tt.code, tt.exp)
			require.ErrorContains(t, gotErr, tt.wantErr)
		})
	}
}

func TestPrepareDoesNotWriteSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e2e.yaml")
	body := []byte("env: dev\nsteps:\n  - deploy: {}\n")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	_, err = PrepareE2EFixture(path, fx)
	require.NoError(t, err)
	got, err := os.ReadFile(path) // #nosec G304 -- path is the temp file this test wrote
	require.NoError(t, err)
	assert.Equal(t, string(body), string(got))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	require.False(t, errors.Is(err, os.ErrNotExist))
}
