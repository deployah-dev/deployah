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
	"math"
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
		write     bool
		delete    bool
		fieldPath string
		fieldOp   semantic.FieldOp
	}{
		{
			name:   "create",
			change: createChange("app", "v1"),
			action: semantic.Create,
			after:  true,
			write:  true,
		},
		{
			name:      "update",
			change:    updateChange("app", "v1", "v2"),
			action:    semantic.Update,
			before:    true,
			after:     true,
			fields:    true,
			write:     true,
			fieldPath: "/data/key",
			fieldOp:   semantic.FieldReplace,
		},
		{
			name:   "delete",
			change: deleteChange("app", "v1"),
			action: semantic.Delete,
			before: true,
			delete: true,
		},
		{
			name:      "replace",
			change:    replaceChange("app", "v1", "v2"),
			action:    semantic.Replace,
			before:    true,
			after:     true,
			fields:    true,
			write:     true,
			delete:    true,
			fieldPath: "/data/key",
			fieldOp:   semantic.FieldReplace,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(header, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change}, nil, nil)
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
			assert.Equal(t, tt.write, c.Apply.Write != nil)
			assert.Equal(t, tt.delete, c.Apply.Delete != nil)
			assert.Equal(t, tt.fieldPath, path)
			assert.Equal(t, tt.fieldOp, op)
			assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
		})
	}
}

func TestNew_UpdateWithoutAfterRequiresLimitation(t *testing.T) {
	t.Parallel()
	header := semantic.Header{Release: "web", Namespace: "prod"}
	res := ref("ConfigMap", "app")
	change := semantic.ResourceChange{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		Apply:    writeApply(),
	}

	_, err := semantic.New(header, semantic.HelmUpgrade, []semantic.ResourceChange{change}, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "update without after")

	p, err := semantic.New(header, semantic.HelmUpgrade, []semantic.ResourceChange{change}, nil, []semantic.Diagnostic{limitation(res)})
	require.NoError(t, err)
	assert.Nil(t, p.Changes[0].After)
	assert.Empty(t, p.Changes[0].Fields)
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
}

func TestNew_TasksAlwaysNonNil(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, p.Tasks)
	assert.Empty(t, p.Tasks)
	require.NotNil(t, p.Changes)
	assert.Empty(t, p.Changes)
	require.NotNil(t, p.Diagnostics)
	assert.Empty(t, p.Diagnostics)
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	assert.Equal(t, 0, p.Summary.Total())
}

func TestNew_SummaryDerived(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{
		createChange("a", "1"),
		updateChange("b", "1", "2"),
		deleteChange("c", "1"),
		replaceChange("d", "1", "2"),
	}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, semantic.Summary{Create: 1, Update: 1, Delete: 1, Replace: 1}, p.Summary)
	assert.Equal(t, 4, p.Summary.Total())
}

func TestNew_DeterministicOrder(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{
		deleteChange("z", "1"),
		createChange("a", "1"),
		updateChange("m", "1", "2"),
	}, nil, nil)
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
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Deployment", "web"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: obj},
		Apply:    writeApply(),
	}}, nil, nil)
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
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: obj},
		Apply:    writeApply(),
	}
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{change}, nil, nil)
	require.NoError(t, err)
	setObjectString(t, obj, "mutated", "data", "key")
	assert.Equal(t, "v1", objectString(t, p.Changes[0].After.Object, "data", "key"))
}

func TestNew_DoesNotAliasCallerInputs(t *testing.T) {
	t.Parallel()
	helm := &semantic.HelmOrigin{Release: "web", Namespace: "prod"}
	write := &semantic.WriteSemantics{Method: semantic.WriteServerSide, FieldManager: "deployah"}
	del := &semantic.DeleteSemantics{Propagation: semantic.PropagationBackground}
	res := ref("ConfigMap", "app")
	diag := limitation(res)
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: res,
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: helm},
		Action:   semantic.Replace,
		Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
		Apply:    semantic.ApplySemantics{Write: write, Delete: del},
	}}, nil, []semantic.Diagnostic{diag})
	require.NoError(t, err)

	helm.Release = "mutated"
	write.FieldManager = "mutated"
	del.Propagation = 0
	diag.Resource.Name = "mutated"

	require.Len(t, p.Changes, 1)
	assert.Equal(t, "web", p.Changes[0].Origin.Helm.Release)
	assert.Equal(t, "deployah", p.Changes[0].Apply.Write.FieldManager)
	assert.Equal(t, semantic.PropagationBackground, p.Changes[0].Apply.Delete.Propagation)
	require.Len(t, p.Diagnostics, 1)
	assert.Equal(t, "app", p.Diagnostics[0].Resource.Name)
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	got := semantic.Summarize([]semantic.ResourceChange{
		{Action: semantic.Create},
		{Action: semantic.Create},
		{Action: semantic.Update},
		{Action: semantic.Delete},
		{Action: semantic.Replace},
	})
	assert.Equal(t, semantic.Summary{Create: 2, Update: 1, Delete: 1, Replace: 1}, got)
	assert.Equal(t, 5, got.Total())
}

