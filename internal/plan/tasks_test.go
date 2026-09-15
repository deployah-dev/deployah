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
	prep := helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1}
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{
		testHook("ConfigMap", "migrate-env", "migrate", "v1", 0),
		testHook("Job", "migrate", "migrate", "v1", 1),
		testHook("Job", "smoke", "smoke", "v1", 2),
	}, nil)
	require.NoError(t, err)
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, []semantic.ResourceChange{labeledCreate("api", "api")}, tasks))
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
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
	changes := []semantic.ResourceChange{labeledCreate("api", "api")}
	tasks, err := assembleTasks(resolved, prep, []*v1.Hook{migrate, seed}, changes)
	require.NoError(t, err)
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, changes, tasks))
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
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
			applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
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
			wantErr:  spec.LabelTask,
		},
		{
			name:     "desired hook for unknown task",
			resolved: resolvedWithTasks(map[string]spec.ResolvedTask{"migrate": hookResolved(spec.TaskOnPreDeploy, 1)}),
			prep:     helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			desired:  []*v1.Hook{testHook("Job", "ghost", "ghost", "v1", 1)},
			wantErr:  spec.LabelTask,
		},
		{
			name:     "desired hook for schedule task",
			resolved: resolvedWithTasks(map[string]spec.ResolvedTask{"cleanup": {Task: spec.Task{On: spec.TaskOnSchedule}}}),
			prep:     helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			desired:  []*v1.Hook{testHook("Job", "cleanup", "cleanup", "v1", 1)},
			wantErr:  spec.LabelTask,
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

func TestAssembleTasks_HookChangeRunsAllCurrentHooks(t *testing.T) {
	t.Parallel()
	type want struct {
		Action  semantic.TaskAction
		Phase   semantic.TaskPhase
		WillRun bool
	}
	seed := testHook("Job", "seed", "seed", "same", 1)
	migrateV1 := testHook("Job", "migrate", "migrate", "v1", 1)
	migrateV2 := testHook("Job", "migrate", "migrate", "v2", 1)
	smoke := testHook("Job", "smoke", "smoke", "v1", 2, v1.HookPostInstall, v1.HookPostUpgrade)
	old := testHook("Job", "old", "old", "v1", 2)
	tests := []struct {
		name     string
		resolved map[string]spec.ResolvedTask
		previous map[string]any
		prev     []*v1.Hook
		desired  []*v1.Hook
		want     map[string]want
	}{
		{
			name: "definition change runs unchanged sibling",
			resolved: map[string]spec.ResolvedTask{
				"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
				"seed":    hookResolved(spec.TaskOnPreDeploy, 2),
			},
			previous: map[string]any{
				"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1},
				"seed":    map[string]any{"on": "preDeploy", "hookWeight": 2},
			},
			prev:    []*v1.Hook{migrateV1, seed},
			desired: []*v1.Hook{migrateV2, seed},
			want: map[string]want{
				"migrate": {Action: semantic.TaskUpdate, Phase: semantic.TaskPreDeploy, WillRun: true},
				"seed":    {Action: semantic.TaskUnchanged, Phase: semantic.TaskPreDeploy, WillRun: true},
			},
		},
		{
			name: "new hook runs unchanged sibling",
			resolved: map[string]spec.ResolvedTask{
				"seed":  hookResolved(spec.TaskOnPreDeploy, 1),
				"smoke": hookResolved(spec.TaskOnPostDeploy, 2),
			},
			previous: map[string]any{
				"seed": map[string]any{"on": "preDeploy", "hookWeight": 1},
			},
			prev:    []*v1.Hook{seed},
			desired: []*v1.Hook{seed, smoke},
			want: map[string]want{
				"seed":  {Action: semantic.TaskUnchanged, Phase: semantic.TaskPreDeploy, WillRun: true},
				"smoke": {Action: semantic.TaskCreate, Phase: semantic.TaskPostDeploy, WillRun: true},
			},
		},
		{
			name: "removed hook runs remaining sibling",
			resolved: map[string]spec.ResolvedTask{
				"seed": hookResolved(spec.TaskOnPreDeploy, 1),
			},
			previous: map[string]any{
				"old":  map[string]any{"on": "preDeploy", "hookWeight": 2},
				"seed": map[string]any{"on": "preDeploy", "hookWeight": 1},
			},
			prev:    []*v1.Hook{old, seed},
			desired: []*v1.Hook{seed},
			want: map[string]want{
				"old":  {Action: semantic.TaskDelete, Phase: semantic.TaskPreDeploy, WillRun: false},
				"seed": {Action: semantic.TaskUnchanged, Phase: semantic.TaskPreDeploy, WillRun: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prep := upgradePrep(tt.previous, tt.prev, nil)
			tasks, err := assembleTasks(resolvedWithTasks(tt.resolved), prep, tt.desired, nil)
			require.NoError(t, err)
			applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
			byName := taskByName(t, tasks)
			require.Len(t, byName, len(tt.want))
			for name, want := range tt.want {
				got, ok := byName[name]
				require.True(t, ok, "missing task %s", name)
				assert.Equal(t, want.Action, got.Action, name)
				assert.Equal(t, want.Phase, got.Phase, name)
				assert.Equal(t, want.WillRun, got.WillRun, name)
			}
		})
	}
}

func TestAssembleTasks_RemovedScheduleDoesNotCaptureComponent(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(nil)
	cron := labeledLegacyDelete("cleanup", "cleanup")
	comp := labeledCreate("cleanup", "cleanup")
	prep := upgradePrep(map[string]any{
		"cleanup": map[string]any{"on": "schedule"},
	}, nil, nil)
	changes := []semantic.ResourceChange{cron, comp}
	tasks, err := assembleTasks(resolved, prep, nil, changes)
	require.NoError(t, err)
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, changes, tasks))
	byName := taskByName(t, tasks)
	require.Contains(t, byName, "cleanup")
	assert.Equal(t, semantic.TaskDelete, byName["cleanup"].Action)
	require.Len(t, byName["cleanup"].Resources, 1)
	assert.Equal(t, "cleanup", byName["cleanup"].Resources[0].Name)
	assert.Equal(t, "CronJob", byName["cleanup"].Resources[0].Kind)
}

