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

package semantic_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestNew_SnapshotInvariants(t *testing.T) {
	t.Parallel()
	header := semantic.Header{Release: "web", Namespace: "prod"}
	tests := []struct {
		name      string
		change    semantic.ResourceChange
		action    semantic.Action
		before    bool
		after     bool
		fields    bool
		fieldPath string
		fieldOp   semantic.FieldOp
	}{
		{
			name:   "create",
			change: createChange("app", "v1"),
			action: semantic.Create,
			after:  true,
		},
		{
			name:      "update",
			change:    updateChange("app", "v1", "v2"),
			action:    semantic.Update,
			before:    true,
			after:     true,
			fields:    true,
			fieldPath: "/data/key",
			fieldOp:   semantic.FieldReplace,
		},
		{
			name:   "delete",
			change: deleteChange("app", "v1"),
			action: semantic.Delete,
			before: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{Header: header, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{tt.change}, Tasks: nil})
			require.NoError(t, err)
			require.Len(t, p.Changes, 1)
			c := p.Changes[0]
			var path string
			var op semantic.FieldOp
			if len(c.Fields) > 0 {
				path = c.Fields[0].Path
				op = c.Fields[0].Op
			}
			assert.Equal(t, tt.action, c.Action)
			assert.Equal(t, tt.before, c.Before != nil)
			assert.Equal(t, tt.after, c.After != nil)
			assert.Equal(t, tt.fields, len(c.Fields) > 0)
			assert.Equal(t, tt.fieldPath, path)
			assert.Equal(t, tt.fieldOp, op)
		})
	}
}

func TestNew_UpdateRequiresAfterAndFields(t *testing.T) {
	t.Parallel()
	header := semantic.Header{Release: "web", Namespace: "prod"}
	res := ref("ConfigMap", "app")
	missingAfter := semantic.ResourceChange{
		Resource: res,
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		Fields:   []semantic.FieldChange{{Path: "/data/key", Op: semantic.FieldReplace, Before: "v1", After: "v2"}},
	}
	_, err := semantic.New(semantic.Input{Header: header, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{missingAfter}, Tasks: nil})
	require.Error(t, err)
	assert.ErrorContains(t, err, "update requires an after snapshot")

	missingFields := semantic.ResourceChange{
		Resource: res,
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
	}
	_, err = semantic.New(semantic.Input{Header: header, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{missingFields}, Tasks: nil})
	require.Error(t, err)
	assert.ErrorContains(t, err, "update requires a field change")
}

func TestNew_TasksAlwaysNonNil(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: nil, Tasks: nil})
	require.NoError(t, err)
	require.NotNil(t, p.Tasks)
	assert.Empty(t, p.Tasks)
	require.NotNil(t, p.Changes)
	assert.Empty(t, p.Changes)
	require.NotNil(t, p.ChartCRDs)
	assert.Empty(t, p.ChartCRDs)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	assert.Equal(t, 0, p.Summary.Total())
}

func TestNew_SummaryDerived(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{
		createChange("a", "1"),
		updateChange("b", "1", "2"),
		deleteChange("c", "1"),
	}, Tasks: nil})
	require.NoError(t, err)
	assert.Equal(t, semantic.Summary{Create: 1, Update: 1, Delete: 1}, p.Summary)
	assert.Equal(t, 3, p.Summary.Total())
}

func TestNew_DeterministicOrder(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{
		deleteChange("z", "1"),
		createChange("a", "1"),
		updateChange("m", "1", "2"),
	}, Tasks: nil})
	require.NoError(t, err)
	require.Len(t, p.Changes, 3)
	assert.Equal(t, "a", p.Changes[0].Resource.Name)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, "m", p.Changes[1].Resource.Name)
	assert.Equal(t, "z", p.Changes[2].Resource.Name)
}

func TestNew_GoIntInSnapshot(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "web", "namespace": "prod"},
		"spec":       map[string]any{"replicas": 1},
	}
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{{
		Resource: ref("Deployment", "web"),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: obj},
	}}, Tasks: nil})
	require.NoError(t, err)
	require.NotNil(t, p.Changes[0].After)
	spec, ok := p.Changes[0].After.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, spec["replicas"])

	callerSpec, ok := obj["spec"].(map[string]any)
	require.True(t, ok)
	callerSpec["replicas"] = 9
	spec, ok = p.Changes[0].After.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, spec["replicas"])
}

