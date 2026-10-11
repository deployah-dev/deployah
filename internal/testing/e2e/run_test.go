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
			Observe: func(_ context.Context, sel ResourceSelect, _ string) ([]map[string]any, error) {
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

func TestRun_assertions(t *testing.T) {
	t.Parallel()
	object := []map[string]any{{"kind": "ConfigMap"}}
	terminal := errors.Join(ErrObserveTerminal, errors.New("forbidden"))
	tests := []struct {
		name     string
		doc      string
		observe  func(name string, call int) ([]map[string]any, error)
		wantSeen []string
		want     []Status
		step     Status
		next     Status
		hasNext  bool
		stopped  bool
		minCalls int
	}{
		{
			name: "first terminal stop skips later assertions",
			doc: `
env: dev
timeout: 40ms
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expr: ['false']
        stopOnFailure: true
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: b
        expr: ['objects.size() == 1']
    stopOnFailure: false
  - deploy: {}
`,
			observe: func(name string, _ int) ([]map[string]any, error) {
				if name == "a" {
					return nil, terminal
				}
				return object, nil
			},
			wantSeen: []string{"a"},
			want:     []Status{StatusError, StatusSkip},
			step:     StatusError,
			next:     StatusSkip,
			hasNext:  true,
			stopped:  true,
		},
		{
			name: "middle terminal stop skips the rest of the tick",
			doc: `
env: dev
timeout: 1s
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
        expr: ['false']
        stopOnFailure: true
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: c
        expr: ['objects.size() == 1']
    stopOnFailure: false
  - deploy: {}
`,
			observe: func(name string, _ int) ([]map[string]any, error) {
				if name == "b" {
					return nil, terminal
				}
				return object, nil
			},
			wantSeen: []string{"a", "b"},
			want:     []Status{StatusPass, StatusError, StatusSkip},
			step:     StatusError,
			next:     StatusSkip,
			hasNext:  true,
			stopped:  true,
		},
		{
			name: "last assertion stop",
			doc: `
env: dev
timeout: 1s
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
        expr: ['false']
        stopOnFailure: true
`,
			observe: func(name string, _ int) ([]map[string]any, error) {
				if name == "b" {
					return []map[string]any{}, nil
				}
				return object, nil
			},
			want:    []Status{StatusPass, StatusFail},
			step:    StatusFail,
			stopped: true,
		},
		{
			name: "nonfatal failures stay inside the step",
			doc: `
env: dev
timeout: 30ms
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
    stopOnFailure: false
  - deploy: {}
`,
			observe: func(string, int) ([]map[string]any, error) {
				return []map[string]any{}, nil
			},
			want:    []Status{StatusFail, StatusFail},
			step:    StatusFail,
			next:    StatusPass,
			hasNext: true,
		},
		{
			name: "nonfatal failures then step stop",
			doc: `
env: dev
timeout: 40ms
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expr: ['objects.size() == 1']
        stopOnFailure: false
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: b
        expr: ['objects.size() == 1']
        stopOnFailure: false
    stopOnFailure: true
  - deploy: {}
`,
			observe: func(string, int) ([]map[string]any, error) {
				return []map[string]any{}, nil
			},
			want:    []Status{StatusFail, StatusFail},
			step:    StatusFail,
			next:    StatusSkip,
			hasNext: true,
		},
		{
			name: "transient mismatch then success",
			doc: `
env: dev
timeout: 1s
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expr: ['objects.size() == 1']
`,
			observe: func(_ string, call int) ([]map[string]any, error) {
				if call == 1 {
					return []map[string]any{}, nil
				}
				return object, nil
			},
			want:     []Status{StatusPass},
			step:     StatusPass,
			minCalls: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prepared := prepareRun(t, tt.doc)
			var seen []string
			var calls int
			result := Run(context.Background(), RunInput{
				Env: "dev", Fixture: prepared, ResourcePoll: time.Millisecond,
				Observe: func(_ context.Context, sel ResourceSelect, _ string) ([]map[string]any, error) {
					calls++
					seen = append(seen, sel.Name)
					return tt.observe(sel.Name, calls)
				},
				Exec: func(context.Context, string, []string) CommandResult {
					return CommandResult{ExitCode: 0}
				},
			})
			if tt.wantSeen != nil {
				assert.Equal(t, tt.wantSeen, seen)
			}
			require.Len(t, result.Steps[0].Assertions, len(tt.want))
			for i, status := range tt.want {
				assert.Equal(t, status, result.Steps[0].Assertions[i].Status)
			}
			assert.Equal(t, tt.step, result.Steps[0].Status)
			assert.Equal(t, tt.stopped, result.Steps[0].StoppedHere)
			if tt.hasNext {
				require.Len(t, result.Steps, 2)
				assert.Equal(t, tt.next, result.Steps[1].Status)
			}
			if tt.minCalls > 0 {
				assert.GreaterOrEqual(t, calls, tt.minCalls)
			}
			assert.Equal(t, tt.step == StatusFail || tt.step == StatusError, result.Failed())
		})
	}
}