func TestAssembleTasks_TransitionToManual(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		task           string
		previous       map[string]any
		prevHooks      []*v1.Hook
		changes        []semantic.ResourceChange
		wantPhase      semantic.TaskPhase
		wantDefActions []semantic.Action
		wantResources  []string
	}{
		{
			name: "preDeploy",
			task: "migrate",
			previous: map[string]any{
				"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1},
			},
			prevHooks:      []*v1.Hook{testHook("Job", "migrate", "migrate", "v1", 1)},
			wantPhase:      semantic.TaskPreDeploy,
			wantDefActions: []semantic.Action{semantic.Delete},
			wantResources:  []string{},
		},
		{
			name: "postDeploy",
			task: "smoke",
			previous: map[string]any{
				"smoke": map[string]any{"on": "postDeploy", "hookWeight": 2},
			},
			prevHooks:      []*v1.Hook{testHook("Job", "smoke", "smoke", "v1", 2, v1.HookPostInstall, v1.HookPostUpgrade)},
			wantPhase:      semantic.TaskPostDeploy,
			wantDefActions: []semantic.Action{semantic.Delete},
			wantResources:  []string{},
		},
		{
			name: "schedule",
			task: "cleanup",
			previous: map[string]any{
				"cleanup": map[string]any{"on": "schedule"},
			},
			changes:        []semantic.ResourceChange{labeledLegacyDelete("cleanup", "cleanup")},
			wantPhase:      semantic.TaskSchedule,
			wantDefActions: []semantic.Action{},
			wantResources:  []string{"cleanup"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
				tt.task: {Task: spec.Task{On: spec.TaskOnManual}},
			})
			prep := upgradePrep(tt.previous, tt.prevHooks, nil)
			tasks, err := assembleTasks(resolved, prep, nil, tt.changes)
			require.NoError(t, err)
			applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, tt.changes, tasks))
			byName := taskByName(t, tasks)
			require.Contains(t, byName, tt.task)
			got := byName[tt.task]
			assert.Equal(t, semantic.TaskDelete, got.Action)
			assert.False(t, got.WillRun)
			assert.Equal(t, tt.wantPhase, got.Phase)
			assert.Equal(t, tt.wantDefActions, definitionActions(got.Definitions))
			assert.Equal(t, tt.wantResources, resourceNames(got.Resources))
		})
	}
}

