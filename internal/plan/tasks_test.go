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

package plan

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestAssembleTasks_FreshInstallPrePost(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
		"smoke":   hookResolved(spec.TaskOnPostDeploy, 2),
		"run":     {Task: spec.Task{On: spec.TaskOnManual}},
	})
	tasks, err := assembleTasks(resolved, helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1}, []*v1.Hook{
		testHook("ConfigMap", "migrate-env", "migrate", "v1", 0),
		testHook("Job", "migrate", "migrate", "v1", 1),
		testHook("Job", "smoke", "smoke", "v1", 2),
	}, nil)
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "migrate")
	require.Contains(t, byName, "smoke")
	assert.NotContains(t, byName, "run")
	assert.Equal(t, semantic.TaskCreate, byName["migrate"].Action)
	assert.True(t, byName["migrate"].WillRun)
	assert.Equal(t, semantic.TaskPreDeploy, byName["migrate"].Phase)
	assert.Len(t, byName["migrate"].Definitions, 2)
	assert.Equal(t, semantic.TaskCreate, byName["smoke"].Action)
	assert.True(t, byName["smoke"].WillRun)
	assert.Empty(t, byName["migrate"].Resources)
}

func TestAssembleTasks_ManifestChangeUnchangedHookWillRun(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"seed": hookResolved(spec.TaskOnPreDeploy, 1),
	})
	hook := testHook("Job", "seed", "seed", "same", 1)
	prep := upgradePrep(map[string]any{"seed": map[string]any{"on": "preDeploy", "hookWeight": 1}}, []*v1.Hook{hook}, nil)
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{hook}, []semantic.ResourceChange{labeledCreate("api", "api")})
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "seed")
	assert.Equal(t, semantic.TaskUnchanged, byName["seed"].Action)
	assert.True(t, byName["seed"].WillRun)
	assert.Empty(t, byName["seed"].Definitions)
}

func TestAssembleTasks_HookDefinitionOnly(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
	})
	prev := testHook("Job", "migrate", "migrate", "v1", 1)
	desired := testHook("Job", "migrate", "migrate", "v2", 1)
	prep := upgradePrep(map[string]any{"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1}}, []*v1.Hook{prev}, nil)
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{desired}, nil)
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "migrate")
	assert.Equal(t, semantic.TaskUpdate, byName["migrate"].Action)
	assert.True(t, byName["migrate"].WillRun)
	require.Len(t, byName["migrate"].Definitions, 1)
	assert.Equal(t, semantic.Update, byName["migrate"].Definitions[0].Action)
}

func TestAssembleTasks_HookCreateAndDelete(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"smoke": hookResolved(spec.TaskOnPostDeploy, 2),
	})
	prev := testHook("Job", "old", "old", "v1", 1)
	desired := testHook("Job", "smoke", "smoke", "v1", 2)
	prep := upgradePrep(map[string]any{
		"old": map[string]any{"on": "preDeploy", "hookWeight": 1},
	}, []*v1.Hook{prev}, nil)
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{desired}, nil)
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "smoke")
	require.Contains(t, byName, "old")
	assert.Equal(t, semantic.TaskCreate, byName["smoke"].Action)
	assert.True(t, byName["smoke"].WillRun)
	assert.Equal(t, semantic.TaskDelete, byName["old"].Action)
	assert.False(t, byName["old"].WillRun)
	require.Len(t, byName["old"].Definitions, 1)
	assert.Equal(t, semantic.Delete, byName["old"].Definitions[0].Action)
}

func TestAssembleTasks_AllCurrentHooksRunOnHelmChange(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
		"seed":    hookResolved(spec.TaskOnPreDeploy, 2),
	})
	migrate := testHook("Job", "migrate", "migrate", "same", 1)
	seed := testHook("Job", "seed", "seed", "same", 2)
	prep := upgradePrep(map[string]any{
		"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1},
		"seed":    map[string]any{"on": "preDeploy", "hookWeight": 2},
	}, []*v1.Hook{migrate, seed}, nil)
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{migrate, seed}, []semantic.ResourceChange{labeledCreate("api", "api")})
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	assert.True(t, byName["migrate"].WillRun)
	assert.True(t, byName["seed"].WillRun)
	assert.Equal(t, semantic.TaskUnchanged, byName["migrate"].Action)
	assert.Equal(t, semantic.TaskUnchanged, byName["seed"].Action)
}

