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
	"fmt"
	"regexp"
	"time"

	"deployah.dev/deployah/internal/testing/celassert"
)

// PreparedFixture is an [E2EFixture] with durations, CEL programs, and
// regular expressions compiled once.
type PreparedFixture struct {
	Fixture E2EFixture
	Steps   []PreparedStep
}

// PreparedStep is one compiled scenario step.
type PreparedStep struct {
	Op            string
	Timeout       time.Duration
	StopOnFailure bool
	Output        *PreparedOutput
	Deploy        *DeployOp
	Plan          *PreparedPlan
	Run           *RunOp
	Logs          *PreparedLogs
	Delete        *DeleteOp
	Assert        []PreparedAssert
}

// PreparedOutput is a compiled [OutputExpectation]. A missing exit code is 0.
type PreparedOutput struct {
	ExitCode  int
	Stdout    []*regexp.Regexp
	Stderr    []*regexp.Regexp
	StdoutPat []string
	StderrPat []string
}

// PreparedPlan is a compiled [PlanOp].
type PreparedPlan struct {
	DetailedExitCode bool
	Programs         []*celassert.Program
	Expr             []string
}

// PreparedLogs is a compiled [LogsOp].
type PreparedLogs struct {
	Component string
	Patterns  []*regexp.Regexp
	Raw       []string
}

// PreparedAssert is a compiled [ResourceAssertion].
type PreparedAssert struct {
	Select        ResourceSelect
	HasExpect     bool
	Expect        map[string]any
	Programs      []*celassert.Program
	Expr          []string
	StopOnFailure bool
}

// PrepareE2EFixture compiles durations, CEL, and regular expressions.
// It does not touch Kubernetes.
func PrepareE2EFixture(file string, fx E2EFixture) (PreparedFixture, error) {
	var errs []error
	if _, err := parseTimeout(fx.Timeout, ""); err != nil {
		errs = append(errs, &ConfigError{File: file, Step: -1, Assertion: -1, Path: "timeout", Err: err})
	}
	steps := make([]PreparedStep, 0, len(fx.Steps))
	for i, step := range fx.Steps {
		prepared, err := prepareStep(file, fx.Timeout, i, step)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		steps = append(steps, prepared)
	}
	if err := errors.Join(errs...); err != nil {
		return PreparedFixture{}, err
	}
	return PreparedFixture{Fixture: fx, Steps: steps}, nil
}

func prepareStep(file, fixtureTimeout string, index int, step Step) (PreparedStep, error) {
	timeout, err := parseTimeout(step.Timeout, fixtureTimeout)
	op := step.OpName()
	if op == "" {
		return PreparedStep{}, &ConfigError{
			File: file, Step: index, Assertion: -1,
			Err: errors.New("exactly one operation is required"),
		}
	}
	prepared := PreparedStep{
		Op:            op,
		Timeout:       timeout,
		StopOnFailure: step.StopOnFailureEnabled(),
		Deploy:        step.Deploy,
		Run:           step.Run,
		Delete:        step.Delete,
	}
	var errs []error
	if err != nil {
		errs = append(errs, &ConfigError{File: file, Step: index, Assertion: -1, Path: "timeout", Err: err})
		prepared.Timeout = DefaultE2EStepTimeout
	}
	if out := step.Output(); out != nil {
		compiled, outErr := compileOutput(file, index, out)
		if outErr != nil {
			errs = append(errs, outErr)
		} else {
			prepared.Output = compiled
		}
	}
	switch op {
	case "plan":
		plan, planErr := compilePlan(file, index, step.Plan)
		if planErr != nil {
			errs = append(errs, planErr)
		} else {
			prepared.Plan = plan
		}
	case "logs":
		logs, logsErr := compileLogs(file, index, step.Logs)
		if logsErr != nil {
			errs = append(errs, logsErr)
		} else {
			prepared.Logs = logs
		}
	case "assert":
		asserts, assertErrs := compileAsserts(file, index, step.Assert)
		errs = append(errs, assertErrs...)
		prepared.Assert = asserts
	}
	if joinErr := errors.Join(errs...); joinErr != nil {
		return PreparedStep{}, joinErr
	}
	return prepared, nil
}

