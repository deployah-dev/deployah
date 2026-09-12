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
			name:      "recreate",
			change:    recreateChange("app", "v1", "v2"),
			action:    semantic.Recreate,
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
			p, err := semantic.New(header, []semantic.ResourceChange{tt.change}, nil)
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
		Before:   snap(cm("app", "v1")),
		Apply:    writeApply(),
	}

	_, err := semantic.New(header, []semantic.ResourceChange{change}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "update without after")

	p, err := semantic.New(header, []semantic.ResourceChange{change}, []semantic.Diagnostic{limitation(res)})
	require.NoError(t, err)
	assert.Nil(t, p.Changes[0].After)
	assert.Empty(t, p.Changes[0].Fields)
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
}

func TestNew_ExecutionsAlwaysEmpty(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, p.Executions)
	assert.Empty(t, p.Executions)
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
	assert.Equal(t, 0, p.Summary.Total())
}

func TestNew_SummaryDerived(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{
		createChange("a", "1"),
		updateChange("b", "1", "2"),
		deleteChange("c", "1"),
		recreateChange("d", "1", "2"),
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, semantic.Summary{Create: 1, Update: 1, Delete: 1, Recreate: 1}, p.Summary)
	assert.Equal(t, 4, p.Summary.Total())
}

func TestNew_DeterministicOrder(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{
		deleteChange("z", "1"),
		createChange("a", "1"),
		updateChange("m", "1", "2"),
	}, nil)
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
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: ref("Deployment", "web"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(obj),
		Apply:    writeApply(),
	}}, nil)
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
		After:    snap(obj),
		Apply:    writeApply(),
	}
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{change}, nil)
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
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: res,
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginHelm, Helm: helm},
		Action:   semantic.Recreate,
		Before:   snap(cm("app", "v1")),
		After:    snap(cm("app", "v2")),
		Apply:    semantic.ApplySemantics{Write: write, Delete: del},
	}}, []semantic.Diagnostic{diag})
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
		{Action: semantic.Recreate},
	})
	assert.Equal(t, semantic.Summary{Create: 2, Update: 1, Delete: 1, Recreate: 1}, got)
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
				After:    snap(cm("app", "v1")),
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
				After:    snap(cm("app", "v1")),
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
				Before:   snap(cm("app", "v0")),
				After:    snap(cm("app", "v1")),
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
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
			},
			wantErr: "requires write semantics",
		},
		{
			name: "update missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				After:    snap(cm("app", "v2")),
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
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply:    deleteApply(),
			},
			wantErr: "delete must not have an after snapshot",
		},
		{
			name: "recreate missing before",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				After:    snap(cm("app", "v2")),
				Apply:    bothApply(),
			},
			wantErr: "recreate requires a before snapshot",
		},
		{
			name: "recreate missing after",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				Apply:    bothApply(),
			},
			wantErr: "recreate requires an after snapshot",
		},
		{
			name: "recreate invalid write",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply: semantic.ApplySemantics{
					Write:  &semantic.WriteSemantics{Method: semantic.WriteServerSide},
					Delete: deleteApply().Delete,
				},
			},
			wantErr: "field manager",
		},
		{
			name: "recreate invalid delete",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
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
				After:    snap(cm("app", "v1")),
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
				After:    snap(cm("app", "v1")),
			},
			wantErr: "requires write semantics",
		},
		{
			name: "update rejects delete",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
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
				Before:   snap(cm("app", "v1")),
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
				Before:   snap(cm("app", "v1")),
			},
			wantErr: "requires delete semantics",
		},
		{
			name: "recreate rejects write only",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply:    writeApply(),
			},
			wantErr: "requires delete semantics",
		},
		{
			name: "recreate rejects delete only",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply:    deleteApply(),
			},
			wantErr: "requires write semantics",
		},
		{
			name: "recreate rejects nil apply",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
			},
			wantErr: "requires write semantics",
		},
		{
			name: "invalid write method",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(cm("app", "v1")),
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
				Before:   snap(cm("app", "v1")),
				Apply:    semantic.ApplySemantics{Delete: &semantic.DeleteSemantics{}},
			},
			wantErr: "invalid delete propagation",
		},
		{
			name: "zero action",
			change: semantic.ResourceChange{
				Resource: res,
				Origin:   helmOrigin(),
				After:    snap(cm("app", "v1")),
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
				Before:   snap(cm("app", "v1")),
				After:    snap(map[string]any{"n": math.NaN()}),
				Apply:    writeApply(),
			},
			wantErr: "encode after snapshot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{tt.change}, nil)
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
			_, err := semantic.New(semantic.Header{}, nil, []semantic.Diagnostic{tt.diag})
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
		Before:   snap(cm("app", "v1")),
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
			_, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{change}, tt.diags)
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
				Before:   snap(cm("app", "v1")),
				Apply:    writeApply(),
			}},
			diags: []semantic.Diagnostic{limitation(res)},
			want:  semantic.CompletenessPartial,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Header{}, tt.changes, tt.diags)
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
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(obj),
		Fields:   []semantic.FieldChange{{Path: "/unused", Op: semantic.FieldAdd, After: "x"}},
		Apply:    writeApply(),
	}}, nil)
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
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{},
		After:    snap(cm("app", "v1")),
		Apply:    writeApply(),
	}}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes[0].Fields)
	assert.Equal(t, semantic.FieldAdd, p.Changes[0].Fields[0].Op)
}

func TestNew_NilSnapshotObject(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{},
		Apply:    writeApply(),
	}}, nil)
	require.NoError(t, err)
	require.NotNil(t, p.Changes[0].After)
	assert.Nil(t, p.Changes[0].After.Object)
}
