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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"deployah.dev/deployah/internal/testing/celassert"

	inttest "deployah.dev/deployah/internal/testing"
)

const (
	// ResourcePollInterval is the pause between Kubernetes assertion reads.
	ResourcePollInterval = 2 * time.Second
	// LogsPollInterval is the pause between log reads.
	LogsPollInterval = 5 * time.Second
)

// CommandResult is one finished CLI invocation.
type CommandResult struct {
	Stdout      string
	Stderr      string
	ExitCode    int
	NotExecuted bool
	Interrupted bool
	// Retryable marks a nonzero exit that may succeed on a later poll.
	// The caller sets it. A nonzero exit alone is terminal.
	Retryable bool
}

// ExecFunc runs one deployah invocation. dir may be empty.
type ExecFunc func(ctx context.Context, dir string, args []string) CommandResult

// ObserveFunc reads the objects for sel. An empty slice means nothing matched.
type ObserveFunc func(ctx context.Context, sel ResourceSelect, scenarioNS string) ([]map[string]any, error)

// ErrObserveRetry means the resource read should be tried again.
var ErrObserveRetry = errors.New("resource observation should retry")

// ErrObserveTerminal means the resource read will not succeed later.
var ErrObserveTerminal = errors.New("resource observation failed")

// RunInput is one prepared scenario.
// Exec and Observe talk to the CLI and the cluster.
type RunInput struct {
	Dir          string
	Namespace    string
	Project      string
	Env          string
	Fixture      PreparedFixture
	Exec         ExecFunc
	Observe      ObserveFunc
	ResourcePoll time.Duration
	LogsPoll     time.Duration
}

// Run executes every step on that step's deadline.
// It does not create or delete namespaces.
func Run(ctx context.Context, in RunInput) RunResult {
	if ctx == nil {
		return RunResult{Steps: []StepResult{{
			Op: "scenario", Status: StatusError, Detail: "context is nil", Unsafe: true,
		}}}
	}
	if in.ResourcePoll <= 0 {
		in.ResourcePoll = ResourcePollInterval
	}
	if in.LogsPoll <= 0 {
		in.LogsPoll = LogsPollInterval
	}
	steps := make([]StepResult, 0, len(in.Fixture.Steps))
	stop := false
	for i, step := range in.Fixture.Steps {
		if stop {
			steps = append(steps, StepResult{
				Index: i, Op: step.Op, Status: StatusSkip, Detail: skipReason(steps),
			})
			continue
		}
		result := runOne(ctx, in, step, i)
		steps = append(steps, result)
		if result.Unsafe || result.StoppedHere {
			stop = true
			continue
		}
		if (result.Status == StatusFail || result.Status == StatusError) && step.StopOnFailure {
			stop = true
		}
	}
	return RunResult{Steps: steps}
}

func runOne(parent context.Context, in RunInput, step PreparedStep, index int) StepResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, step.Timeout)
	defer cancel()
	var result StepResult
	switch step.Op {
	case "assert":
		result = runAssert(ctx, in, step, index)
	case "logs":
		result = runLogs(ctx, in, step, index)
	default:
		result = runCommand(ctx, in, step, index)
	}
	result.Elapsed = time.Since(start)
	result.Timeout = step.Timeout
	return result
}

func selectLabel(sel ResourceSelect) string {
	label := sel.APIVersion + " " + sel.Kind
	if sel.Name != "" {
		label += "/" + sel.Name
	}
	return label
}

func celFalseDetail(index int, expr, diagnosis string) string {
	detail := fmt.Sprintf("expr[%d] %s: false", index, expr)
	if diagnosis == "" {
		return detail
	}
	return detail + "\n" + diagnosis
}