func TestAssembleTasks_HookPhaseChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		current   spec.TaskOn
		previous  string
		prev      *v1.Hook
		desired   *v1.Hook
		wantPhase semantic.TaskPhase
	}{
		{
			name:      "preDeploy to postDeploy",
			current:   spec.TaskOnPostDeploy,
			previous:  "preDeploy",
			prev:      testHook("Job", "migrate", "migrate", "v1", 1),
			desired:   testHook("Job", "migrate", "migrate", "v2", 1, v1.HookPostInstall, v1.HookPostUpgrade),
			wantPhase: semantic.TaskPostDeploy,
		},
		{
			name:      "postDeploy to preDeploy",
			current:   spec.TaskOnPreDeploy,
			previous:  "postDeploy",
			prev:      testHook("Job", "migrate", "migrate", "v1", 1, v1.HookPostInstall, v1.HookPostUpgrade),
			desired:   testHook("Job", "migrate", "migrate", "v2", 1),
			wantPhase: semantic.TaskPreDeploy,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
				"migrate": hookResolved(tt.current, 1),
			})
			prep := upgradePrep(map[string]any{
				"migrate": map[string]any{"on": tt.previous, "hookWeight": 1},
			}, []*v1.Hook{tt.prev}, nil)
			tasks, err := assembleTasks(resolved, prep, []*v1.Hook{tt.desired}, nil)
			require.NoError(t, err)
			applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, nil, tasks))
			got := taskByName(t, tasks)["migrate"]
			assert.Equal(t, tt.wantPhase, got.Phase)
			assert.Equal(t, semantic.TaskUpdate, got.Action)
			assert.True(t, got.WillRun)
		})
	}
}

