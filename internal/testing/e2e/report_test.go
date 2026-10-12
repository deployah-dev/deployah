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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepReports(t *testing.T) {
	t.Parallel()
	steps := []StepResult{
		{Index: 0, Op: "plan", Status: StatusPass, Elapsed: 1200 * time.Millisecond},
		{
			Index: 1, Op: "assert", Status: StatusPass, Elapsed: 2100 * time.Millisecond,
			Assertions: []AssertResult{{Status: StatusPass}, {Status: StatusPass}, {Status: StatusPass}},
		},
		{Index: 2, Op: "deploy", Status: StatusPass, Elapsed: 800 * time.Millisecond},
		{
			Index: 3, Op: "assert", Status: StatusFail, Elapsed: time.Second, Timeout: 3 * time.Minute,
			Detail: "expr[0] objects.size() == 1: false\nexpected: objects.size() = 1\nactual: objects.size() = 0",
			Assertions: []AssertResult{
				{Index: 0, Status: StatusFail, Detail: "no resources selected: apps/v1 Deployment/web observed 0"},
				{Index: 1, Status: StatusPass},
				{Index: 2, Status: StatusSkip, Detail: "skipped after terminal assertion failure"},
			},
		},
		{Index: 4, Op: "delete", Status: StatusSkip, Detail: "skipped after steps[3] assert"},
		{Index: 5, Op: "logs", Status: StatusError, Detail: "logs exit 1\nstderr:\ncomponent not found", Timeout: time.Minute},
	}
	tests := []struct {
		name     string
		verbose  bool
		wantInfo []string
	}{
		{name: "default hides success and skip"},
		{
			name:    "verbose shows success and skip",
			verbose: true,
			wantInfo: []string{
				"PASS  [0] plan  (1.2s)",
				"PASS  [1] assert  (2.1s)\n      3/3 assertions passed",
				"PASS  [2] deploy  (800ms)",
				"SKIP  [4] delete\n      skipped after steps[3] assert",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info, fails := StepReports(tt.verbose, "basic-web-service", steps)
			assert.Equal(t, tt.wantInfo, info)
			require.Len(t, fails, 2)
			assert.Contains(t, fails[0], "FAIL  basic-web-service")
			assert.Contains(t, fails[0], "Step: [3] assert")
			assert.Contains(t, fails[0], "expected: objects.size() = 1")
			assert.Contains(t, fails[0], "actual: objects.size() = 0")
			assert.Contains(t, fails[0], "no resources selected")
			assert.Contains(t, fails[0], "Timeout: 3m0s")
			assert.NotContains(t, fails[0], "Assertion: [1]")
			assert.Contains(t, fails[1], "ERROR  basic-web-service")
			assert.Contains(t, fails[1], "component not found")
		})
	}
}