func TestNew_RejectsInvalid(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	tests := []struct {
		name    string
		change  semantic.ResourceChange
		wantErr string
	}{
		{
			name: "unknown origin",
			change: semantic.ResourceChange{
				Resource: res,
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    writeApply(),
			},
			wantErr: "invalid origin",
		},
		{
			name: "helm origin without details",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm},
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    writeApply(),
			},
			wantErr: "helm origin requires helm details",
		},
		{
			name: "create with before",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v0")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    writeApply(),
			},
			wantErr: "create must not have a before snapshot",
		},
		{
			name: "create missing after",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				Apply:    writeApply(),
			},
			wantErr: "create requires an after snapshot",
		},
		{
			name: "update missing write",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			},
			wantErr: "requires write semantics",
		},
		{
			name: "update missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply:    writeApply(),
			},
			wantErr: "update requires a before snapshot",
		},
		{
			name: "delete missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Apply:    deleteApply(),
			},
			wantErr: "delete requires a before snapshot",
		},
		{
			name: "delete with after",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply:    deleteApply(),
			},
			wantErr: "delete must not have an after snapshot",
		},
		{
			name: "replace missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply:    bothApply(),
			},
			wantErr: "replace requires a before snapshot",
		},
		{
			name: "replace missing after",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    bothApply(),
			},
			wantErr: "replace requires an after snapshot",
		},
		{
			name: "replace invalid write",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply: semantic.ApplySemantics{
					Write:  &semantic.WriteSemantics{Method: semantic.WriteServerSide},
					Delete: deleteApply().Delete,
				},
			},
			wantErr: "field manager",
		},
		{
			name: "replace invalid delete",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply: semantic.ApplySemantics{
					Write:  writeApply().Write,
					Delete: &semantic.DeleteSemantics{},
				},
			},
			wantErr: "invalid delete propagation",
		},
		{
			name: "create rejects delete",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    bothApply(),
			},
			wantErr: "must not have delete semantics",
		},
		{
			name: "create rejects missing write",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			},
			wantErr: "requires write semantics",
		},
		{
			name: "update rejects delete",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply:    bothApply(),
			},
			wantErr: "must not have delete semantics",
		},
		{
			name: "delete rejects write",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    bothApply(),
			},
			wantErr: "must not have write semantics",
		},
		{
			name: "delete rejects missing delete",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			},
			wantErr: "requires delete semantics",
		},
		{
			name: "replace rejects write only",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply:    writeApply(),
			},
			wantErr: "requires delete semantics",
		},
		{
			name: "replace rejects delete only",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
				Apply:    deleteApply(),
			},
			wantErr: "requires write semantics",
		},
		{
			name: "replace rejects nil apply",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			},
			wantErr: "requires write semantics",
		},
		{
			name: "invalid write method",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    semantic.ApplySemantics{Write: &semantic.WriteSemantics{}},
			},
			wantErr: "invalid write method",
		},
		{
			name: "invalid delete propagation",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    semantic.ApplySemantics{Delete: &semantic.DeleteSemantics{}},
			},
			wantErr: "invalid delete propagation",
		},
		{
			name: "zero action",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    writeApply(),
			},
			wantErr: "invalid action",
		},
		{
			name: "server-side apply missing field manager",
			change: func() semantic.ResourceChange {
				c := createChange("app", "v1")
				c.Apply.Write.FieldManager = ""
				return c
			}(),
			wantErr: "field manager",
		},
		{
			name: "unencodable after snapshot",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				After:    &semantic.ResourceSnapshot{Object: map[string]any{"n": math.NaN()}},
				Apply:    writeApply(),
			},
			wantErr: "encode after snapshot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change}, nil, nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.ErrorContains(t, err, res.String())
		})
	}
}