func TestAssembleTasks_NewestHooksIgnored(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
	})
	currentHook := testHook("Job", "migrate", "migrate", "current", 1)
	newestHook := testHook("Job", "migrate", "migrate", "newest", 1)
	desired := testHook("Job", "migrate", "migrate", "current", 1)
	newest := &v1.Release{Hooks: []*v1.Hook{newestHook}}
	prep := upgradePrep(map[string]any{"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1}}, []*v1.Hook{currentHook}, newest)
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{desired}, nil)
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	assert.Equal(t, semantic.TaskUnchanged, byName["migrate"].Action)
	assert.Empty(t, byName["migrate"].Definitions)
}

func TestAssembleTasks_MultiResourceBundleOnlyChangedDefinition(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
	})
	prevCM := testHook("ConfigMap", "migrate-env", "migrate", "v8", 0)
	prevJob := testHook("Job", "migrate", "migrate", "same", 1)
	desiredCM := testHook("ConfigMap", "migrate-env", "migrate", "v9", 0)
	desiredJob := testHook("Job", "migrate", "migrate", "same", 1)
	prep := upgradePrep(map[string]any{"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1}}, []*v1.Hook{prevCM, prevJob}, nil)
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{desiredCM, desiredJob}, nil)
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Len(t, byName["migrate"].Definitions, 1)
	assert.Equal(t, "migrate-env", byName["migrate"].Definitions[0].Resource.Name)
	assert.Equal(t, semantic.TaskUpdate, byName["migrate"].Action)
}

func TestAssembleTasks_LeftoverHookWithoutConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		events []v1.HookEvent
		phase  semantic.TaskPhase
	}{
		{
			name:   "preDeploy",
			events: []v1.HookEvent{v1.HookPreInstall, v1.HookPreUpgrade},
			phase:  semantic.TaskPreDeploy,
		},
		{
			name:   "postDeploy",
			events: []v1.HookEvent{v1.HookPostInstall, v1.HookPostUpgrade},
			phase:  semantic.TaskPostDeploy,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hook := testHook("Job", "orphan", "orphan", "v1", 4, tt.events...)
			prep := upgradePrep(map[string]any{}, []*v1.Hook{hook}, nil)
			tasks, err := assembleTasks(resolvedWithTasks(nil), prep, nil, nil)
			require.NoError(t, err)
			byName := taskByName(t, tasks)
			require.Contains(t, byName, "orphan")
			assert.Equal(t, semantic.TaskDelete, byName["orphan"].Action)
			assert.Equal(t, tt.phase, byName["orphan"].Phase)
			assert.False(t, byName["orphan"].WillRun)
			require.Len(t, byName["orphan"].Definitions, 1)
			assert.Equal(t, semantic.Delete, byName["orphan"].Definitions[0].Action)
			assert.Equal(t, 4, byName["orphan"].HookWeight)
		})
	}
}

func TestAssembleTasks_LeftoverHookEventsFromAnnotation(t *testing.T) {
	t.Parallel()
	hook := testHook("Job", "orphan", "orphan", "v1", 1)
	hook.Events = nil
	hook.Manifest = `apiVersion: batch/v1
kind: Job
metadata:
  name: orphan
  namespace: prod
  labels:
    ` + spec.LabelComponent + `: orphan
  annotations:
    helm.sh/hook: post-install,post-upgrade
spec:
  template:
    spec:
      containers:
      - name: job
        image: v1
`
	prep := upgradePrep(map[string]any{}, []*v1.Hook{hook}, nil)
	tasks, err := assembleTasks(resolvedWithTasks(nil), prep, nil, nil)
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "orphan")
	assert.Equal(t, semantic.TaskDelete, byName["orphan"].Action)
	assert.Equal(t, semantic.TaskPostDeploy, byName["orphan"].Phase)
	assert.False(t, byName["orphan"].WillRun)
}

