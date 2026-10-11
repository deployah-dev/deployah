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
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	inttest "deployah.dev/deployah/internal/testing"
)

func TestResolveNamespaceCreate(t *testing.T) {
	t.Parallel()
	const runID = "run-1"
	const name = "e2e-demo"
	own := &NamespaceView{Name: name, UID: "uid-1", Run: runID}

	tests := []struct {
		name      string
		class     CreateClass
		got       *NamespaceView
		getErr    error
		wantOwned bool
		wantRetry bool
		wantUID   string
	}{
		{name: "create succeeded", class: CreateSucceeded, got: own, wantOwned: true, wantUID: "uid-1"},
		{name: "already exists same run", class: CreateAlreadyExists, got: own, wantOwned: true, wantUID: "uid-1"},
		{name: "ambiguous same run", class: CreateAmbiguous, got: own, wantOwned: true, wantUID: "uid-1"},
		{name: "ambiguous not found", class: CreateAmbiguous, getErr: ErrNotFound, wantRetry: true},
		{name: "ambiguous get failed", class: CreateAmbiguous, getErr: errors.New("timeout"), wantRetry: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveNamespaceCreate(runID, name, tt.class, tt.got, tt.getErr)
			require.NoError(t, got.Err)
			assert.Equal(t, tt.wantOwned, got.Owned)
			assert.Equal(t, tt.wantRetry, got.Retry)
			assert.Equal(t, tt.wantUID, got.Claim.UID)
		})
	}
}

func TestResolveNamespaceCreate_notOwner(t *testing.T) {
	t.Parallel()
	const runID = "run-1"
	const name = "e2e-demo"
	other := &NamespaceView{Name: name, UID: "uid-other", Run: "run-2"}
	got := ResolveNamespaceCreate(runID, name, CreateAlreadyExists, other, nil)
	require.ErrorIs(t, got.Err, ErrNotOwner)
	assert.False(t, got.Owned)
	assert.False(t, got.Retry)
}

func TestResolveNamespaceCreate_rejected(t *testing.T) {
	t.Parallel()
	got := ResolveNamespaceCreate("run-1", "e2e-demo", CreateRejected, nil, nil)
	require.ErrorContains(t, got.Err, "create rejected")
	assert.False(t, got.Owned)
	assert.False(t, got.Retry)
}

func TestFinishUncertain(t *testing.T) {
	t.Parallel()
	err := FinishUncertain("e2e-demo", errors.New("create timeout"), ErrNotFound)
	require.ErrorIs(t, err, ErrOwnershipUncertain)
	assert.ErrorContains(t, err, "not deleting")
	assert.ErrorContains(t, err, "e2e-demo")
}

func TestConfirmNamespaceOwner(t *testing.T) {
	t.Parallel()
	claim := Claim{Name: "e2e-demo", RunID: "run-1", UID: "uid-1"}
	err := ConfirmNamespaceOwner(claim, &NamespaceView{Name: claim.Name, UID: claim.UID, Run: claim.RunID}, nil)
	require.NoError(t, err)
}