func TestNew_DoesNotMutateCallerSnapshots(t *testing.T) {
	t.Parallel()
	obj := cm("app", "v1")
	change := semantic.ResourceChange{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: obj},
	}
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{change}, Tasks: nil})
	require.NoError(t, err)
	setObjectString(t, obj, "mutated", "data", "key")
	assert.Equal(t, "v1", objectString(t, p.Changes[0].After.Object, "data", "key"))
}

func TestNew_KeepsCallerFields(t *testing.T) {
	t.Parallel()
	before := cm("app", "v1")
	after := cm("app", "v2")
	fields := []semantic.FieldChange{
		{Path: "/z", Op: semantic.FieldReplace, Before: "1", After: "9"},
		{Path: "/a", Op: semantic.FieldReplace, Before: "1", After: "2"},
	}
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: before},
		After:    &semantic.ResourceSnapshot{Object: after},
		Fields:   fields,
	}}, Tasks: nil})
	require.NoError(t, err)
	require.Len(t, p.Changes[0].Fields, 2)
	assert.Equal(t, "/a", p.Changes[0].Fields[0].Path)
	assert.Equal(t, "/z", p.Changes[0].Fields[1].Path)
	assert.Equal(t, "9", p.Changes[0].Fields[1].After)

	fields[0].After = "mutated"
	assert.Equal(t, "2", p.Changes[0].Fields[0].After)
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	got := semantic.Summarize([]semantic.ResourceChange{
		{Action: semantic.Create},
		{Action: semantic.Create},
		{Action: semantic.Update},
		{Action: semantic.Delete},
		{Action: semantic.Action(99)},
	})
	assert.Equal(t, semantic.Summary{Create: 2, Update: 1, Delete: 1}, got)
	assert.Equal(t, 4, got.Total())
}

func TestNew_RejectsInvalid(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	fields := []semantic.FieldChange{{Path: "/data/key", Op: semantic.FieldReplace, Before: "v1", After: "v2"}}
	tests := []struct {
		name    string
		change  semantic.ResourceChange
		wantErr string
	}{
		{
			name: "create with before",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Create,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v0")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			},
			wantErr: "create must not have a before snapshot",
		},
		{
			name: "create missing after",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Create,
			},
			wantErr: "create requires an after snapshot",
		},
		{
			name: "create with fields",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Fields:   fields,
			},
			wantErr: "create must not have field changes",
		},
		{
			name: "update missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Update,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Fields:   fields,
			},
			wantErr: "update requires a before snapshot",
		},
		{
			name: "update missing after",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Update,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Fields:   fields,
			},
			wantErr: "update requires an after snapshot",
		},
		{
			name: "update missing fields",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Update,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			},
			wantErr: "update requires a field change",
		},
		{
			name: "delete missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Delete,
			},
			wantErr: "delete requires a before snapshot",
		},
		{
			name: "delete with after",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Delete,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			},
			wantErr: "delete must not have an after snapshot",
		},
		{
			name: "delete with fields",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Delete,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Fields:   fields,
			},
			wantErr: "delete must not have field changes",
		},
		{
			name: "zero action",
			change: semantic.ResourceChange{
				Resource: res,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			},
			wantErr: "invalid action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{tt.change}, Tasks: nil})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.ErrorContains(t, err, res.String())
		})
	}
}

func TestNew_CopiesNestedSnapshotShapes(t *testing.T) {
	t.Parallel()
	labels := map[string]string{"app": "web"}
	args := []string{"serve"}
	nested := []any{map[string]any{"k": "v"}}
	inner := map[string]any{"x": "1"}
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "app", "namespace": "prod", "labels": labels},
		"data":       map[string]any{"args": args, "nested": nested, "inner": inner, "empty": map[string]any(nil)},
	}
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: obj},
	}}, Tasks: nil})
	require.NoError(t, err)
	assert.Empty(t, p.Changes[0].Fields)

	labels["app"] = "mutated"
	args[0] = "mutated"
	nestedMap, ok := nested[0].(map[string]any)
	require.True(t, ok)
	nestedMap["k"] = "mutated"
	inner["x"] = "mutated"

	meta, ok := p.Changes[0].After.Object["metadata"].(map[string]any)
	require.True(t, ok)
	gotLabels, ok := meta["labels"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "web", gotLabels["app"])
	data, ok := p.Changes[0].After.Object["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []string{"serve"}, data["args"])
}