func runCommand(ctx context.Context, in RunInput, step PreparedStep, index int) StepResult {
	base := StepResult{Index: index, Op: step.Op}
	args, err := stepArgs(in, step)
	if err != nil {
		base.Status = StatusError
		base.Detail = err.Error()
		base.Unsafe = true
		return base
	}
	if in.Exec == nil {
		base.Status = StatusError
		base.Detail = "command executor is nil"
		base.Unsafe = true
		return base
	}
	got := in.Exec(ctx, in.Dir, args)
	if got.NotExecuted {
		base.Status = StatusError
		base.Detail = "command was not executed"
		base.Unsafe = true
		return base
	}
	if got.Interrupted || ctx.Err() != nil {
		base.Status = StatusError
		base.Detail = interruptedDetail(ctx, got)
		base.Unsafe = parentCanceled(ctx)
		return base
	}
	if err = CheckOutput(got.Stdout, got.Stderr, got.ExitCode, step.Output); err != nil {
		base.Status = StatusFail
		base.Detail = err.Error()
		return base
	}
	if step.Op == "plan" && step.Plan != nil && len(step.Plan.Programs) > 0 {
		return evalPlan(ctx, base, step.Plan, got.Stdout)
	}
	base.Status = StatusPass
	return base
}

func evalPlan(ctx context.Context, base StepResult, plan *PreparedPlan, stdout string) StepResult {
	var doc any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		base.Status = StatusError
		base.Detail = fmt.Sprintf("plan JSON: %v", err)
		return base
	}
	var problems []string
	for i, prog := range plan.Programs {
		expr := plan.Expr[i]
		err := prog.Eval(ctx, map[string]any{"plan": doc})
		if err == nil {
			continue
		}
		if errors.Is(err, celassert.ErrFalse) {
			problems = append(problems, celFalseDetail(i, expr, prog.Diagnose(ctx, map[string]any{"plan": doc})))
			continue
		}
		base.Status = StatusError
		base.Detail = fmt.Sprintf("expr[%d] %s: %v", i, expr, err)
		base.Unsafe = parentCanceled(ctx)
		return base
	}
	if len(problems) > 0 {
		base.Status = StatusFail
		base.Detail = strings.Join(problems, "; ")
		return base
	}
	base.Status = StatusPass
	return base
}

func runLogs(ctx context.Context, in RunInput, step PreparedStep, index int) StepResult {
	base := StepResult{Index: index, Op: step.Op}
	args, err := stepArgs(in, step)
	if err != nil {
		base.Status = StatusError
		base.Detail = err.Error()
		base.Unsafe = true
		return base
	}
	if in.Exec == nil || step.Logs == nil {
		base.Status = StatusError
		base.Detail = "logs executor is nil"
		base.Unsafe = true
		return base
	}
	var last string
	sawOutput := false
	for {
		if ctx.Err() != nil {
			return logsTimeout(base, sawOutput, last, ctx)
		}
		got := in.Exec(ctx, in.Dir, args)
		if got.NotExecuted {
			base.Status = StatusError
			base.Detail = "command was not executed"
			base.Unsafe = true
			return base
		}
		if got.Interrupted {
			if ctx.Err() != nil {
				return logsTimeout(base, sawOutput, last, ctx)
			}
			base.Status = StatusError
			base.Detail = "logs interrupted"
			base.Unsafe = true
			return base
		}
		if got.ExitCode != 0 {
			last = logsExitDetail(got)
			if !got.Retryable {
				base.Status = StatusError
				base.Detail = last
				return base
			}
			if ctx.Err() != nil {
				return logsTimeout(base, false, last, ctx)
			}
			if !waitPoll(ctx, in.LogsPoll) {
				continue
			}
			continue
		}
		sawOutput = true
		missing := unmatched(step.Logs.Patterns, step.Logs.Raw, got.Stdout)
		if len(missing) == 0 {
			base.Status = StatusPass
			return base
		}
		last = strings.Join(missing, "; ")
		if ctx.Err() != nil {
			return logsTimeout(base, true, last, ctx)
		}
		if !waitPoll(ctx, in.LogsPoll) {
			continue
		}
	}
}

func logsExitDetail(got CommandResult) string {
	detail := fmt.Sprintf("logs exit %d", got.ExitCode)
	if stderr := strings.TrimSpace(got.Stderr); stderr != "" {
		detail += "\nstderr:\n" + stderr
	}
	return detail
}

func logsTimeout(base StepResult, sawOutput bool, last string, ctx context.Context) StepResult {
	cause := ctx.Err()
	if cause == nil {
		cause = context.DeadlineExceeded
	}
	if last != "" {
		base.Detail = last + "; timeout " + cause.Error()
	} else {
		base.Detail = "timeout " + cause.Error()
	}
	base.Unsafe = parentCanceled(ctx)
	if sawOutput && !base.Unsafe {
		base.Status = StatusFail
		return base
	}
	base.Status = StatusError
	return base
}

