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

// createTaskSubCharts creates a sub-chart directory for each name in
// taskNames, as returned by [hookTaskNames]. Manual tasks and tasks from
// other environments are absent from that list and get no subchart.
func createTaskSubCharts(chartDir string, taskNames []string) error {
	chartsDir := filepath.Join(chartDir, "charts")
	if err := os.MkdirAll(chartsDir, 0o750); err != nil {
		return fmt.Errorf("failed to create charts directory: %w", err)
	}

	for _, name := range taskNames {
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
		if err := createTaskJobTemplate(templatesDir); err != nil {
			return fmt.Errorf("failed to create job.yaml template for task %s: %w", name, err)
		}
	}
	return nil
}

func createTaskJobTemplate(templatesDir string) error {
	body := `{{- include "deployah.job" . -}}`
	return os.WriteFile(filepath.Join(templatesDir, "job.yaml"), []byte(body), 0o600)
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

// hookTasksForChart returns merged hook tasks that belong in this
// environment. Manual tasks are omitted.
func hookTasksForChart(m *spec.Spec, desiredEnvironment string, resolved *spec.ResolvedSpec) (map[string]spec.ResolvedTask, error) {
	all, err := spec.EffectiveTasks(m, desiredEnvironment, resolved)
	if err != nil {
		return nil, err
	}
	out := make(map[string]spec.ResolvedTask, len(all))
	for name, rt := range all {
		if rt.Task.On.IsHook() {
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
	return resolvedTasks, nil
}

func mapTaskToChartValues(m *spec.Spec, name string, rt spec.ResolvedTask, desiredEnvironment string) (map[string]any, error) {
	fields, err := spec.NewTaskJobSpec(rt.Task, 0, 0)
	if err != nil {
		return nil, err
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
		"job":       job,
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
		return nil, applyErr
	}
	return values, nil
}
