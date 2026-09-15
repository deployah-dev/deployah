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

package semantic_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestNew_TaskValidation(t *testing.T) {
	t.Parallel()
	app := ref("ConfigMap", "app")
	web := ref("ConfigMap", "web")
	tests := []struct {
		name    string
		changes []semantic.ResourceChange
		tasks   []semantic.TaskPlan
		wantErr string
	}{
		{
			name:    "dangling schedule resource",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			tasks: []semantic.TaskPlan{{
				Name:      "cleanup",
				Phase:     semantic.TaskSchedule,
				Action:    semantic.TaskUpdate,
				Resources: []semantic.ResourceRef{web},
			}},
			wantErr: "dangling resource",
		},
		{
			name:    "duplicate resource ownership",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			tasks: []semantic.TaskPlan{
				{
					Name:      "cleanup",
					Phase:     semantic.TaskSchedule,
					Action:    semantic.TaskUpdate,
					Resources: []semantic.ResourceRef{app},
				},
				{
					Name:      "other",
					Phase:     semantic.TaskSchedule,
					Action:    semantic.TaskUpdate,
					Resources: []semantic.ResourceRef{app},
				},
			},
			wantErr: "already referenced",
		},
		{
			name:    "preDeploy with resources",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			tasks: []semantic.TaskPlan{{
				Name:      "migrate",
				Phase:     semantic.TaskPreDeploy,
				Action:    semantic.TaskUnchanged,
				WillRun:   true,
				Resources: []semantic.ResourceRef{app},
			}},
			wantErr: "must not reference resource changes",
		},
		{
			name:    "postDeploy with resources",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			tasks: []semantic.TaskPlan{{
				Name:      "smoke",
				Phase:     semantic.TaskPostDeploy,
				Action:    semantic.TaskCreate,
				WillRun:   true,
				Resources: []semantic.ResourceRef{app},
			}},
			wantErr: "must not reference resource changes",
		},
		{
			name: "schedule with hook definitions",
			tasks: []semantic.TaskPlan{{
				Name:   "cleanup",
				Phase:  semantic.TaskSchedule,
				Action: semantic.TaskUnchanged,
				Definitions: []semantic.HookDefinition{{
					Resource: app,
					Action:   semantic.Create,
					After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				}},
			}},
			wantErr: "schedule must not have hook definitions",
		},
		{
			name: "schedule with WillRun",
			tasks: []semantic.TaskPlan{{
				Name:    "cleanup",
				Phase:   semantic.TaskSchedule,
				Action:  semantic.TaskUnchanged,
				WillRun: true,
			}},
			wantErr: "schedule must not will run",
		},
		{
			name: "delete with WillRun",
			tasks: []semantic.TaskPlan{{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskDelete,
				WillRun: true,
			}},
			wantErr: "delete must not will run",
		},
		{
			name: "duplicate task name",
			tasks: []semantic.TaskPlan{
				{Name: "migrate", Phase: semantic.TaskPreDeploy, Action: semantic.TaskUnchanged, WillRun: true},
				{Name: "migrate", Phase: semantic.TaskPostDeploy, Action: semantic.TaskUnchanged, WillRun: true},
			},
			wantErr: "duplicate task name",
		},
		{
			name: "missing task name",
			tasks: []semantic.TaskPlan{{
				Phase:  semantic.TaskPreDeploy,
				Action: semantic.TaskUnchanged,
			}},
			wantErr: "name is required",
		},
		{
			name: "invalid phase",
			tasks: []semantic.TaskPlan{{
				Name:   "migrate",
				Action: semantic.TaskUnchanged,
			}},
			wantErr: "invalid phase",
		},
		{
			name: "invalid task action",
			tasks: []semantic.TaskPlan{{
				Name:  "migrate",
				Phase: semantic.TaskPreDeploy,
			}},
			wantErr: "invalid action",
		},
		{
			name: "hook replace action",
			tasks: []semantic.TaskPlan{{
				Name:   "migrate",
				Phase:  semantic.TaskPreDeploy,
				Action: semantic.TaskUpdate,
				Definitions: []semantic.HookDefinition{{
					Resource: app,
					Action:   semantic.Replace,
					Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
					After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				}},
			}},
			wantErr: "invalid action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, tt.changes, tt.tasks, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNew_ValidScheduleReference(t *testing.T) {
	t.Parallel()
	app := ref("ConfigMap", "app")
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{createChange("app", "v1")}, []semantic.TaskPlan{{
		Name:      "cleanup",
		Phase:     semantic.TaskSchedule,
		Action:    semantic.TaskCreate,
		Resources: []semantic.ResourceRef{app},
	}}, nil)
	require.NoError(t, err)
	require.Len(t, p.Tasks, 1)
	require.Len(t, p.Tasks[0].Resources, 1)
	assert.Equal(t, app, p.Tasks[0].Resources[0])
	assert.Equal(t, 1, p.Summary.Create)
	assert.Equal(t, 1, p.Summary.Total())
}

func TestNew_SummaryIgnoresHookDefinitions(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
		Name:    "migrate",
		Phase:   semantic.TaskPreDeploy,
		Action:  semantic.TaskCreate,
		WillRun: true,
		Definitions: []semantic.HookDefinition{{
			Resource: ref("Job", "migrate"),
			Action:   semantic.Create,
			After:    &semantic.ResourceSnapshot{Object: cm("migrate", "v1")},
		}},
	}}, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, p.Summary.Total())
	require.Len(t, p.Tasks, 1)
	require.Len(t, p.Tasks[0].Definitions, 1)
	assert.Empty(t, p.Tasks[0].Definitions[0].Fields)
}