func TestAssembleTasks_FailClosed(t *testing.T) {
	t.Parallel()
	dup := testHook("Job", "migrate", "migrate", "v1", 1)
	tests := []struct {
		name     string
		resolved *spec.ResolvedSpec
		prep     helm.ReleasePrep
		desired  []*v1.Hook
		wantErr  string
	}{
		{
			name:     "unlabeled hook",
			resolved: resolvedWithTasks(nil),
			prep:     helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			desired:  []*v1.Hook{unlabeledHook()},
			wantErr:  spec.LabelComponent,
		},
		{
			name:     "duplicate hook identity",
			resolved: resolvedWithTasks(map[string]spec.ResolvedTask{"migrate": hookResolved(spec.TaskOnPreDeploy, 1)}),
			prep:     helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			desired:  []*v1.Hook{dup, testHook("Job", "migrate", "migrate", "v2", 1)},
			wantErr:  "duplicate hook identity",
		},
		{
			name:     "malformed previous task",
			resolved: resolvedWithTasks(map[string]spec.ResolvedTask{"migrate": hookResolved(spec.TaskOnPreDeploy, 1)}),
			prep:     upgradePrep(map[string]any{"migrate": "not-an-object"}, nil, nil),
			wantErr:  "expected object",
		},
		{
			name:     "leftover hook with unsupported events",
			resolved: resolvedWithTasks(nil),
			prep:     upgradePrep(map[string]any{}, []*v1.Hook{testHook("Job", "orphan", "orphan", "v1", 1, v1.HookTest)}, nil),
			wantErr:  "unsupported hook event",
		},
		{
			name:     "leftover hook with no events",
			resolved: resolvedWithTasks(nil),
			prep:     upgradePrep(map[string]any{}, []*v1.Hook{eventlessHook()}, nil),
			wantErr:  "no helm hook events",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := assembleTasks(tt.resolved, tt.prep, tt.desired, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestAssembleTasks_ScheduleReferencesAndOmitsUnchanged(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"cleanup": {Task: spec.Task{On: spec.TaskOnSchedule}},
		"keep":    {Task: spec.Task{On: spec.TaskOnSchedule}},
	})
	cron := labeledUpdate("cleanup", "cleanup")
	prep := upgradePrep(map[string]any{
		"cleanup": map[string]any{"on": "schedule"},
		"keep":    map[string]any{"on": "schedule"},
	}, nil, nil)
	tasks, err := assembleTasks(resolved, prep, nil, []semantic.ResourceChange{cron, labeledCreate("api", "api")})
	require.NoError(t, err)
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "cleanup")
	assert.NotContains(t, byName, "keep")
	assert.Equal(t, semantic.TaskSchedule, byName["cleanup"].Phase)
	assert.False(t, byName["cleanup"].WillRun)
	assert.Empty(t, byName["cleanup"].Definitions)
	require.Len(t, byName["cleanup"].Resources, 1)
	assert.Equal(t, "cleanup", byName["cleanup"].Resources[0].Name)
	assert.Equal(t, semantic.TaskUpdate, byName["cleanup"].Action)
}

func TestStampHelmApplyOrder_ConfigMapBeforeDeployment(t *testing.T) {
	t.Parallel()
	changes := []semantic.ResourceChange{
		{
			Resource: semantic.ResourceRef{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api"},
			Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
			Action:   semantic.Create,
			After:    snapObj(map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]any{"name": "api"}}),
			Apply:    writeApply(),
		},
		{
			Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "api"},
			Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
			Action:   semantic.Create,
			After:    snapObj(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "api"}}),
			Apply:    writeApply(),
		},
	}
	require.NoError(t, stampHelmApplyOrder(changes))
	p, err := semantic.New(semantic.Header{}, changes, nil, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes, 2)
	assert.Equal(t, "ConfigMap", p.Changes[0].Resource.Kind)
	assert.Equal(t, "Deployment", p.Changes[1].Resource.Kind)
}