func TestRun_logs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		doc       string
		cancel    bool
		reply     func(call int) CommandResult
		want      Status
		detail    string
		notDetail string
		calls     int
		minCalls  int
		next      Status
		hasNext   bool
		unsafe    bool
	}{
		{
			name: "content appears on a later poll",
			doc: `
env: dev
timeout: 200ms
steps:
  - logs:
      component: web
      contains: ['started']
`,
			reply: func(call int) CommandResult {
				if call < 2 {
					return CommandResult{Stdout: "waiting", ExitCode: 0}
				}
				return CommandResult{Stdout: "started", ExitCode: 0}
			},
			want:     StatusPass,
			minCalls: 2,
		},
		{
			name: "terminal exit stops",
			doc: `
env: dev
timeout: 1s
steps:
  - logs:
      component: missing
      contains: ['started']
  - deploy: {}
`,
			reply: func(int) CommandResult {
				return CommandResult{Stderr: "component not found", ExitCode: 1}
			},
			want:    StatusError,
			detail:  "component not found",
			calls:   1,
			next:    StatusSkip,
			hasNext: true,
		},
		{
			name: "retryable failure then a match",
			doc: `
env: dev
timeout: 1s
steps:
  - logs:
      component: web
      contains: ['ready']
`,
			reply: func(call int) CommandResult {
				if call == 1 {
					return CommandResult{Stderr: "connection reset", ExitCode: 1, Retryable: true}
				}
				return CommandResult{Stdout: "ready", ExitCode: 0}
			},
			want:  StatusPass,
			calls: 2,
		},
		{
			name: "retryable failure exhausts the deadline",
			doc: `
env: dev
timeout: 30ms
steps:
  - logs:
      component: web
      contains: ['ready']
    stopOnFailure: false
  - deploy: {}
`,
			reply: func(int) CommandResult {
				return CommandResult{Stderr: "timeout talking to apiserver", ExitCode: 1, Retryable: true}
			},
			want:      StatusError,
			notDetail: "missing pattern",
			next:      StatusPass,
			hasNext:   true,
		},
		{
			name: "retrieved logs that never match",
			doc: `
env: dev
timeout: 30ms
steps:
  - logs:
      component: web
      contains: ['ready']
`,
			reply: func(int) CommandResult {
				return CommandResult{Stdout: "still starting", ExitCode: 0}
			},
			want:   StatusFail,
			detail: "ready",
		},
		{
			name: "command was not executed",
			doc: `
env: dev
timeout: 1s
steps:
  - logs:
      component: web
      contains: ['x']
`,
			reply: func(int) CommandResult {
				return CommandResult{NotExecuted: true}
			},
			want:   StatusError,
			detail: "not executed",
			unsafe: true,
		},
		{
			name: "interrupted retrieval",
			doc: `
env: dev
timeout: 1s
steps:
  - logs:
      component: web
      contains: ['x']
`,
			reply: func(int) CommandResult {
				return CommandResult{Interrupted: true}
			},
			want:   StatusError,
			detail: "interrupted",
			unsafe: true,
		},
		{
			name:   "canceled parent",
			cancel: true,
			doc: `
env: dev
timeout: 1s
steps:
  - logs:
      component: web
      contains: ['ready']
    stopOnFailure: false
  - deploy: {}
`,
			reply: func(int) CommandResult {
				return CommandResult{Stdout: "ready", ExitCode: 0}
			},
			want:    StatusError,
			unsafe:  true,
			next:    StatusSkip,
			hasNext: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prepared := prepareRun(t, tt.doc)
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}
			var calls int
			result := Run(ctx, RunInput{
				Env: "dev", Project: "p", Fixture: prepared, LogsPoll: time.Millisecond,
				Exec: func(_ context.Context, _ string, args []string) CommandResult {
					if len(args) == 0 || args[0] != "logs" {
						return CommandResult{ExitCode: 0}
					}
					calls++
					return tt.reply(calls)
				},
			})
			assert.Equal(t, tt.want, result.Steps[0].Status)
			assert.Equal(t, tt.unsafe, result.Steps[0].Unsafe)
			if tt.detail != "" {
				assert.Contains(t, result.Steps[0].Detail, tt.detail)
			}
			if tt.notDetail != "" {
				assert.NotContains(t, result.Steps[0].Detail, tt.notDetail)
			}
			if tt.calls > 0 {
				assert.Equal(t, tt.calls, calls)
			}
			if tt.minCalls > 0 {
				assert.GreaterOrEqual(t, calls, tt.minCalls)
			}
			if tt.hasNext {
				require.Len(t, result.Steps, 2)
				assert.Equal(t, tt.next, result.Steps[1].Status)
			}
		})
	}
}

