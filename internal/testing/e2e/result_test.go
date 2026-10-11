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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStepResultString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		step StepResult
		want string
	}{
		{
			name: "pass is one line",
			step: StepResult{Index: 0, Op: "deploy", Status: StatusPass},
			want: "PASS steps[0] deploy",
		},
		{
			name: "fail with detail",
			step: StepResult{Index: 1, Op: "plan", Status: StatusFail, Detail: "exit 1"},
			want: "FAIL steps[1] plan: exit 1",
		},
		{
			name: "error keeps assertion lines",
			step: StepResult{
				Index: 2, Op: "assert", Status: StatusError, Detail: "forbidden",
				Assertions: []AssertResult{
					{Index: 0, Status: StatusPass, Detail: "hidden"},
					{Index: 1, Status: StatusFail, Detail: "replicas"},
					{Index: 2, Status: StatusError, Detail: "timeout"},
					{Index: 3, Status: StatusSkip},
					{Index: 4},
				},
			},
			want: "ERROR steps[2] assert: forbidden\nFAIL steps[2] assert[1]: replicas\nERROR steps[2] assert[2]: timeout\nSKIP steps[2] assert[3]",
		},
		{
			name: "empty detail",
			step: StepResult{Index: 0, Op: "delete", Status: StatusSkip},
			want: "SKIP steps[0] delete",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.step.String())
		})
	}
}

func TestRunResultFailed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  RunResult
		want bool
	}{
		{name: "empty", run: RunResult{}, want: false},
		{name: "pass and skip", run: RunResult{Steps: []StepResult{
			{Status: StatusPass}, {Status: StatusSkip},
		}}, want: false},
		{name: "fail", run: RunResult{Steps: []StepResult{
			{Status: StatusPass}, {Status: StatusFail},
		}}, want: true},
		{name: "error", run: RunResult{Steps: []StepResult{
			{Status: StatusError},
		}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.run.Failed())
		})
	}
}