func TestNew_TaskSort(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, []semantic.TaskPlan{
		{Name: "z", Phase: semantic.TaskPreDeploy, Action: semantic.TaskUnchanged, WillRun: true, HookWeight: 2},
		{Name: "cleanup", Phase: semantic.TaskSchedule, Action: semantic.TaskUnchanged},
		{Name: "a", Phase: semantic.TaskPreDeploy, Action: semantic.TaskUnchanged, WillRun: true, HookWeight: 2},
		{Name: "smoke", Phase: semantic.TaskPostDeploy, Action: semantic.TaskCreate, WillRun: true},
		{Name: "migrate", Phase: semantic.TaskPreDeploy, Action: semantic.TaskUnchanged, WillRun: true, HookWeight: 1},
	}, nil)
	require.NoError(t, err)
	require.Len(t, p.Tasks, 5)
	assert.Equal(t, []string{"migrate", "a", "z", "smoke", "cleanup"}, []string{
		p.Tasks[0].Name, p.Tasks[1].Name, p.Tasks[2].Name, p.Tasks[3].Name, p.Tasks[4].Name,
	})
}

func TestNew_NestedTaskOrder(t *testing.T) {
	t.Parallel()
	z := createChange("z", "1")
	z.ApplyOrder = 1
	a := createChange("a", "1")
	a.ApplyOrder = 2
	tests := []struct {
		name          string
		changes       []semantic.ResourceChange
		task          semantic.TaskPlan
		wantDefs      []string
		wantResources []string
	}{
		{
			name: "definitions by hook weight",
			task: semantic.TaskPlan{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUpdate,
				WillRun: true,
				Definitions: []semantic.HookDefinition{
					{
						Resource:   ref("ConfigMap", "z-env"),
						Action:     semantic.Create,
						After:      &semantic.ResourceSnapshot{Object: cm("z-env", "v1")},
						HookWeight: 2,
					},
					{
						Resource:   ref("ConfigMap", "a-env"),
						Action:     semantic.Create,
						After:      &semantic.ResourceSnapshot{Object: cm("a-env", "v1")},
						HookWeight: 1,
					},
				},
			},
			wantDefs:      []string{"a-env", "z-env"},
			wantResources: []string{},
		},
		{
			name:    "schedule resources by apply order",
			changes: []semantic.ResourceChange{a, z},
			task: semantic.TaskPlan{
				Name:      "cleanup",
				Phase:     semantic.TaskSchedule,
				Action:    semantic.TaskUpdate,
				Resources: []semantic.ResourceRef{a.Resource, z.Resource},
			},
			wantDefs:      []string{},
			wantResources: []string{"z", "a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, tt.changes, []semantic.TaskPlan{tt.task}, nil)
			require.NoError(t, err)
			require.Len(t, p.Tasks, 1)
			gotDefs := make([]string, 0, len(p.Tasks[0].Definitions))
			for _, d := range p.Tasks[0].Definitions {
				gotDefs = append(gotDefs, d.Resource.Name)
			}
			gotResources := make([]string, 0, len(p.Tasks[0].Resources))
			for _, r := range p.Tasks[0].Resources {
				gotResources = append(gotResources, r.Name)
			}
			assert.Equal(t, tt.wantDefs, gotDefs)
			assert.Equal(t, tt.wantResources, gotResources)
		})
	}
}

func TestNew_ApplyOrderThenIdentity(t *testing.T) {
	t.Parallel()
	a := createChange("a", "1")
	a.ApplyOrder = 2
	z := createChange("z", "1")
	z.ApplyOrder = 1
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{a, z}, nil, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes, 2)
	assert.Equal(t, "z", p.Changes[0].Resource.Name)
	assert.Equal(t, "a", p.Changes[1].Resource.Name)
}

func TestNew_NonNilNestedTaskSlices(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
		Name:    "seed",
		Phase:   semantic.TaskPreDeploy,
		Action:  semantic.TaskUnchanged,
		WillRun: true,
	}}, nil)
	require.NoError(t, err)
	require.NotNil(t, p.Tasks[0].Definitions)
	assert.Empty(t, p.Tasks[0].Definitions)
	require.NotNil(t, p.Tasks[0].Resources)
	assert.Empty(t, p.Tasks[0].Resources)
}

func TestNew_DoesNotAliasCallerTasks(t *testing.T) {
	t.Parallel()
	defs := []semantic.HookDefinition{{
		Resource: ref("ConfigMap", "env"),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: cm("env", "v1")},
		After:    &semantic.ResourceSnapshot{Object: cm("env", "v2")},
	}}
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
		Name:        "migrate",
		Phase:       semantic.TaskPreDeploy,
		Action:      semantic.TaskUpdate,
		WillRun:     true,
		Definitions: defs,
	}}, nil)
	require.NoError(t, err)
	defs[0].Resource.Name = "mutated"
	assert.Equal(t, "env", p.Tasks[0].Definitions[0].Resource.Name)
	require.NotEmpty(t, p.Tasks[0].Definitions[0].Fields)
}