func runAssert(ctx context.Context, in RunInput, step PreparedStep, index int) StepResult {
	base := StepResult{Index: index, Op: step.Op, Assertions: make([]AssertResult, len(step.Assert))}
	pending := make([]int, 0, len(step.Assert))
	for i := range step.Assert {
		pending = append(pending, i)
	}
	for {
		next, fatal := evalPending(ctx, in, step, base.Assertions, pending)
		if fatal {
			skipPending(base.Assertions, next)
			base.StoppedHere = true
			base.Status = aggregateAssert(base.Assertions)
			base.Unsafe = assertUnsafe(base.Assertions)
			return base
		}
		pending = next
		if len(pending) == 0 {
			base.Status = aggregateAssert(base.Assertions)
			base.Unsafe = assertUnsafe(base.Assertions)
			return base
		}
		if ctx.Err() != nil {
			failPending(base.Assertions, pending, ctx.Err())
			for _, i := range pending {
				if step.Assert[i].StopOnFailure {
					base.StoppedHere = true
				}
			}
			base.Status = aggregateAssert(base.Assertions)
			base.Unsafe = assertUnsafe(base.Assertions)
			return base
		}
		if !waitPoll(ctx, in.ResourcePoll) {
			continue
		}
	}
}

func evalPending(ctx context.Context, in RunInput, step PreparedStep, results []AssertResult, pending []int) ([]int, bool) {
	next := make([]int, 0, len(pending))
	for n, i := range pending {
		status, detail, terminal, unsafe := evalAssert(ctx, in, step.Assert[i])
		if !terminal {
			results[i] = AssertResult{Index: i, Detail: detail}
			next = append(next, i)
			continue
		}
		results[i] = AssertResult{Index: i, Status: status, Detail: detail, Unsafe: unsafe}
		if status != StatusPass && (assertionStop(step.Assert[i], status) || unsafe) {
			skipPending(results, pending[n+1:])
			return next, true
		}
	}
	return next, false
}

func assertionStop(assertion PreparedAssert, status Status) bool {
	return assertion.StopOnFailure && status != StatusPass
}

func evalAssert(ctx context.Context, in RunInput, assertion PreparedAssert) (Status, string, bool, bool) {
	if in.Observe == nil {
		return StatusError, "resource observer is nil", true, true
	}
	objects, err := in.Observe(ctx, assertion.Select, in.Namespace)
	if err != nil {
		if errors.Is(err, ErrObserveRetry) && ctx.Err() == nil {
			return StatusFail, err.Error(), false, false
		}
		if ctx.Err() != nil {
			return StatusFail, err.Error() + "; timeout " + ctx.Err().Error(), true, parentCanceled(ctx)
		}
		unsafe := errors.Is(err, ErrObserveTerminal)
		return StatusError, err.Error(), true, unsafe
	}
	if objects == nil {
		objects = []map[string]any{}
	}
	problems := expectProblems(assertion, objects)
	exprProblems, evalErr := exprProblems(ctx, assertion, objects)
	if evalErr != nil {
		return StatusError, evalErr.Error(), true, false
	}
	problems = append(problems, exprProblems...)
	if len(problems) == 0 {
		return StatusPass, "", true, false
	}
	detail := strings.Join(problems, "; ")
	if ctx.Err() != nil {
		return StatusFail, detail + "; timeout " + ctx.Err().Error(), true, parentCanceled(ctx)
	}
	return StatusFail, detail, false, false
}

func expectProblems(assertion PreparedAssert, objects []map[string]any) []string {
	if !assertion.HasExpect {
		return nil
	}
	if len(objects) == 0 {
		return []string{"no resources selected: " + selectLabel(assertion.Select) + " observed 0"}
	}
	var problems []string
	for i, obj := range objects {
		for _, diff := range inttest.DiffSubset("$", assertion.Expect, obj) {
			problems = append(problems, fmt.Sprintf("object[%d] %s", i, diff))
		}
	}
	return problems
}