func TestRun_expect(t *testing.T) {
	t.Parallel()
	doc := `
env: dev
timeout: 20ms
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
          name: a
        expect:
          data:
            ok: "yes"
`
	tests := []struct {
		name    string
		objects []map[string]any
		detail  string
	}{
		{name: "empty selection", objects: []map[string]any{}, detail: "no resources selected"},
		{name: "field mismatch", objects: []map[string]any{{"data": map[string]any{"ok": "no"}}}, detail: "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prepared := prepareRun(t, doc)
			result := Run(context.Background(), RunInput{
				Env: "dev", Fixture: prepared, ResourcePoll: time.Millisecond,
				Observe: func(context.Context, ResourceSelect, string) ([]map[string]any, error) {
					return tt.objects, nil
				},
			})
			assert.Equal(t, StatusFail, result.Steps[0].Status)
			assert.Contains(t, result.Steps[0].Assertions[0].Detail, tt.detail)
		})
	}
}

func TestRun_edges(t *testing.T) {
	t.Parallel()

	t.Run("nil context", func(t *testing.T) {
		t.Parallel()
		// Run rejects a missing context instead of panicking.
		var missing context.Context
		result := Run(missing, RunInput{})
		require.Len(t, result.Steps, 1)
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.True(t, result.Steps[0].Unsafe)
		assert.True(t, result.Failed())
	})

	t.Run("parent cancel during deploy", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, "env: dev\ntimeout: 1s\nsteps:\n  - deploy: {}\n    stopOnFailure: false\n  - delete: {}\n")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := Run(ctx, RunInput{
			Env: "dev", Namespace: "ns", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stderr: "canceled", Interrupted: true}
			},
		})
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.True(t, result.Steps[0].Unsafe)
		assert.Contains(t, result.Steps[0].Detail, "canceled")
		assert.Equal(t, StatusSkip, result.Steps[1].Status)
		assert.Contains(t, result.Steps[1].Detail, "steps[0] deploy")
	})

	t.Run("nil observer is unsafe", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
timeout: 1s
steps:
  - assert:
      - select:
          apiVersion: v1
          kind: ConfigMap
        expr: ['true']
  - deploy: {}
`)
		result := Run(context.Background(), RunInput{Env: "dev", Fixture: prepared})
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.True(t, result.Steps[0].Unsafe)
		assert.Equal(t, StatusSkip, result.Steps[1].Status)
	})

	t.Run("command flags", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(dir+"/deployah.yaml", []byte("apiVersion: v1-alpha.5\n"), 0o600))
		require.NoError(t, os.WriteFile(dir+"/e2e.yaml", []byte(`
env: dev
steps:
  - deploy:
      spec: deployah.yaml
      args: ['--atomic']
  - plan:
      detailedExitCode: true
  - run:
      task: migrate
  - delete: {}
`), 0o600))
		fx, err := LoadE2EFixture(dir+"/e2e.yaml", dir)
		require.NoError(t, err)
		prepared, err := PrepareE2EFixture(dir+"/e2e.yaml", fx)
		require.NoError(t, err)
		var got [][]string
		result := Run(context.Background(), RunInput{
			Env: "dev", Namespace: "ns", Project: "demo", Fixture: prepared,
			Exec: func(_ context.Context, _ string, args []string) CommandResult {
				got = append(got, append([]string(nil), args...))
				return CommandResult{ExitCode: 0}
			},
		})
		require.False(t, result.Failed(), result.Steps)
		require.Len(t, got, 4)
		assert.Equal(t, []string{"deploy", "dev", "--context", KindContext, "--namespace", "ns", "--spec", "deployah.yaml", "--atomic"}, got[0])
		assert.Contains(t, got[1], "--detailed-exitcode")
		assert.Equal(t, "run", got[2][0])
		assert.Equal(t, "migrate", got[2][1])
		assert.Equal(t, "delete", got[3][0])
	})

	t.Run("missing run task", func(t *testing.T) {
		t.Parallel()
		result := Run(context.Background(), RunInput{
			Fixture: PreparedFixture{Steps: []PreparedStep{{Op: "run", Timeout: time.Second}}},
			Exec:    func(context.Context, string, []string) CommandResult { return CommandResult{} },
		})
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.Contains(t, result.Steps[0].Detail, "run.task")
		assert.True(t, result.Steps[0].Unsafe)
	})

	t.Run("plan expression error", func(t *testing.T) {
		t.Parallel()
		prepared := prepareRun(t, `
env: dev
steps:
  - plan:
      expr:
        - 'plan'
`)
		result := Run(context.Background(), RunInput{
			Env: "dev", Fixture: prepared,
			Exec: func(context.Context, string, []string) CommandResult {
				return CommandResult{Stdout: `{"changes":[]}`, ExitCode: 0}
			},
		})
		assert.Equal(t, StatusError, result.Steps[0].Status)
		assert.Contains(t, result.Steps[0].Detail, "expr[0]")
	})
}

func TestScenariosDirResolves(t *testing.T) {
	t.Parallel()
	info, err := os.Stat(inttest.TestScenariosDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func prepareRun(t *testing.T, body string) PreparedFixture {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/e2e.yaml"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	fx, err := LoadE2EFixture(path, dir)
	require.NoError(t, err)
	prepared, err := PrepareE2EFixture(path, fx)
	require.NoError(t, err)
	return prepared
}