func TestNew_DiagnosticValidation(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	tests := []struct {
		name    string
		diag    semantic.Diagnostic
		wantErr string
	}{
		{
			name: "invalid severity",
			diag: semantic.Diagnostic{
				Category: semantic.CategoryPredictionLimitation,
				Message:  "prediction is not exact",
				Resource: &res,
			},
			wantErr: "invalid diagnostic severity",
		},
		{
			name: "invalid category",
			diag: semantic.Diagnostic{
				Severity: semantic.DiagnosticWarning,
				Message:  "prediction is not exact",
				Resource: &res,
			},
			wantErr: "invalid diagnostic category",
		},
		{
			name: "empty message",
			diag: semantic.Diagnostic{
				Severity: semantic.DiagnosticWarning,
				Category: semantic.CategoryPredictionLimitation,
				Resource: &res,
			},
			wantErr: "diagnostic message is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, nil, []semantic.Diagnostic{tt.diag})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestNew_LimitationMatching(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	other := ref("ConfigMap", "other")
	change := semantic.ResourceChange{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		Apply:    writeApply(),
	}
	nilResource := limitation(res)
	nilResource.Resource = nil
	tests := []struct {
		name  string
		diags []semantic.Diagnostic
	}{
		{name: "other resource", diags: []semantic.Diagnostic{limitation(other)}},
		{name: "nil resource", diags: []semantic.Diagnostic{nilResource}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{change}, nil, tt.diags)
			require.Error(t, err)
			assert.ErrorContains(t, err, "update without after")
		})
	}
}

func TestNew_Completeness(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	tests := []struct {
		name    string
		changes []semantic.ResourceChange
		diags   []semantic.Diagnostic
		want    semantic.Completeness
	}{
		{
			name:    "complete create",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			want:    semantic.CompletenessComplete,
		},
		{
			name:    "limitation on create",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			diags:   []semantic.Diagnostic{limitation(res)},
			want:    semantic.CompletenessPartial,
		},
		{
			name:  "limitation without changes",
			diags: []semantic.Diagnostic{limitation(res)},
			want:  semantic.CompletenessPartial,
		},
		{
			name: "update without after",
			changes: []semantic.ResourceChange{{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
				Apply:    writeApply(),
			}},
			diags: []semantic.Diagnostic{limitation(res)},
			want:  semantic.CompletenessPartial,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, tt.changes, nil, tt.diags)
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.Completeness)
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
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: obj},
		Fields:   []semantic.FieldChange{{Path: "/unused", Op: semantic.FieldAdd, After: "x"}},
		Apply:    writeApply(),
	}}, nil, nil)
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
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{},
		After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		Apply:    writeApply(),
	}}, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes[0].Fields)
	assert.Equal(t, semantic.FieldAdd, p.Changes[0].Fields[0].Op)
}