func TestAssembleTasks_HookScheduleTransition(t *testing.T) {
	t.Parallel()
	pre := testHook("Job", "work", "work", "v1", 1)
	post := testHook("Job", "work", "work", "v1", 1, v1.HookPostInstall, v1.HookPostUpgrade)
	scheduled := map[string]any{"work": map[string]any{"on": "schedule", "hookWeight": 1}}
	preCfg := map[string]any{"work": map[string]any{"on": "preDeploy", "hookWeight": 1}}
	postCfg := map[string]any{"work": map[string]any{"on": "postDeploy", "hookWeight": 1}}
	cronDelete := labeledTaskDelete("work")
	cronCreate := labeledTaskCreate("work")
	tests := []struct {
		name            string
		current         spec.TaskOn
		previous        map[string]any
		prevHooks       []*v1.Hook
		desired         []*v1.Hook
		changes         []semantic.ResourceChange
		wantPhase       semantic.TaskPhase
		wantAction      semantic.TaskAction
		wantWillRun     bool
		wantDefActions  []semantic.Action
		wantOwned       []string
		wantChangeNames []string
	}{
		{
			name:            "schedule to preDeploy",
			current:         spec.TaskOnPreDeploy,
			previous:        scheduled,
			desired:         []*v1.Hook{pre},
			changes:         []semantic.ResourceChange{cronDelete},
			wantPhase:       semantic.TaskPreDeploy,
			wantAction:      semantic.TaskUpdate,
			wantWillRun:     true,
			wantDefActions:  []semantic.Action{semantic.Create},
			wantOwned:       []string{},
			wantChangeNames: []string{"work"},
		},
		{
			name:            "schedule to postDeploy",
			current:         spec.TaskOnPostDeploy,
			previous:        scheduled,
			desired:         []*v1.Hook{post},
			changes:         []semantic.ResourceChange{cronDelete},
			wantPhase:       semantic.TaskPostDeploy,
			wantAction:      semantic.TaskUpdate,
			wantWillRun:     true,
			wantDefActions:  []semantic.Action{semantic.Create},
			wantOwned:       []string{},
			wantChangeNames: []string{"work"},
		},
		{
			name:            "preDeploy to schedule",
			current:         spec.TaskOnSchedule,
			previous:        preCfg,
			prevHooks:       []*v1.Hook{pre},
			changes:         []semantic.ResourceChange{cronCreate},
			wantPhase:       semantic.TaskSchedule,
			wantAction:      semantic.TaskUpdate,
			wantWillRun:     false,
			wantDefActions:  []semantic.Action{},
			wantOwned:       []string{"work"},
			wantChangeNames: []string{"work"},
		},
		{
			name:            "postDeploy to schedule",
			current:         spec.TaskOnSchedule,
			previous:        postCfg,
			prevHooks:       []*v1.Hook{post},
			changes:         []semantic.ResourceChange{cronCreate},
			wantPhase:       semantic.TaskSchedule,
			wantAction:      semantic.TaskUpdate,
			wantWillRun:     false,
			wantDefActions:  []semantic.Action{},
			wantOwned:       []string{"work"},
			wantChangeNames: []string{"work"},
		},
		{
			name:            "leftover preDeploy hooks to schedule",
			current:         spec.TaskOnSchedule,
			previous:        map[string]any{},
			prevHooks:       []*v1.Hook{pre},
			changes:         []semantic.ResourceChange{cronCreate},
			wantPhase:       semantic.TaskSchedule,
			wantAction:      semantic.TaskCreate,
			wantWillRun:     false,
			wantDefActions:  []semantic.Action{},
			wantOwned:       []string{"work"},
			wantChangeNames: []string{"work"},
		},
		{
			name:            "leftover postDeploy hooks to schedule",
			current:         spec.TaskOnSchedule,
			previous:        map[string]any{},
			prevHooks:       []*v1.Hook{post},
			changes:         []semantic.ResourceChange{cronCreate},
			wantPhase:       semantic.TaskSchedule,
			wantAction:      semantic.TaskCreate,
			wantWillRun:     false,
			wantDefActions:  []semantic.Action{},
			wantOwned:       []string{"work"},
			wantChangeNames: []string{"work"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
				"work": {Task: spec.Task{On: tt.current}, HookWeight: 1},
			})
			prep := upgradePrep(tt.previous, tt.prevHooks, nil)
			tasks, err := assembleTasks(resolved, prep, tt.desired, tt.changes)
			require.NoError(t, err)
			applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, tt.changes, tasks))
			p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, tt.changes, tasks, nil)
			require.NoError(t, err)
			byName := taskByName(t, p.Tasks)
			require.Contains(t, byName, "work")
			got := byName["work"]
			assert.Equal(t, tt.wantPhase, got.Phase)
			assert.Equal(t, tt.wantAction, got.Action)
			assert.Equal(t, tt.wantWillRun, got.WillRun)
			assert.Equal(t, tt.wantDefActions, definitionActions(got.Definitions))
			assert.Equal(t, tt.wantOwned, resourceNames(got.Resources))
			assert.Equal(t, tt.wantChangeNames, resourceNames(changeResources(p.Changes)))
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
	changes := []semantic.ResourceChange{cron, labeledCreate("api", "api")}
	tasks, err := assembleTasks(resolved, prep, nil, changes)
	require.NoError(t, err)
	applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, changes, tasks))
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

func TestAssembleTasks_ScheduleProvenance(t *testing.T) {
	t.Parallel()
	cleanup := map[string]spec.ResolvedTask{
		"cleanup": {Task: spec.Task{On: spec.TaskOnSchedule}},
	}
	tests := []struct {
		name          string
		resolved      map[string]spec.ResolvedTask
		previous      map[string]any
		changes       []semantic.ResourceChange
		wantAction    semantic.TaskAction
		wantResources []string
	}{
		{
			name:          "create uses After LabelTask only",
			resolved:      cleanup,
			previous:      map[string]any{},
			changes:       []semantic.ResourceChange{labeledTaskCreate("cleanup")},
			wantAction:    semantic.TaskCreate,
			wantResources: []string{"cleanup"},
		},
		{
			name:          "update uses After LabelTask",
			resolved:      cleanup,
			previous:      map[string]any{"cleanup": map[string]any{"on": "schedule"}},
			changes:       []semantic.ResourceChange{labeledUpdate("cleanup", "cleanup")},
			wantAction:    semantic.TaskUpdate,
			wantResources: []string{"cleanup"},
		},
		{
			name:          "legacy delete uses Before LabelComponent",
			resolved:      map[string]spec.ResolvedTask{},
			previous:      map[string]any{"cleanup": map[string]any{"on": "schedule"}},
			changes:       []semantic.ResourceChange{labeledLegacyDelete("cleanup", "cleanup")},
			wantAction:    semantic.TaskDelete,
			wantResources: []string{"cleanup"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prep := upgradePrep(tt.previous, nil, nil)
			tasks, err := assembleTasks(resolvedWithTasks(tt.resolved), prep, nil, tt.changes)
			require.NoError(t, err)
			applyHelmWillRun(tasks, deriveHelmAction(prep.Operation, tt.changes, tasks))
			byName := taskByName(t, tasks)
			require.Contains(t, byName, "cleanup")
			got := byName["cleanup"]
			assert.Equal(t, tt.wantAction, got.Action)
			assert.Equal(t, tt.wantResources, resourceNames(got.Resources))
		})
	}
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
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, changes, nil, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes, 2)
	assert.Equal(t, "ConfigMap", p.Changes[0].Resource.Kind)
	assert.Equal(t, "Deployment", p.Changes[1].Resource.Kind)
}

