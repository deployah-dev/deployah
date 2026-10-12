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
	"time"
)

// StepReports splits steps into log lines and failures.
func StepReports(verbose bool, scenario string, steps []StepResult) (info, failures []string) {
	// Pass and skip are logged only when verbose is true.
	// Fail and error always fail the test. Skip does not.
	for _, step := range steps {
		switch step.Status {
		case StatusFail, StatusError:
			failures = append(failures, FormatFailure(scenario, step))
		case StatusPass, StatusSkip:
			if verbose {
				info = append(info, FormatStep(step))
			}
		}
	}
	return info, failures
}

// FormatFailure is the multi-line diagnostic for one failed step.
func FormatFailure(scenario string, step StepResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", step.Status, scenario)
	fmt.Fprintf(&b, "      Step: [%d] %s\n", step.Index, step.Op)
	writeTiming(&b, step)
	if step.Detail != "" {
		b.WriteString(indent(step.Detail))
	}
	for _, assertion := range step.Assertions {
		if assertion.Status == StatusPass || assertion.Status == "" {
			continue
		}
		fmt.Fprintf(&b, "      Assertion: [%d] %s\n", assertion.Index, assertion.Status)
		if assertion.Detail != "" {
			b.WriteString(indent(assertion.Detail))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FormatStep is one verbose progress line, including skipped steps.
func FormatStep(step StepResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  [%d] %s", step.Status, step.Index, step.Op)
	if step.Elapsed > 0 {
		fmt.Fprintf(&b, "  (%s)", step.Elapsed.Round(time.Millisecond))
	}
	if step.Status == StatusPass && len(step.Assertions) > 0 {
		passed := 0
		for _, assertion := range step.Assertions {
			if assertion.Status == StatusPass {
				passed++
			}
		}
		fmt.Fprintf(&b, "\n      %d/%d assertions passed", passed, len(step.Assertions))
	}
	if step.Detail != "" && step.Status != StatusPass {
		fmt.Fprintf(&b, "\n      %s", step.Detail)
	}
	return b.String()
}

func writeTiming(b *strings.Builder, step StepResult) {
	if step.Elapsed > 0 {
		fmt.Fprintf(b, "      Elapsed: %s\n", step.Elapsed.Round(time.Millisecond))
	}
	if step.Timeout > 0 {
		fmt.Fprintf(b, "      Timeout: %s\n", step.Timeout)
	}
}

func indent(text string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		fmt.Fprintf(&b, "      %s\n", line)
	}
	return b.String()
}
