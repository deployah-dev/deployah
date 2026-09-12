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

	t.Run("create", func(t *testing.T) {
		t.Parallel()
		p, err := semantic.New(header, []semantic.ResourceChange{createChange("app", "v1")}, nil)
		require.NoError(t, err)
		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Create, p.Changes[0].Action)
		assert.Nil(t, p.Changes[0].Before)
		require.NotNil(t, p.Changes[0].After)
		assert.Empty(t, p.Changes[0].Fields)
		assert.NotNil(t, p.Changes[0].Apply.Write)
		assert.Nil(t, p.Changes[0].Apply.Delete)
		assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
	})

	t.Run("update", func(t *testing.T) {
		t.Parallel()
		p, err := semantic.New(header, []semantic.ResourceChange{updateChange("app", "v1", "v2")}, nil)
		require.NoError(t, err)
		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Update, p.Changes[0].Action)
		require.NotNil(t, p.Changes[0].Before)
		require.NotNil(t, p.Changes[0].After)
		require.NotEmpty(t, p.Changes[0].Fields)
		assert.Equal(t, "/data/key", p.Changes[0].Fields[0].Path)
		assert.Equal(t, semantic.FieldReplace, p.Changes[0].Fields[0].Op)
		assert.NotNil(t, p.Changes[0].Apply.Write)
		assert.Nil(t, p.Changes[0].Apply.Delete)
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		p, err := semantic.New(header, []semantic.ResourceChange{deleteChange("app", "v1")}, nil)
		require.NoError(t, err)
		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Delete, p.Changes[0].Action)
		require.NotNil(t, p.Changes[0].Before)
		assert.Nil(t, p.Changes[0].After)
		assert.Empty(t, p.Changes[0].Fields)
		assert.Nil(t, p.Changes[0].Apply.Write)
		assert.NotNil(t, p.Changes[0].Apply.Delete)
	})

	t.Run("recreate", func(t *testing.T) {
		t.Parallel()
		p, err := semantic.New(header, []semantic.ResourceChange{recreateChange("app", "v1", "v2")}, nil)
		require.NoError(t, err)
		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Recreate, p.Changes[0].Action)
		require.NotNil(t, p.Changes[0].Before)
		require.NotNil(t, p.Changes[0].After)
		require.NotEmpty(t, p.Changes[0].Fields)
		assert.NotNil(t, p.Changes[0].Apply.Write)
		assert.NotNil(t, p.Changes[0].Apply.Delete)
	})
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

func TestNew_InvalidZeroAction(t *testing.T) {
	t.Parallel()
	_, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		After:    snap(cm("app", "v1")),
		Apply:    writeApply(),
	}}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid action")
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

func TestNew_ApplyInvariants(t *testing.T) {
	t.Parallel()
	header := semantic.Header{}
	tests := []struct {
		name    string
		change  semantic.ResourceChange
		wantErr string
	}{
		{
			name: "create write only",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(cm("app", "v1")),
				Apply:    writeApply(),
			},
		},
		{
			name: "create rejects delete",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
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
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(cm("app", "v1")),
			},
			wantErr: "requires write semantics",
		},
		{
			name: "update write only",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply:    writeApply(),
			},
		},
		{
			name: "update rejects delete",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply:    bothApply(),
			},
			wantErr: "must not have delete semantics",
		},
		{
			name: "delete delete only",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   snap(cm("app", "v1")),
				Apply:    deleteApply(),
			},
		},
		{
			name: "delete rejects write",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
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
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   snap(cm("app", "v1")),
			},
			wantErr: "requires delete semantics",
		},
		{
			name: "recreate requires both",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(cm("app", "v1")),
				After:    snap(cm("app", "v2")),
				Apply:    bothApply(),
			},
		},
		{
			name: "recreate rejects write only",
			change: semantic.ResourceChange{
				Resource: ref("ConfigMap", "app"),
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
				Resource: ref("ConfigMap", "app"),
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
				Resource: ref("ConfigMap", "app"),
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
				Resource: ref("ConfigMap", "app"),
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
				Resource: ref("ConfigMap", "app"),
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   snap(cm("app", "v1")),
				Apply:    semantic.ApplySemantics{Delete: &semantic.DeleteSemantics{}},
			},
			wantErr: "invalid delete propagation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(header, []semantic.ResourceChange{tt.change}, nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestActionString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "create", semantic.Create.String())
	assert.Equal(t, "update", semantic.Update.String())
	assert.Equal(t, "delete", semantic.Delete.String())
	assert.Equal(t, "recreate", semantic.Recreate.String())
	assert.Equal(t, "Action(0)", semantic.Action(0).String())
	assert.Equal(t, "complete", semantic.CompletenessComplete.String())
	assert.Equal(t, "partial", semantic.CompletenessPartial.String())
	assert.Equal(t, "Completeness(0)", semantic.Completeness(0).String())
}