func exprProblems(ctx context.Context, assertion PreparedAssert, objects []map[string]any) ([]string, error) {
	list := make([]any, 0, len(objects))
	for i := range objects {
		list = append(list, objects[i])
	}
	var problems []string
	for i, prog := range assertion.Programs {
		expr := ""
		if i < len(assertion.Expr) {
			expr = assertion.Expr[i]
		}
		err := prog.Eval(ctx, map[string]any{"objects": list})
		if err == nil {
			continue
		}
		if errors.Is(err, celassert.ErrFalse) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			if errors.Is(err, celassert.ErrFalse) {
				problems = append(problems, celFalseDetail(i, expr, prog.Diagnose(ctx, map[string]any{"objects": list})))
				continue
			}
			problems = append(problems, fmt.Sprintf("expr[%d] %s: %v", i, expr, err))
			continue
		}
		return nil, fmt.Errorf("expr[%d] %s: %w", i, expr, err)
	}
	return problems, nil
}

func skipPending(results []AssertResult, pending []int) {
	for _, i := range pending {
		results[i] = AssertResult{
			Index: i, Status: StatusSkip, Detail: "skipped after terminal assertion failure",
		}
	}
}

func failPending(results []AssertResult, pending []int, cause error) {
	for _, i := range pending {
		detail := results[i].Detail
		if detail == "" {
			detail = "deadline exceeded"
		}
		detail += "; timeout " + cause.Error()
		results[i] = AssertResult{Index: i, Status: StatusFail, Detail: detail}
	}
}

func aggregateAssert(results []AssertResult) Status {
	status := StatusPass
	for _, result := range results {
		switch result.Status {
		case StatusError:
			return StatusError
		case StatusFail:
			status = StatusFail
		}
	}
	return status
}

func assertUnsafe(results []AssertResult) bool {
	for _, result := range results {
		if result.Unsafe {
			return true
		}
	}
	return false
}

func stepArgs(in RunInput, step PreparedStep) ([]string, error) {
	nsFlag := []string{"--context", KindContext, "--namespace", in.Namespace}
	switch step.Op {
	case "deploy":
		args := []string{"deploy", in.Env}
		args = append(args, nsFlag...)
		if step.Deploy != nil && step.Deploy.Spec != "" {
			args = append(args, "--spec", step.Deploy.Spec)
		}
		if step.Deploy != nil {
			args = append(args, step.Deploy.Args...)
		}
		return args, nil
	case "plan":
		args := []string{"plan", in.Env, "-o", "json"}
		args = append(args, nsFlag...)
		if step.Plan != nil && step.Plan.DetailedExitCode {
			args = append(args, "--detailed-exitcode")
		}
		return args, nil
	case "run":
		if step.Run == nil || step.Run.Task == "" {
			return nil, errors.New("run.task is required")
		}
		args := []string{"run", step.Run.Task, in.Env, "--yes"}
		return append(args, nsFlag...), nil
	case "logs":
		if step.Logs == nil || step.Logs.Component == "" {
			return nil, errors.New("logs.component is required")
		}
		return []string{
			"logs", in.Project,
			"--component=" + step.Logs.Component,
			"--environment=" + in.Env,
			"--no-follow",
			"--context", KindContext,
			"--namespace", in.Namespace,
		}, nil
	case "delete":
		args := []string{"delete", in.Project, in.Env, "--yes", "--wait", "--allow-missing-platform"}
		return append(args, nsFlag...), nil
	case "assert":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown operation %q", step.Op)
	}
}

func unmatched(patterns []*regexp.Regexp, raw []string, text string) []string {
	var missing []string
	for i, re := range patterns {
		if re.MatchString(text) {
			continue
		}
		pattern := re.String()
		if i < len(raw) {
			pattern = raw[i]
		}
		missing = append(missing, fmt.Sprintf("logs missing pattern %q", pattern))
	}
	return missing
}

func waitPoll(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = time.Millisecond
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func interruptedDetail(ctx context.Context, got CommandResult) string {
	detail := "command interrupted"
	if ctx.Err() != nil {
		detail = "timeout " + ctx.Err().Error()
	}
	if strings.TrimSpace(got.Stderr) != "" {
		detail += "\nstderr:\n" + strings.TrimSpace(got.Stderr)
	}
	return detail
}

func parentCanceled(ctx context.Context) bool {
	return ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) && !errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func skipReason(prev []StepResult) string {
	for _, step := range slices.Backward(prev) {
		if step.Status == StatusSkip {
			continue
		}
		return fmt.Sprintf("skipped after steps[%d] %s", step.Index, step.Op)
	}
	return "skipped"
}