func TestStampHelmApplyOrder_SameKindPreservesInputOrder(t *testing.T) {
	t.Parallel()
	const n = 12
	changes := make([]semantic.ResourceChange, 0, n)
	for i := range n {
		name := fmt.Sprintf("n%d", n-1-i)
		changes = append(changes, semantic.ResourceChange{
			Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: name},
			Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
			Action:   semantic.Create,
			After:    snapObj(map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": name}}),
			Apply:    writeApply(),
		})
	}
	require.NoError(t, stampHelmApplyOrder(changes))
	for i, c := range changes {
		assert.Equal(t, i+1, c.ApplyOrder, "input index %d", i)
	}
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, changes, nil, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes, n)
	for i, c := range p.Changes {
		assert.Equal(t, fmt.Sprintf("n%d", n-1-i), c.Resource.Name)
	}
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
    %s: %s
%s`, api, kind, name, spec.LabelComponent, task, spec.LabelTask, task, body),
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

func labeledTaskCreate(name string) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: "prod", Name: name},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
		Action:   semantic.Create,
		After: snapObj(map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "prod",
				"labels":    map[string]any{spec.LabelTask: name},
			},
			"spec": map[string]any{"schedule": "0 3 * * *"},
		}),
		Apply: writeApply(),
	}
}

func labeledTaskDelete(name string) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: "prod", Name: name},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
		Action:   semantic.Delete,
		Before: snapObj(map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "prod",
				"labels":    map[string]any{spec.LabelTask: name},
			},
			"spec": map[string]any{"schedule": "0 3 * * *"},
		}),
		Apply: deleteApply(),
	}
}

func labeledUpdate(name, component string) semantic.ResourceChange {
	obj := func(sched string, labels map[string]any) map[string]any {
		return map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "prod",
				"labels":    labels,
			},
			"spec": map[string]any{"schedule": sched},
		}
	}
	return semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: "prod", Name: name},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
		Action:   semantic.Update,
		Before:   snapObj(obj("0 2 * * *", map[string]any{spec.LabelComponent: component})),
		After: snapObj(obj("0 3 * * *", map[string]any{
			spec.LabelComponent: component,
			spec.LabelTask:      name,
		})),
		Apply: writeApply(),
	}
}

func labeledLegacyDelete(name, component string) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: "prod", Name: name},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: &semantic.HelmOrigin{Release: "web", Namespace: "prod"}},
		Action:   semantic.Delete,
		Before: snapObj(map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "CronJob",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "prod",
				"labels":    map[string]any{spec.LabelComponent: component},
			},
			"spec": map[string]any{"schedule": "0 3 * * *"},
		}),
		Apply: deleteApply(),
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

func definitionActions(defs []semantic.HookDefinition) []semantic.Action {
	out := make([]semantic.Action, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Action)
	}
	return out
}

func resourceNames(refs []semantic.ResourceRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Name)
	}
	return out
}

func changeResources(changes []semantic.ResourceChange) []semantic.ResourceRef {
	out := make([]semantic.ResourceRef, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.Resource)
	}
	return out
}
