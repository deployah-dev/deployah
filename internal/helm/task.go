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

package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"deployah.dev/deployah/internal/spec"
)

const hookDeletePolicy = "before-hook-creation,hook-succeeded"

// createTaskSubCharts creates a sub-chart directory for each hook and
// scheduled task name. Manual tasks and tasks from other environments are
// absent from those lists and get no subchart.
func createTaskSubCharts(chartDir string, hookNames, scheduledNames []string) error {
	chartsDir := filepath.Join(chartDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o750); err != nil {
		return fmt.Errorf("failed to create charts directory: %w", err)
	}

	for _, name := range hookNames {
		if err := createOneTaskSubChart(chartsDir, name, createTaskJobTemplate); err != nil {
			return err
		}
	}
	for _, name := range scheduledNames {
		if err := createOneTaskSubChart(chartsDir, name, createTaskCronJobTemplate); err != nil {
			return err
		}
	}
	return nil
}

func createOneTaskSubChart(chartsDir, name string, writeTemplate func(string) error) error {
	taskChartDir := filepath.Join(chartsDir, name)
	if err := os.MkdirAll(taskChartDir, 0o750); err != nil {
		return fmt.Errorf("failed to create task chart directory for %s: %w", name, err)
	}
	if err := createComponentChartYAML(taskChartDir, name); err != nil {
		return fmt.Errorf("failed to create Chart.yaml for task %s: %w", name, err)
	}
	templatesDir := filepath.Join(taskChartDir, "templates")
	if err := os.MkdirAll(templatesDir, 0o750); err != nil {
		return fmt.Errorf("failed to create templates directory for task %s: %w", name, err)
	}
	if err := writeTemplate(templatesDir); err != nil {
		return fmt.Errorf("failed to create template for task %s: %w", name, err)
	}
	return nil
}

func createTaskJobTemplate(templatesDir string) error {
	body := `{{- include "deployah.job" . -}}`
	return os.WriteFile(filepath.Join(templatesDir, "job.yaml"), []byte(body), 0o600)
}

func createTaskCronJobTemplate(templatesDir string) error {
	body := `{{- include "deployah.cronjob" . -}}`
	return os.WriteFile(filepath.Join(templatesDir, "cronjob.yaml"), []byte(body), 0o600)
}

// mergeTaskNames returns the sorted union of hook and scheduled task names
// for Chart.yaml import-values.
func mergeTaskNames(hookNames, scheduledNames []string) []string {
	names := append(slices.Clone(hookNames), scheduledNames...)
	slices.Sort(names)
	return names
}