func resolvedWithTasks(tasks map[string]spec.ResolvedTask) *spec.ResolvedSpec {
	return &spec.ResolvedSpec{
		Spec:  &spec.Spec{Project: "shop"},
		Env:   spec.EnvIdentity{Original: "prod"},
		Tasks: tasks,
	}
}

func hookResolved(on spec.TaskOn, weight int) spec.ResolvedTask {
	return spec.ResolvedTask{Task: spec.Task{On: on}, HookWeight: weight}
}

func upgradePrep(tasks map[string]any, hooks []*v1.Hook, newest *v1.Release) helm.ReleasePrep {
	return helm.ReleasePrep{
		Operation: helm.OperationUpgrade,
		Current: &v1.Release{
			Config: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{"tasks": tasks},
				},
			},
			Hooks: hooks,
		},
		Newest:       newest,
		NextRevision: 2,
	}
}

func testHook(kind, name, task, data string, weight int, events ...v1.HookEvent) *v1.Hook {
	api := "v1"
	if kind == "Job" {
		api = "batch/v1"
	}
	if len(events) == 0 {
		events = []v1.HookEvent{v1.HookPreInstall, v1.HookPreUpgrade}
	}
	body := fmt.Sprintf("data:\n  key: %s\n", data)
	if kind == "Job" {
		body = fmt.Sprintf("spec:\n  template:\n    spec:\n      containers:\n      - name: job\n        image: %s\n", data)
	}
	return &v1.Hook{
		Name:   name,
		Kind:   kind,
		Weight: weight,
		Events: events,
		Manifest: fmt.Sprintf(`apiVersion: %s
kind: %s
metadata:
  name: %s
  namespace: prod
  labels:
    %s: %s
%s`, api, kind, name, spec.LabelComponent, task, body),
	}
}

func unlabeledHook() *v1.Hook {
	return &v1.Hook{
		Name:   "chart-hook",
		Kind:   "Job",
		Weight: 0,
		Events: []v1.HookEvent{v1.HookPreInstall},
		Manifest: `apiVersion: batch/v1
kind: Job
metadata:
  name: chart-hook
  namespace: prod
  annotations:
    helm.sh/hook: pre-install
spec:
  template:
    spec:
      containers:
      - name: job
        image: busybox
`,
	}
}

func eventlessHook() *v1.Hook {
	h := testHook("Job", "orphan", "orphan", "v1", 1)
	h.Events = nil
	return h
}

func labeledCreate(name, component string) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: name},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
		Action:   semantic.Create,
		After: snapObj(map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "prod",
				"labels":    map[string]any{spec.LabelComponent: component},
			},
			"data": map[string]any{"key": "v1"},
		}),
		Apply: writeApply(),
	}
}

func labeledUpdate(name, component string) semantic.ResourceChange {
	obj := func(sched string) map[string]any {
		return map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "prod",
				"labels":    map[string]any{spec.LabelComponent: component},
			},
			"spec": map[string]any{"schedule": sched},
		}
	}
	return semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: "prod", Name: name},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
		Action:   semantic.Update,
		Before:   snapObj(obj("0 2 * * *")),
		After:    snapObj(obj("0 3 * * *")),
		Apply:    writeApply(),
	}
}

func snapObj(obj map[string]any) *semantic.ResourceSnapshot {
	return &semantic.ResourceSnapshot{Object: obj}
}

func taskByName(t *testing.T, tasks []semantic.TaskPlan) map[string]semantic.TaskPlan {
	t.Helper()
	out := make(map[string]semantic.TaskPlan, len(tasks))
	for _, task := range tasks {
		_, exists := out[task.Name]
		require.False(t, exists, "duplicate task %s", task.Name)
		out[task.Name] = task
	}
	return out
}