func TestConfirmNamespaceOwner_errors(t *testing.T) {
	t.Parallel()
	claim := Claim{Name: "e2e-demo", RunID: "run-1", UID: "uid-1"}
	tests := []struct {
		name   string
		claim  Claim
		got    *NamespaceView
		getErr error
		want   error
	}{
		{name: "already gone", claim: claim, getErr: ErrNotFound, want: ErrNamespaceGone},
		{name: "different uid", claim: claim, got: &NamespaceView{Name: claim.Name, UID: "other", Run: claim.RunID}, want: ErrNotOwner},
		{name: "get failed", claim: claim, getErr: errors.New("boom"), want: ErrOwnershipUncertain},
		{name: "incomplete claim", claim: Claim{Name: claim.Name}, want: ErrOwnershipUncertain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ConfirmNamespaceOwner(tt.claim, tt.got, tt.getErr)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestUIDPrecondition(t *testing.T) {
	t.Parallel()
	pre, err := UIDPrecondition("uid-1")
	require.NoError(t, err)
	require.NotNil(t, pre.UID)
	assert.Equal(t, "uid-1", string(*pre.UID))
}

func TestUIDPrecondition_empty(t *testing.T) {
	t.Parallel()
	_, err := UIDPrecondition("")
	require.Error(t, err)
}

func TestRunCleanup_phaseIsolationAndBudget(t *testing.T) {
	t.Parallel()
	var started [2]bool
	err := RunCleanup(context.Background(), []CleanupPhase{
		{Name: "first", Run: func(ctx context.Context) error {
			started[0] = true
			<-ctx.Done()
			return ctx.Err()
		}},
		{Name: "second", Run: func(ctx context.Context) error {
			started[1] = true
			require.NoError(t, ctx.Err())
			return errors.New("pvc failed")
		}},
	}, WithPhaseTimeout(20*time.Millisecond))
	require.Error(t, err)
	assert.True(t, started[0])
	assert.True(t, started[1])
	assert.ErrorContains(t, err, "first")
	assert.ErrorContains(t, err, "pvc failed")

	called := false
	err = RunCleanup(context.Background(), []CleanupPhase{
		{Name: "slow", Run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}},
		{Name: "late", Run: func(context.Context) error {
			called = true
			return nil
		}},
	}, WithPhaseTimeout(time.Second), WithBudget(15*time.Millisecond))
	require.Error(t, err)
	assert.False(t, called)
	assert.ErrorContains(t, err, "deadline")
}

func TestRunCleanup_joinsAndStopsOnLostClaim(t *testing.T) {
	t.Parallel()
	var deleted bool
	err := RunCleanup(context.Background(), []CleanupPhase{
		{Name: "release", Run: func(context.Context) error { return errors.New("release failed") }},
		{Name: "workloads", Run: func(context.Context) error { return errors.New("workloads failed") }},
		{Name: "namespace", Run: func(context.Context) error { return errors.New("namespace failed") }},
	}, WithPhaseTimeout(time.Second))
	require.Error(t, err)
	assert.ErrorContains(t, err, "release failed")
	assert.ErrorContains(t, err, "workloads failed")
	assert.ErrorContains(t, err, "namespace failed")

	err = RunCleanup(context.Background(), []CleanupPhase{
		{Name: "confirm", Run: func(context.Context) error { return ErrNotOwner }},
		{Name: "namespace", Run: func(context.Context) error {
			deleted = true
			return nil
		}},
	}, WithPhaseTimeout(time.Second))
	require.ErrorIs(t, err, ErrNotOwner)
	assert.False(t, deleted)
}

func TestRun_policies(t *testing.T) {
	t.Parallel()
	t.Run("default exit and extra output", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, "env: dev\nsteps:\n  - deploy:\n      output:\n        stdout:\n          contains: ['Deployed']\n")
		var calls int
		result := Run(context.Background(), RunInput{
			Env: "dev", Namespace: "ns", Project: "p", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				calls++
				return CommandResult{Stdout: "Deployed extra", ExitCode: 0}
			},
		})
		require.Len(t, result.Steps, 1)
		assert.Equal(t, StatusPass, result.Steps[0].Status)
		assert.Equal(t, 1, calls)
	})

	t.Run("expected and unexpected exit", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
steps:
  - run:
      task: missing
      output:
        exitCode: 1
        stderr:
          contains: ['not found']
`)
		ok := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stderr: "task not found", ExitCode: 1}
			},
		})
		assert.Equal(t, StatusPass, ok.Steps[0].Status)
		bad := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{ExitCode: 2}
			},
		})
		assert.Equal(t, StatusFail, bad.Steps[0].Status)
	})

	t.Run("not executed", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, "env: dev\nsteps:\n  - deploy: {}\n  - delete: {}\n")
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{NotExecuted: true}
			},
		})
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.True(t, result.Steps[0].Unsafe)
		assert.Equal(t, StatusSkip, result.Steps[1].Status)
	})

	t.Run("plan expr uses one execution", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
steps:
  - plan:
      expr:
        - 'plan.changes.size() == 0'
        - 'plan.tasks.size() > 0'
`)
		var calls int
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				calls++
				return CommandResult{Stdout: `{"changes":[],"tasks":[{"name":"migrate"}]}`, ExitCode: 0}
			},
		})
		assert.Equal(t, StatusPass, result.Steps[0].Status)
		assert.Equal(t, 1, calls)

		falseResult := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stdout: `{"changes":[{}],"tasks":[]}`, ExitCode: 0}
			},
		})
		assert.Equal(t, StatusFail, falseResult.Steps[0].Status)

		badJSON := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stdout: "not-json", ExitCode: 1}
			},
		})
		assert.Equal(t, StatusFail, badJSON.Steps[0].Status, "exit mismatch is an expectation failure before JSON is required")
	})

	t.Run("plan output only allows bad json", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
steps:
  - plan:
      output:
        exitCode: 1
        stderr:
          contains: ['failed']
`)
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stdout: "not-json", Stderr: "failed", ExitCode: 1}
			},
		})
		assert.Equal(t, StatusPass, result.Steps[0].Status)
	})

	t.Run("malformed plan json with expr is error", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
steps:
  - plan:
      output:
        exitCode: 0
      expr:
        - 'plan.changes.size() == 0'
`)
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stdout: "{", ExitCode: 0}
			},
		})
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.Contains(t, result.Steps[0].Detail, "plan JSON")
	})

	t.Run("assertion stop and step stop", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
timeout: 40ms
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expr:
          - 'false'
        stopOnFailure: true
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: b
        expr:
          - 'objects.size() == 1'
    stopOnFailure: false
  - deploy: {}
`)
		var observed []string
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared, ResourcePoll: time.Millisecond,
			Observe: func(_ context.Context, sel inttest.ResourceSelect, _ string) ([]map[string]any, error) {
				observed = append(observed, sel.Name)
				if sel.Name == "a" {
					return nil, errors.Join(ErrObserveTerminal, errors.New("forbidden"))
				}
				return []map[string]any{{"kind": "ConfigMap"}}, nil
			},
		})
		require.Len(t, result.Steps, 2)
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.True(t, result.Steps[0].StoppedHere)
		assert.Equal(t, StatusSkip, result.Steps[1].Status)
		assert.Contains(t, observed, "b")
	})

	t.Run("nonfatal assertions finish before step stop", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
timeout: 30ms
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expr:
          - 'objects.size() == 1'
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: b
        expr:
          - 'objects.size() == 1'
    stopOnFailure: false
  - deploy: {}
`)
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared, ResourcePoll: time.Millisecond,
			Observe: func(context.Context, inttest.ResourceSelect, string) ([]map[string]any, error) {
				return []map[string]any{}, nil
			},
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{ExitCode: 0}
			},
		})
		require.Len(t, result.Steps, 2)
		assert.Equal(t, StatusFail, result.Steps[0].Status)
		assert.Equal(t, StatusFail, result.Steps[0].Assertions[0].Status)
		assert.Equal(t, StatusFail, result.Steps[0].Assertions[1].Status)
		assert.Equal(t, StatusPass, result.Steps[1].Status)
	})

	t.Run("step stop skips later steps", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
steps:
  - deploy: {}
    stopOnFailure: true
  - delete: {}
    stopOnFailure: false
`)
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{ExitCode: 1}
			},
		})
		assert.Equal(t, StatusFail, result.Steps[0].Status)
		assert.Equal(t, StatusSkip, result.Steps[1].Status)
	})

	t.Run("logs appear on a later poll", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
timeout: 200ms
steps:
  - logs:
      component: web
      contains: ['started']
`)
		var calls int
		result := Run(context.Background(), RunInput{
			Env: "dev", Project: "p", Fixture: prepared, LogsPoll: time.Millisecond,
			Exec: func(context.Context, string, []string) CommandResult {
				calls++
				if calls < 2 {
					return CommandResult{Stdout: "waiting", ExitCode: 0}
				}
				return CommandResult{Stdout: "started", ExitCode: 0}
			},
		})
		assert.Equal(t, StatusPass, result.Steps[0].Status)
		assert.GreaterOrEqual(t, calls, 2)
	})

	t.Run("logs command error stops", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
timeout: 1s
steps:
  - logs:
      component: missing
      contains: ['started']
  - deploy: {}
`)
		var calls int
		result := Run(context.Background(), RunInput{
			Env: "dev", Project: "p", Fixture: prepared, LogsPoll: time.Millisecond,
			Exec: func(context.Context, string, []string) CommandResult {
				calls++
				return CommandResult{Stderr: "component not found", ExitCode: 1}
			},
		})
		require.Len(t, result.Steps, 2)
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.Equal(t, 1, calls)
		assert.Equal(t, StatusSkip, result.Steps[1].Status)
	})

	t.Run("shared deadline records both assertions", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
timeout: 25ms
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expr: ['objects.size() == 1']
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: b
        expr: ['objects.size() == 1']
`)
		seen := map[string]int{}
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared, ResourcePoll: time.Millisecond,
			Observe: func(_ context.Context, sel inttest.ResourceSelect, _ string) ([]map[string]any, error) {
				seen[sel.Name]++
				return []map[string]any{}, nil
			},
		})
		assert.Equal(t, StatusFail, result.Steps[0].Status)
		assert.Equal(t, StatusFail, result.Steps[0].Assertions[0].Status)
		assert.Equal(t, StatusFail, result.Steps[0].Assertions[1].Status)
		assert.Greater(t, seen["a"], 0)
		assert.Greater(t, seen["b"], 0)
	})
}

func TestScenariosDirResolves(t *testing.T) {
	t.Parallel()
	info, err := os.Stat(inttest.TestScenariosDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func prepareRun(t *testing.T, body string) inttest.PreparedFixture {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/e2e.yaml"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	fx, err := inttest.LoadE2EFixture(path, dir)
	require.NoError(t, err)
	prepared, err := inttest.PrepareE2EFixture(path, fx)
	require.NoError(t, err)
	return prepared
}