func parseTimeout(raw, fallback string) (time.Duration, error) {
	if raw == "" {
		raw = fallback
	}
	if raw == "" {
		return DefaultE2EStepTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("timeout %q: %w", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("timeout %q must be positive", raw)
	}
	return d, nil
}

func compileOutput(file string, step int, out *OutputExpectation) (*PreparedOutput, error) {
	prepared := &PreparedOutput{ExitCode: 0}
	if out.ExitCode != nil {
		prepared.ExitCode = *out.ExitCode
	}
	var errs []error
	if out.Stdout != nil {
		compiled, raw, err := compilePatterns(file, step, -1, "output.stdout.contains", out.Stdout.Contains)
		if err != nil {
			errs = append(errs, err)
		}
		prepared.Stdout = compiled
		prepared.StdoutPat = raw
	}
	if out.Stderr != nil {
		compiled, raw, err := compilePatterns(file, step, -1, "output.stderr.contains", out.Stderr.Contains)
		if err != nil {
			errs = append(errs, err)
		}
		prepared.Stderr = compiled
		prepared.StderrPat = raw
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return prepared, nil
}

func compilePlan(file string, step int, op *PlanOp) (*PreparedPlan, error) {
	plan := &PreparedPlan{DetailedExitCode: op.DetailedExitCode, Expr: op.Expr}
	if len(op.Expr) == 0 {
		return plan, nil
	}
	programs := make([]*celassert.Program, 0, len(op.Expr))
	var errs []error
	for i, expr := range op.Expr {
		prog, err := celassert.Compile(expr, celassert.WithVariable("plan"))
		if err != nil {
			errs = append(errs, &ConfigError{
				File: file, Step: step, Assertion: -1,
				Path: fmt.Sprintf("plan.expr[%d]", i),
				Err:  err,
			})
			continue
		}
		programs = append(programs, prog)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	plan.Programs = programs
	return plan, nil
}

func compileLogs(file string, step int, op *LogsOp) (*PreparedLogs, error) {
	compiled, raw, err := compilePatterns(file, step, -1, "logs.contains", op.Contains)
	if err != nil {
		return nil, err
	}
	return &PreparedLogs{Component: op.Component, Patterns: compiled, Raw: raw}, nil
}

func compileAsserts(file string, step int, assertions []ResourceAssertion) ([]PreparedAssert, []error) {
	out := make([]PreparedAssert, 0, len(assertions))
	var errs []error
	for j, assertion := range assertions {
		prepared := PreparedAssert{
			Select:        assertion.Select,
			HasExpect:     assertion.Expect != nil,
			Expect:        assertion.Expect,
			Expr:          assertion.Expr,
			StopOnFailure: assertion.StopOnFailureEnabled(),
		}
		programs := make([]*celassert.Program, 0, len(assertion.Expr))
		ok := true
		for i, expr := range assertion.Expr {
			prog, err := celassert.Compile(expr, celassert.WithVariable("objects"))
			if err != nil {
				ok = false
				errs = append(errs, &ConfigError{
					File: file, Step: step, Assertion: j,
					Path: fmt.Sprintf("expr[%d]", i),
					Err:  err,
				})
				continue
			}
			programs = append(programs, prog)
		}
		if ok {
			prepared.Programs = programs
			out = append(out, prepared)
		}
	}
	return out, errs
}

func compilePatterns(file string, step, assertion int, path string, patterns []string) ([]*regexp.Regexp, []string, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	raw := make([]string, 0, len(patterns))
	var errs []error
	for i, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			errs = append(errs, &ConfigError{
				File: file, Step: step, Assertion: assertion,
				Path: fmt.Sprintf("%s[%d]", path, i),
				Err:  err,
			})
			continue
		}
		compiled = append(compiled, re)
		raw = append(raw, pattern)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}
	return compiled, raw, nil
}