func TestNew_UpdateEmptyBeforeObject(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{},
		After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		Fields:   []semantic.FieldChange{{Path: "/data/key", Op: semantic.FieldReplace, Before: "v1", After: "v2"}},
	}}, Tasks: nil})
	require.NoError(t, err)
	require.Len(t, p.Changes[0].Fields, 1)
	assert.Equal(t, "/data/key", p.Changes[0].Fields[0].Path)
}

func TestNew_NilSnapshotObject(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{},
	}}, Tasks: nil})
	require.NoError(t, err)
	require.NotNil(t, p.Changes[0].After)
	assert.Nil(t, p.Changes[0].After.Object)
}

func TestNew_HelmActionHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
		wantErr    string
	}{
		{name: "fresh install", header: semantic.Header{FreshInstall: true}, helmAction: semantic.HelmInstall},
		{name: "upgrade", helmAction: semantic.HelmUpgrade},
		{name: "none", helmAction: semantic.HelmNone},
		{
			name:       "fresh with none",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmNone,
			wantErr:    "fresh install requires helm install",
		},
		{
			name:       "fresh with upgrade",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmUpgrade,
			wantErr:    "fresh install requires helm install",
		},
		{
			name:       "install without fresh",
			helmAction: semantic.HelmInstall,
			wantErr:    "helm install requires a fresh install",
		},
		{
			name:       "zero helm action",
			helmAction: 0,
			wantErr:    "invalid helm action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{Header: tt.header, HelmAction: tt.helmAction, Changes: nil, Tasks: nil})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.helmAction, p.HelmAction)
		})
	}
}

func TestNew_HelmNoneContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		changes []semantic.ResourceChange
		tasks   []semantic.TaskPlan
		wantErr string
	}{
		{name: "empty"},
		{
			name:    "resource change",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			wantErr: "helm none must not include resource changes",
		},
		{
			name: "changed preDeploy",
			tasks: []semantic.TaskPlan{{
				Name:   "migrate",
				Phase:  semantic.TaskPreDeploy,
				Action: semantic.TaskCreate,
			}},
			wantErr: "helm none must not include changed preDeploy task migrate",
		},
		{
			name: "incoming will run is ignored",
			tasks: []semantic.TaskPlan{{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUnchanged,
				WillRun: true,
			}},
		},
		{
			name: "unchanged idle hook",
			tasks: []semantic.TaskPlan{{
				Name:   "migrate",
				Phase:  semantic.TaskPreDeploy,
				Action: semantic.TaskUnchanged,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmNone, Changes: tt.changes, Tasks: tt.tasks})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.name == "incoming will run is ignored" {
				require.Len(t, p.Tasks, 1)
				assert.False(t, p.Tasks[0].WillRun)
			}
		})
	}
}

func TestPlan_HasEffects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
		changes    []semantic.ResourceChange
		tasks      []semantic.TaskPlan
		crds       []semantic.ChartCRD
		want       bool
	}{
		{name: "helm upgrade alone is not an effect", helmAction: semantic.HelmUpgrade},
		{name: "empty none", helmAction: semantic.HelmNone},
		{
			name:       "resource change",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			changes:    []semantic.ResourceChange{createChange("app", "v1")},
			want:       true,
		},
		{
			name:       "changed task",
			helmAction: semantic.HelmUpgrade,
			tasks: []semantic.TaskPlan{{
				Name:   "migrate",
				Phase:  semantic.TaskPreDeploy,
				Action: semantic.TaskCreate,
			}},
			want: true,
		},
		{
			name:       "will run unchanged hook",
			helmAction: semantic.HelmUpgrade,
			tasks: []semantic.TaskPlan{{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUnchanged,
				WillRun: true,
			}},
			want: true,
		},
		{
			name:       "idle unchanged hook",
			helmAction: semantic.HelmNone,
			tasks: []semantic.TaskPlan{{
				Name:   "migrate",
				Phase:  semantic.TaskPreDeploy,
				Action: semantic.TaskUnchanged,
			}},
		},
		{
			name:       "chart crd helm will process",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			crds: []semantic.ChartCRD{{
				Kind:        "CustomResourceDefinition",
				Name:        "widgets.example.com",
				Lifecycle:   semantic.ChartCRDProcess,
				WillProcess: true,
			}},
			want: true,
		},
		{
			name:       "chart crd skip",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			crds: []semantic.ChartCRD{{
				Kind:      "CustomResourceDefinition",
				Name:      "widgets.example.com",
				Lifecycle: semantic.ChartCRDSkip,
			}},
		},
		{
			name:       "chart crd upgrade",
			helmAction: semantic.HelmNone,
			crds: []semantic.ChartCRD{{
				Kind:      "CustomResourceDefinition",
				Name:      "widgets.example.com",
				Lifecycle: semantic.ChartCRDUpgrade,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{
				Header:     tt.header,
				HelmAction: tt.helmAction,
				Changes:    tt.changes,
				Tasks:      tt.tasks,
				ChartCRDs:  tt.crds,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.HasEffects())
		})
	}
}

