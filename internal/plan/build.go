// Copyright 2025 The Deployah Authors
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

package plan

import (
	"context"
	"fmt"
	"slices"

	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// BuildClient is the subset of
// [deployah.dev/deployah/internal/session.HelmClient] that [BuildPlan]
// needs: render the chart client-side and read release history. Defined
// narrowly, like [historyClient], so this package does not depend on
// internal/session and tests can inject a minimal fake.
type BuildClient interface {
	historyClient
	RenderManifests(ctx context.Context, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (*render.RenderResult, func(), error)
}

// BuildPlan renders manifest for environment via client and diffs the
// result against the last successful release, returning the fully
// populated Plan (Header included) alongside the render result. It is the
// single render-diff-header pipeline shared by `deployah plan` and the
// plan `deployah deploy` shows before confirming.
//
// The caller must invoke the returned cleanup func once done with
// result.ChartPath (same contract as [helm.Client.RenderManifests]). On
// error, cleanup is still returned when a chart was prepared and must be
// called. postRenderer, when non-nil, is forwarded to RenderManifests so
// extras appear in the diff.
func BuildPlan(ctx context.Context, client BuildClient, manifest *spec.Spec, environment, clusterContext string, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (*Plan, *render.RenderResult, func(), error) {
	result, cleanup, err := client.RenderManifests(ctx, manifest, environment, resolved, postRenderer)
	if cleanup == nil {
		cleanup = func() {}
	}
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("render manifests: %w", err)
	}

	prevRelease, warning, err := LastSuccessfulRelease(ctx, client, manifest.Project, environment)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("release history: %w", err)
	}

	var previousManifest string
	var previousHooks []*v1.Hook
	revision := 0
	if prevRelease != nil {
		previousManifest = prevRelease.Manifest
		previousHooks = prevRelease.Hooks
		revision = prevRelease.Version
	}

	p, err := ComputeDiff(previousManifest, result.Manifest)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("compute diff: %w", err)
	}
	p.HooksChanged = HooksChanged(previousHooks, result.Hooks)
	p.Header = Header{
		Project:      manifest.Project,
		Environment:  environment,
		Release:      result.ReleaseName,
		Namespace:    result.Namespace,
		Context:      clusterContext,
		Revision:     revision,
		FreshInstall: prevRelease == nil,
		Warning:      warning,
	}
	p.Tasks, err = TasksFromSpec(manifest, environment, resolved)
	if err != nil {
		return nil, nil, cleanup, fmt.Errorf("tasks: %w", err)
	}

	return p, result, cleanup, nil
}

// TasksFromSpec builds the plan Tasks section from the spec. When resolved
// is nil, only tasks that apply to environment are included.
func TasksFromSpec(manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) ([]PlannedTask, error) {
	tasks, err := spec.EffectiveTasks(manifest, environment, resolved)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tasks))
	for name := range tasks {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]PlannedTask, 0, len(names))
	for _, name := range names {
		rt := tasks[name]
		out = append(out, PlannedTask{
			Name:       name,
			On:         plannedTaskOn(rt.Task.On),
			Timeout:    rt.Task.Timeout,
			HookWeight: rt.HookWeight,
			Manual:     rt.Task.On == spec.TaskOnManual,
		})
	}
	return out, nil
}

func plannedTaskOn(on spec.TaskOn) string {
	switch on {
	case spec.TaskOnPreDeploy:
		return TaskOnPreDeploy
	case spec.TaskOnPostDeploy:
		return TaskOnPostDeploy
	case spec.TaskOnManual:
		return TaskOnManual
	default:
		return string(on)
	}
}