func TestNew_NilSnapshotObject(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{},
		Apply:    writeApply(),
	}}, nil, nil)
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
			p, err := semantic.New(tt.header, tt.helmAction, nil, nil, nil)
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
		{name: "origin crd", changes: []semantic.ResourceChange{crdCreate("widgets.example.com")}},
		{
			name:    "origin helm",
			changes: []semantic.ResourceChange{createChange("app", "v1")},
			wantErr: "helm none must not include helm resource changes",
		},
		{
			name:    "origin namespace",
			changes: []semantic.ResourceChange{nsCreate("prod")},
			wantErr: "namespace origin requires helm install",
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
			name: "will run preDeploy",
			tasks: []semantic.TaskPlan{{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUnchanged,
				WillRun: true,
			}},
			wantErr: "helm none must not will run preDeploy task migrate",
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
			_, err := semantic.New(semantic.Header{}, semantic.HelmNone, tt.changes, tt.tasks, nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNew_OriginNamespaceRequiresInstall(t *testing.T) {
	t.Parallel()
	change := nsCreate("prod")
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
		wantErr    string
	}{
		{name: "install", header: semantic.Header{FreshInstall: true}, helmAction: semantic.HelmInstall},
		{name: "none", helmAction: semantic.HelmNone, wantErr: "namespace origin requires helm install"},
		{name: "upgrade", helmAction: semantic.HelmUpgrade, wantErr: "namespace origin requires helm install"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(tt.header, tt.helmAction, []semantic.ResourceChange{change}, nil, nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNew_OriginCRDAllowedWithEveryHelmAction(t *testing.T) {
	t.Parallel()
	change := crdCreate("widgets.example.com")
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
	}{
		{name: "none", helmAction: semantic.HelmNone},
		{name: "install", header: semantic.Header{FreshInstall: true}, helmAction: semantic.HelmInstall},
		{name: "upgrade", helmAction: semantic.HelmUpgrade},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(tt.header, tt.helmAction, []semantic.ResourceChange{change}, nil, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.helmAction, p.HelmAction)
			assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
		})
	}
}

func TestNew_OriginPayload(t *testing.T) {
	t.Parallel()
	after := &semantic.ResourceSnapshot{Object: cm("app", "v1")}
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
		origin     semantic.ResourceOrigin
		wantErr    string
	}{
		{name: "helm with details", helmAction: semantic.HelmUpgrade, origin: helmOrigin()},
		{
			name:       "helm without details",
			helmAction: semantic.HelmUpgrade,
			origin:     semantic.ResourceOrigin{Kind: semantic.OriginHelm},
			wantErr:    "helm origin requires helm details",
		},
		{name: "crd without helm", helmAction: semantic.HelmNone, origin: semantic.ResourceOrigin{Kind: semantic.OriginCRD}},
		{
			name:       "crd with helm",
			helmAction: semantic.HelmNone,
			origin:     semantic.ResourceOrigin{Kind: semantic.OriginCRD, Helm: helmOrigin().Helm},
			wantErr:    "crd origin must not include helm details",
		},
		{
			name:       "namespace without helm",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			origin:     semantic.ResourceOrigin{Kind: semantic.OriginNamespace},
		},
		{
			name:       "namespace with helm",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			origin:     semantic.ResourceOrigin{Kind: semantic.OriginNamespace, Helm: helmOrigin().Helm},
			wantErr:    "namespace origin must not include helm details",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(tt.header, tt.helmAction, []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "app"),
				Origin:   tt.origin,
				Action:   semantic.Create,
				After:    after,
				Apply:    writeCreate(),
			}}, nil, nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNew_WriteSemantics(t *testing.T) {
	t.Parallel()
	createSSA := createChange("app", "v1")
	createK8s := createChange("app", "v1")
	createK8s.Apply = writeCreate()
	createWithManager := createChange("app", "v1")
	createWithManager.Apply = writeCreate()
	createWithManager.Apply.Write.FieldManager = "deployah"
	createWithForce := createChange("app", "v1")
	createWithForce.Apply = writeCreate()
	createWithForce.Apply.Write.ForceConflicts = true
	updateSSA := updateChange("app", "v1", "v2")
	updateCreate := updateChange("app", "v1", "v2")
	updateCreate.Apply = writeCreate()
	ssaForce := createChange("app", "v1")
	ssaForce.Apply.Write.ForceConflicts = true
	ssaNoManager := createChange("app", "v1")
	ssaNoManager.Apply.Write.FieldManager = ""
	tests := []struct {
		name       string
		header     semantic.Header
		helmAction semantic.HelmAction
		change     semantic.ResourceChange
		wantErr    string
	}{
		{name: "create with create write", helmAction: semantic.HelmUpgrade, change: createK8s},
		{name: "create with server side apply", helmAction: semantic.HelmUpgrade, change: createSSA},
		{name: "update with server side apply", helmAction: semantic.HelmUpgrade, change: updateSSA},
		{
			name:       "update with create write",
			helmAction: semantic.HelmUpgrade,
			change:     updateCreate,
			wantErr:    "update requires server_side_apply",
		},
		{
			name:       "create write with field manager",
			helmAction: semantic.HelmUpgrade,
			change:     createWithManager,
			wantErr:    "create write must not set a field manager",
		},
		{
			name:       "create write with force conflicts",
			helmAction: semantic.HelmUpgrade,
			change:     createWithForce,
			wantErr:    "create write must not force conflicts",
		},
		{
			name:       "server side apply without field manager",
			helmAction: semantic.HelmUpgrade,
			change:     ssaNoManager,
			wantErr:    "server_side_apply requires a field manager",
		},
		{name: "server side apply force conflicts", helmAction: semantic.HelmUpgrade, change: ssaForce},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(tt.header, tt.helmAction, []semantic.ResourceChange{tt.change}, nil, nil)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
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
		want       bool
	}{
		{name: "empty upgrade", helmAction: semantic.HelmUpgrade},
		{name: "empty none", helmAction: semantic.HelmNone},
		{
			name:       "resource change",
			helmAction: semantic.HelmNone,
			changes:    []semantic.ResourceChange{crdCreate("widgets.example.com")},
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(tt.header, tt.helmAction, tt.changes, tt.tasks, nil)
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
		diags      []semantic.Diagnostic
		want       bool
	}{
		{name: "A complete helm none", helmAction: semantic.HelmNone, want: true},
		{name: "B helm upgrade zero effects", helmAction: semantic.HelmUpgrade},
		{
			name:       "C fresh install",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
		},
		{
			name:       "D partial helm none",
			helmAction: semantic.HelmNone,
			diags: []semantic.Diagnostic{{
				Severity: semantic.DiagnosticWarning,
				Category: semantic.CategoryPredictionLimitation,
				Message:  "prediction is not exact",
			}},
		},
		{
			name:       "E helm none with origin crd",
			helmAction: semantic.HelmNone,
			changes:    []semantic.ResourceChange{crdCreate("widgets.example.com")},
		},
		{
			name:       "install with origin namespace",
			header:     semantic.Header{FreshInstall: true},
			helmAction: semantic.HelmInstall,
			changes:    []semantic.ResourceChange{nsCreate("prod")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(tt.header, tt.helmAction, tt.changes, tt.tasks, tt.diags)
			require.NoError(t, err)
			assert.Equal(t, tt.want, p.IsNoOp())
		})
	}
}