func TestPlan_IsNoOp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
		changes    []semantic.ResourceChange
		tasks      []semantic.TaskPlan
		want       bool
	}{
		{name: "helm none", helmAction: semantic.HelmNone, want: true},
		{name: "helm upgrade zero effects", helmAction: semantic.HelmUpgrade},
		{
			name:       "fresh install",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
		},
		{
			name:       "install with a resource change",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			changes:    []semantic.ResourceChange{createChange("app", "v1")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{Header: tt.header, HelmAction: tt.helmAction, Changes: tt.changes, Tasks: tt.tasks})
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.IsNoOp())
		})
	}
}

func TestNew_DerivesWillRun(t *testing.T) {
	t.Parallel()
	schedule := semantic.TaskPlan{Name: "cleanup", Phase: semantic.TaskSchedule, Action: semantic.TaskUnchanged, WillRun: true}
	deleteHook := semantic.TaskPlan{Name: "old", Phase: semantic.TaskPreDeploy, Action: semantic.TaskDelete, WillRun: true}
	idle := semantic.TaskPlan{Name: "migrate", Phase: semantic.TaskPreDeploy, Action: semantic.TaskUnchanged, WillRun: true}
	tests := []struct {
		name   string
		header semantic.Header
		action semantic.HelmAction
		task   semantic.TaskPlan
		want   bool
	}{
		{name: "schedule on install", header: semantic.Header{FreshInstall: true}, action: semantic.HelmInstall, task: schedule},
		{name: "delete on upgrade", action: semantic.HelmUpgrade, task: deleteHook},
		{name: "install hook", header: semantic.Header{FreshInstall: true}, action: semantic.HelmInstall, task: idle, want: true},
		{name: "upgrade hook", action: semantic.HelmUpgrade, task: idle, want: true},
		{name: "helm none hook", action: semantic.HelmNone, task: idle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{
				Header:     tt.header,
				HelmAction: tt.action,
				Tasks:      []semantic.TaskPlan{tt.task},
			})
			require.NoError(t, err)
			require.Len(t, p.Tasks, 1)
			assert.Equal(t, tt.want, p.Tasks[0].WillRun)
		})
	}
}

func TestNew_ChartCRDAndDriftIgnoreSummary(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{
		Header:     semantic.Header{},
		HelmAction: semantic.HelmUpgrade,
		Changes:    []semantic.ResourceChange{createChange("app", "v1")},
		Drift:      []semantic.DriftChange{missingDrift("other")},
		ChartCRDs: []semantic.ChartCRD{{
			Kind:        "CustomResourceDefinition",
			Name:        "widgets.example.com",
			Lifecycle:   semantic.ChartCRDProcess,
			WillProcess: true,
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, semantic.Summary{Create: 1}, p.Summary)
	assert.True(t, p.HasEffects())
	assert.True(t, p.HasDrift())
	assert.False(t, p.IsNoOp())

	without, err := semantic.New(semantic.Input{
		Header:     semantic.Header{},
		HelmAction: semantic.HelmUpgrade,
		Drift:      []semantic.DriftChange{missingDrift("other")},
	})
	require.NoError(t, err)
	assert.Equal(t, semantic.Summary{}, without.Summary)
	assert.False(t, without.HasEffects())
	assert.True(t, without.HasDrift())
}
