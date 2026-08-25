// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package spec

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// ValidateSpecTasks validates all tasks in spec: name pool and length, from,
// on, after, schedule fields, fanout, command, timeout, and environment
// filter.
func ValidateSpecTasks(spec *Spec) error {
	if spec == nil {
		return fmt.Errorf("spec cannot be nil")
	}
	if len(spec.Tasks) == 0 {
		return nil
	}

	var errs []error
	for _, name := range spec.TaskNames() {
		task := spec.Tasks[name]
		if err := ValidateComponentName(name); err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", name, err))
		}
		if len(name) > MaxTaskNameLength {
			errs = append(errs, fmt.Errorf("task %s: name must be at most %d characters", name, MaxTaskNameLength))
		}
		if _, exists := spec.Components[name]; exists {
			errs = append(errs, fmt.Errorf("task %s: name collides with a component", name))
		}
		if err := validateTask(name, task, spec); err != nil {
			errs = append(errs, err)
		}
	}
	if err := validateTaskAfterGraph(spec); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("task validation failed: %w", errors.Join(errs...))
	}
	return nil
}

func validateTask(name string, task Task, spec *Spec) error {
	var errs []error
	prefix := fmt.Sprintf("task %s", name)

	if task.From == "" && task.Image == "" {
		errs = append(errs, fmt.Errorf("%s: from or image is required", prefix))
	}
	if task.From != "" {
		if _, ok := spec.Components[task.From]; !ok {
			errs = append(errs, fmt.Errorf("%s: from %q does not name a component", prefix, task.From))
		}
	}

	switch task.On {
	case TaskOnPreDeploy, TaskOnPostDeploy, TaskOnManual, TaskOnSchedule:
	case "":
		errs = append(errs, fmt.Errorf("%s: on is required (preDeploy, postDeploy, manual, or schedule)", prefix))
	default:
		errs = append(errs, fmt.Errorf("%s: on %q is invalid (preDeploy, postDeploy, manual, or schedule)", prefix, task.On))
	}

	if task.On.IsScheduled() {
		if err := validateCronSchedule(task.Schedule); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
		}
		if err := validateTaskTimeZone(task.TimeZone); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
		}
		if err := validateConcurrencyPolicy(task.ConcurrencyPolicy); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
		}
	} else {
		if task.Schedule != "" {
			errs = append(errs, fmt.Errorf("%s: schedule is only valid when on is schedule", prefix))
		}
		if task.TimeZone != "" {
			errs = append(errs, fmt.Errorf("%s: timeZone is only valid when on is schedule", prefix))
		}
		if task.ConcurrencyPolicy != "" {
			errs = append(errs, fmt.Errorf("%s: concurrencyPolicy is only valid when on is schedule", prefix))
		}
		if task.Suspend != nil {
			errs = append(errs, fmt.Errorf("%s: suspend is only valid when on is schedule", prefix))
		}
	}

	if len(task.After) > 0 && (task.On == TaskOnManual || task.On.IsScheduled()) {
		if task.On.IsScheduled() {
			errs = append(errs, fmt.Errorf("%s: after is not allowed on scheduled tasks", prefix))
		} else {
			errs = append(errs, fmt.Errorf("%s: after is not allowed on manual tasks", prefix))
		}
	}
	for _, dep := range task.After {
		if strings.TrimSpace(dep) == "" {
			errs = append(errs, fmt.Errorf("%s: after contains an empty name", prefix))
			continue
		}
		if dep == name {
			errs = append(errs, fmt.Errorf("%s: after cannot include itself", prefix))
		}
	}

	usesParent := task.Image == "" && task.From != ""
	if usesParent && len(task.Command) == 0 {
		errs = append(errs, fmt.Errorf("%s: command is required when using the parent image", prefix))
	}

	count := task.Fanout.EffectiveCount()
	parallelism := task.Fanout.EffectiveParallelism()
	if count < 1 {
		errs = append(errs, fmt.Errorf("%s: fanout.count must be at least 1", prefix))
	} else if _, err := toInt32("fanout.count", count); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
	}
	if parallelism < 1 {
		errs = append(errs, fmt.Errorf("%s: fanout.parallelism must be at least 1", prefix))
	} else if parallelism > MaxFanoutParallelism {
		errs = append(errs, fmt.Errorf("%s: fanout.parallelism must be at most %d", prefix, MaxFanoutParallelism))
	}
	if parallelism > count {
		errs = append(errs, fmt.Errorf("%s: fanout.parallelism must be less than or equal to fanout.count", prefix))
	}
	if task.BackoffLimit != nil {
		if _, err := toInt32("backoffLimit", *task.BackoffLimit); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
		}
	}
	if task.TTLSecondsAfterFinished != nil {
		if _, err := toInt32("ttlSecondsAfterFinished", *task.TTLSecondsAfterFinished); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
		}
	}

	if err := validateResources(task.Resources, task.ResourcePreset); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
	}

	if err := ValidateComponentEnvironmentFilter(Component{Environments: task.Environments}); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
	}
	if err := ValidateComponentProfiles(Component{Profiles: task.Profiles}); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", prefix, err))
	}

	if task.Timeout != "" {
		if _, err := ParseDuration(task.Timeout); err != nil {
			errs = append(errs, fmt.Errorf("%s: timeout: %w", prefix, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// validateCronSchedule mirrors Kubernetes CronJob schedule validation: a
// "TZ" substring check, then [cron.ParseStandard]. The parse is wrapped
// because cron.ParseStandard panics on malformed input such as "TZ=0".
func validateCronSchedule(schedule string) (err error) {
	if schedule == "" {
		return errors.New("schedule is required when on is schedule")
	}
	if strings.Contains(schedule, "TZ") {
		return fmt.Errorf("schedule %q: TZ and CRON_TZ are not supported; use the timeZone field", schedule)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("schedule %q: invalid format: %v", schedule, r)
		}
	}()
	if _, perr := cron.ParseStandard(schedule); perr != nil {
		return fmt.Errorf("schedule %q: %w", schedule, perr)
	}
	return nil
}

func validateTaskTimeZone(tz string) error {
	if tz == "" {
		return nil
	}
	if tz == "Local" {
		return fmt.Errorf("timeZone %q is not a valid IANA name", tz)
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("timeZone %q is not a valid IANA name: %w", tz, err)
	}
	return nil
}

func validateConcurrencyPolicy(policy string) error {
	if policy == "" {
		return nil
	}
	switch policy {
	case "Allow", "Forbid", "Replace":
		return nil
	default:
		return fmt.Errorf("concurrencyPolicy %q is invalid (Allow, Forbid, or Replace)", policy)
	}
}

func validateTaskAfterGraph(spec *Spec) error {
	var errs []error
	tasks := spec.Tasks
	for _, name := range slices.Sorted(maps.Keys(tasks)) {
		task := tasks[name]
		if !task.On.IsHook() {
			continue
		}
		dependent, _ := spec.MergedTask(name)
		for _, dep := range task.After {
			other, ok := tasks[dep]
			if !ok {
				errs = append(errs, fmt.Errorf("task %s: after %q does not name a task", name, dep))
				continue
			}
			if other.On != task.On {
				errs = append(errs, fmt.Errorf("task %s: after %q is not in the same on phase (%s)", name, dep, task.On))
				continue
			}
			depTask, _ := spec.MergedTask(dep)
			if !afterCoversDependentEnvs(dependent, depTask) {
				errs = append(errs, fmt.Errorf("task %s: after %q is not active in every environment where %s runs", name, dep, name))
			}
		}
	}
	if _, err := AssignHookWeights(tasks); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// afterCoversDependentEnvs reports whether dep runs in every environment
// dependent runs in. An empty environments list means every environment.
func afterCoversDependentEnvs(dependent, dep Task) bool {
	if len(dep.Environments) == 0 {
		return true
	}
	if len(dependent.Environments) == 0 {
		return false
	}
	for _, env := range dependent.Environments {
		if _, ok := matchEnvKey(env, dep.Environments); !ok {
			return false
		}
	}
	return true
}

// CheckHookTaskTimeouts reports hook tasks whose timeout is not strictly
// less than limit (the CLI --timeout). Manual tasks are skipped. An empty
// timeout is treated as [DefaultHookTaskTimeout].
func CheckHookTaskTimeouts(tasks map[string]Task, limit time.Duration) error {
	if limit <= 0 {
		return fmt.Errorf("session timeout must be a positive duration")
	}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(tasks)) {
		task := tasks[name]
		if !task.On.IsHook() {
			continue
		}
		if err := taskTimeoutAgainstLimit(name, task, limit, ""); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// CheckTaskTimeout reports when task's timeout is not strictly less than
// limit (the CLI --timeout). An empty timeout on a hook is treated as
// [DefaultHookTaskTimeout]. An empty timeout on a manual or scheduled
// task is skipped because the Job has no deadline. When the timeout
// exceeds limit, the error names --detach and --timeout so the caller
// can wait in the background or raise the session limit.
func CheckTaskTimeout(name string, task Task, limit time.Duration) error {
	if limit <= 0 {
		return fmt.Errorf("session timeout must be a positive duration")
	}
	return taskTimeoutAgainstLimit(name, task, limit, "use --detach or raise --timeout")
}

func taskTimeoutAgainstLimit(name string, task Task, limit time.Duration, hint string) error {
	timeout := task.Timeout
	if timeout == "" {
		if !task.On.IsHook() {
			return nil
		}
		timeout = DefaultHookTaskTimeout
	}
	sec, err := ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("task %s: timeout: %w", name, err)
	}
	if time.Duration(sec)*time.Second >= limit {
		msg := fmt.Sprintf("task %s: timeout %s must be less than --timeout %s", name, timeout, limit)
		if hint != "" {
			return fmt.Errorf("%s (%s)", msg, hint)
		}
		return errors.New(msg)
	}
	return nil
}