// hookTaskNames returns the sorted names of the hook tasks that get a
// sub-chart in this environment.
func hookTaskNames(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) ([]string, error) {
	hooks, err := hookTasksForChart(m, desiredEnvironment, resolved)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(hooks))
	for name := range hooks {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// scheduledTaskNames returns the sorted names of the scheduled tasks that
// get a CronJob sub-chart in this environment.
func scheduledTaskNames(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) ([]string, error) {
	scheduled, err := scheduledTasksForChart(m, desiredEnvironment, resolved)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(scheduled))
	for name := range scheduled {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// hookTasksForChart returns merged hook tasks that belong in this
// environment. Manual and scheduled tasks are omitted.
func hookTasksForChart(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) (map[string]spec.ResolvedTask, error) {
	return tasksForChart(m, desiredEnvironment, resolved, func(rt spec.ResolvedTask) bool {
		return rt.Task.On.IsHook()
	})
}

// scheduledTasksForChart returns merged scheduled tasks that belong in
// this environment.
func scheduledTasksForChart(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) (map[string]spec.ResolvedTask, error) {
	return tasksForChart(m, desiredEnvironment, resolved, func(rt spec.ResolvedTask) bool {
		return rt.Task.On.IsScheduled()
	})
}

func tasksForChart(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec, keep func(spec.ResolvedTask) bool) (map[string]spec.ResolvedTask, error) {
	all, err := spec.EffectiveTasks(m, desiredEnvironment, resolved)
	if err != nil {
		return nil, err
	}
	out := make(map[string]spec.ResolvedTask, len(all))
	for name, rt := range all {
		if keep(rt) {
			out[name] = rt
		}
	}
	return out, nil
}

func applyTaskChartValues(values map[string]any, m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) (map[string]any, error) {
	resolvedTasks := make(map[string]any)
	hooks, err := hookTasksForChart(m, desiredEnvironment, resolved)
	if err != nil {
		return nil, err
	}
	for name, rt := range hooks {
		taskValues, mapErr := mapTaskToChartValues(m, name, rt, desiredEnvironment)
		if mapErr != nil {
			return nil, fmt.Errorf("task %s: %w", name, mapErr)
		}
		values[name] = taskValues
		resolvedTasks[name] = map[string]any{
			"on":         string(rt.Task.On),
			"hookWeight": rt.HookWeight,
			"timeout":    rt.Task.Timeout,
		}
	}
	if schedErr := applyScheduledTaskChartValues(values, resolvedTasks, m, desiredEnvironment, resolved); schedErr != nil {
		return nil, schedErr
	}
	return resolvedTasks, nil
}

func applyScheduledTaskChartValues(values, resolvedTasks map[string]any, m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) error {
	scheduled, err := scheduledTasksForChart(m, desiredEnvironment, resolved)
	if err != nil {
		return err
	}
	for name, rt := range scheduled {
		taskValues, mapErr := mapScheduledTaskToChartValues(m, name, rt, desiredEnvironment)
		if mapErr != nil {
			return fmt.Errorf("task %s: %w", name, mapErr)
		}
		values[name] = taskValues
		resolvedTasks[name] = map[string]any{
			"on":                string(rt.Task.On),
			"timeout":           rt.Task.Timeout,
			"schedule":          rt.Task.Schedule,
			"timeZone":          rt.Task.TimeZone,
			"concurrencyPolicy": rt.Task.ConcurrencyPolicy,
		}
	}
	return nil
}

func mapTaskToChartValues(m *spec.Spec, name string, rt spec.ResolvedTask, desiredEnvironment string) (map[string]any, error) {
	values, fields, err := taskBaseChartValues(m, name, rt, desiredEnvironment)
	if err != nil {
		return nil, err
	}

	job := map[string]any{
		"enabled":          true,
		"hook":             rt.Task.HelmHookEvents(),
		"hookWeight":       rt.HookWeight,
		"hookDeletePolicy": hookDeletePolicy,
		"completions":      int(fields.Completions),
		"parallelism":      int(fields.Parallelism),
		"backoffLimit":     int(fields.BackoffLimit),
	}
	if fields.ActiveDeadlineSeconds != nil {
		job["activeDeadlineSeconds"] = int(*fields.ActiveDeadlineSeconds)
	}
	if fields.TTLSecondsAfterFinished != nil {
		job["ttlSecondsAfterFinished"] = int(*fields.TTLSecondsAfterFinished)
	}
	values["job"] = job
	return values, nil
}

func mapScheduledTaskToChartValues(m *spec.Spec, name string, rt spec.ResolvedTask, desiredEnvironment string) (map[string]any, error) {
	values, fields, err := taskBaseChartValues(m, name, rt, desiredEnvironment)
	if err != nil {
		return nil, err
	}

	deadline, err := scheduledActiveDeadlineSeconds(rt.Task)
	if err != nil {
		return nil, err
	}

	timeZone := rt.Task.TimeZone
	if timeZone == "" {
		timeZone = spec.DefaultScheduleTimeZone
	}
	policy := rt.Task.ConcurrencyPolicy
	if policy == "" {
		policy = spec.DefaultConcurrencyPolicy
	}
	suspend := false
	if rt.Task.Suspend != nil {
		suspend = *rt.Task.Suspend
	}

	cronjob := map[string]any{
		"enabled":                    true,
		"schedule":                   rt.Task.Schedule,
		"timeZone":                   timeZone,
		"concurrencyPolicy":          policy,
		"suspend":                    suspend,
		"successfulJobsHistoryLimit": spec.DefaultSuccessfulJobsHistory,
		"failedJobsHistoryLimit":     spec.DefaultFailedJobsHistory,
		"completions":                int(fields.Completions),
		"parallelism":                int(fields.Parallelism),
		"backoffLimit":               int(fields.BackoffLimit),
		"activeDeadlineSeconds":      deadline,
	}
	if fields.TTLSecondsAfterFinished != nil {
		cronjob["ttlSecondsAfterFinished"] = int(*fields.TTLSecondsAfterFinished)
	}
	values["cronjob"] = cronjob
	return values, nil
}

// scheduledActiveDeadlineSeconds is the CronJob-only cluster deadline.
// An empty timeout becomes [spec.DefaultScheduledTaskTimeout].
func scheduledActiveDeadlineSeconds(task spec.Task) (int, error) {
	timeout := task.Timeout
	if timeout == "" {
		timeout = spec.DefaultScheduledTaskTimeout
	}
	sec, err := spec.ParseDuration(timeout)
	if err != nil {
		return 0, fmt.Errorf("timeout: %w", err)
	}
	return int(sec), nil
}

func taskBaseChartValues(m *spec.Spec, name string, rt spec.ResolvedTask, desiredEnvironment string) (map[string]any, spec.TaskJobSpec, error) {
	fields, err := spec.NewTaskJobSpec(rt.Task, 0, 0)
	if err != nil {
		return nil, spec.TaskJobSpec{}, err
	}
	image, tag := parseContainerImage(fields.Image)

	requests := map[string]any{}
	if fields.Resources.CPU != nil && !fields.Resources.CPU.IsZero() {
		requests["cpu"] = fields.Resources.CPU.String()
	}
	if fields.Resources.Memory != nil && !fields.Resources.Memory.IsZero() {
		requests["memory"] = fields.Resources.Memory.String()
	}
	if fields.Resources.EphemeralStorage != nil && !fields.Resources.EphemeralStorage.IsZero() {
		requests["ephemeral-storage"] = fields.Resources.EphemeralStorage.String()
	}
	resources := map[string]any{}
	if len(requests) > 0 {
		resources["requests"] = requests
	}

	imageValues := map[string]any{"repository": image}
	if tag != "" {
		if strings.HasPrefix(tag, "sha256:") {
			imageValues["digest"] = tag
		} else {
			imageValues["tag"] = tag
		}
	}

	values := map[string]any{
		"commonLabels": map[string]string{
			spec.LabelProject:     m.Project,
			spec.LabelComponent:   name,
			spec.LabelEnvironment: spec.NormalizeEnv(desiredEnvironment).K8sSafe,
		},
		"commonAnnotations": map[string]string{
			spec.AnnotationSource:  spec.SourceSpec,
			spec.AnnotationProject: m.Project,
		},
		"image":     imageValues,
		"resources": resources,
		"service": map[string]any{
			"enabled": false,
		},
	}
	if len(fields.Command) > 0 {
		values["command"] = fields.Command
	}
	if len(fields.Args) > 0 {
		values["args"] = fields.Args
	}
	if len(fields.Env) > 0 {
		values["envVars"] = fields.Env
	}
	if applyErr := applyMergedProfile(values, rt.MergedProfile); applyErr != nil {
		return nil, spec.TaskJobSpec{}, applyErr
	}
	return values, fields, nil
}
