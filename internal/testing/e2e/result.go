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
	"fmt"
	"strings"
)

// Status is the outcome of one step or assertion.
type Status string

const (
	// StatusPass means the operation and its expectations succeeded.
	StatusPass Status = "PASS"
	// StatusFail means an expectation was not met.
	StatusFail Status = "FAIL"
	// StatusError means execution or infrastructure failed.
	StatusError Status = "ERROR"
	// StatusSkip means the step did not run because an earlier step stopped
	// the scenario.
	StatusSkip Status = "SKIP"
)

// AssertResult is one resource assertion inside an assert step.
type AssertResult struct {
	Index  int
	Status Status
	Detail string
	Unsafe bool
}

// StepResult is one scenario step.
type StepResult struct {
	Index       int
	Op          string
	Status      Status
	Detail      string
	Unsafe      bool
	Assertions  []AssertResult
	StoppedHere bool
}

// String is the go test line for a step that did not pass.
func (s StepResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s steps[%d] %s", s.Status, s.Index, s.Op)
	if s.Detail != "" {
		b.WriteString(": ")
		b.WriteString(s.Detail)
	}
	for _, assertion := range s.Assertions {
		if assertion.Status == StatusPass || assertion.Status == "" {
			continue
		}
		fmt.Fprintf(&b, "\n%s steps[%d] assert[%d]", assertion.Status, s.Index, assertion.Index)
		if assertion.Detail != "" {
			b.WriteString(": ")
			b.WriteString(assertion.Detail)
		}
	}
	return b.String()
}

// RunResult is every step of one scenario, in order.
type RunResult struct {
	Steps []StepResult
}

// Failed reports whether any step is FAIL or ERROR.
func (r RunResult) Failed() bool {
	for _, step := range r.Steps {
		if step.Status == StatusFail || step.Status == StatusError {
			return true
		}
	}
	return false
}
