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

func TestNew_DriftStartsEmpty(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, p.Drift)
	assert.Empty(t, p.Drift)
	assert.False(t, p.HasDrift())
}

func TestAttachDrift_FreshInstallRejectsDrift(t *testing.T) {
	t.Parallel()
	base := mustPlan(t, semantic.Header{FreshInstall: true}, semantic.HelmInstall)
	tests := []struct {
		name    string
		drift   []semantic.DriftChange
		wantErr string
	}{
		{name: "missing", drift: []semantic.DriftChange{missingDrift("app")}, wantErr: "fresh install must not include drift"},
		{name: "unexpected", drift: []semantic.DriftChange{unexpectedDrift("extra")}, wantErr: "fresh install must not include drift"},
		{name: "nil"},
		{name: "empty", drift: []semantic.DriftChange{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := semantic.AttachDrift(base, tt.drift)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got.Drift)
			assert.Empty(t, got.Drift)
			assert.False(t, got.HasDrift())
		})
	}
}

func TestAttachDrift_Validation(t *testing.T) {
	t.Parallel()
	base := mustPlan(t, semantic.Header{}, semantic.HelmUpgrade)
	live := &semantic.ResourceSnapshot{Object: cm("app", "live")}
	prev := &semantic.ResourceSnapshot{Object: cm("app", "prev")}
	fields := []semantic.FieldChange{{Path: "/data/key", Op: semantic.FieldReplace, Before: "prev", After: "live"}}
	tests := []struct {
		name    string
		drift   semantic.DriftChange
		wantErr string
	}{
		{
			name:    "invalid action",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: 0},
			wantErr: "invalid action",
		},
		{
			name: "name required",
			drift: semantic.DriftChange{
				Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", GenerateName: "app-"},
				Action:   semantic.DriftMissing,
				Previous: prev,
			},
			wantErr: "name is required",
		},
		{
			name: "generateName empty",
			drift: semantic.DriftChange{
				Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Name: "app", GenerateName: "app-"},
				Action:   semantic.DriftMissing,
				Previous: prev,
			},
			wantErr: "generateName must be empty",
		},
		{
			name:    "modified needs previous",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftModified, Live: live, Fields: fields},
			wantErr: "modified requires a previous snapshot",
		},
		{
			name:    "modified needs live",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftModified, Previous: prev, Fields: fields},
			wantErr: "modified requires a live snapshot",
		},
		{
			name:    "modified needs fields",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftModified, Previous: prev, Live: live},
			wantErr: "modified requires a field change",
		},
		{
			name:    "missing needs previous",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftMissing},
			wantErr: "missing requires a previous snapshot",
		},
		{
			name:    "missing rejects live",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftMissing, Previous: prev, Live: live},
			wantErr: "missing must not have a live snapshot",
		},
		{
			name:    "missing rejects fields",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftMissing, Previous: prev, Fields: fields},
			wantErr: "missing must not have field changes",
		},
		{
			name:    "unexpected rejects previous",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftUnexpected, Previous: prev, Live: live},
			wantErr: "unexpected must not have a previous snapshot",
		},
		{
			name:    "unexpected needs live",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftUnexpected},
			wantErr: "unexpected requires a live snapshot",
		},
		{
			name:    "unexpected rejects fields",
			drift:   semantic.DriftChange{Resource: ref("ConfigMap", "app"), Action: semantic.DriftUnexpected, Live: live, Fields: fields},
			wantErr: "unexpected must not have field changes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.AttachDrift(base, []semantic.DriftChange{tt.drift})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestAttachDrift_DuplicateLogicalIdentity(t *testing.T) {
	t.Parallel()
	base := mustPlan(t, semantic.Header{}, semantic.HelmUpgrade)
	first := missingDrift("app")
	second := missingDrift("app")
	second.Resource.APIVersion = "core/v1"
	_, err := semantic.AttachDrift(base, []semantic.DriftChange{first, second})
	require.Error(t, err)
	assert.ErrorContains(t, err, "duplicate logical identity")
}

func TestAttachDrift_SortsAndCopies(t *testing.T) {
	t.Parallel()
	change := updateChange("app", "v1", "v2")
	base, err := semantic.New(semantic.Header{Release: "web"}, semantic.HelmUpgrade, []semantic.ResourceChange{change}, nil)
	require.NoError(t, err)
	summary := base.Summary

	zeta := missingDrift("zeta")
	alpha := unexpectedDrift("alpha")
	alpha.Resource.APIVersion = "apps/v1"
	alpha.Resource.Kind = "Deployment"
	mod := modifiedDrift("mid")
	mod.Fields = []semantic.FieldChange{
		{Path: "/b", Op: semantic.FieldReplace, Before: "1", After: "2"},
		{Path: "/a", Op: semantic.FieldReplace, Before: "1", After: "2"},
	}
	input := []semantic.DriftChange{zeta, alpha, mod}
	got, err := semantic.AttachDrift(base, input)
	require.NoError(t, err)
	require.Len(t, got.Drift, 3)
	// Group sorts before kind, so core ConfigMaps precede apps/Deployment.
	assert.Equal(t, "mid", got.Drift[0].Resource.Name)
	assert.Equal(t, "zeta", got.Drift[1].Resource.Name)
	assert.Equal(t, "alpha", got.Drift[2].Resource.Name)
	assert.Equal(t, "/a", got.Drift[0].Fields[0].Path)
	assert.Equal(t, "/b", got.Drift[0].Fields[1].Path)

	gotData, ok := got.Drift[0].Previous.Object["data"].(map[string]any)
	require.True(t, ok)
	gotData["key"] = "mutated"
	got.Drift[0].Fields[0].Before = "mutated"
	inputData, ok := input[2].Previous.Object["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "prev", inputData["key"])
	assert.Equal(t, "1", input[2].Fields[0].Before)

	assert.Equal(t, summary, got.Summary)
	assert.Equal(t, semantic.HelmUpgrade, got.HelmAction)
	require.Len(t, got.Changes, 1)
	assert.Equal(t, semantic.Update, got.Changes[0].Action)
	assert.True(t, got.HasEffects())
	assert.False(t, got.IsNoOp())
	assert.True(t, got.HasDrift())
}

func TestAttachDrift_DoesNotChangeReleaseIntent(t *testing.T) {
	t.Parallel()
	base, err := semantic.New(semantic.Header{Release: "web"}, semantic.HelmNone, nil, nil)
	require.NoError(t, err)
	got, err := semantic.AttachDrift(base, []semantic.DriftChange{missingDrift("app")})
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmNone, got.HelmAction)
	assert.Empty(t, got.Changes)
	assert.Equal(t, semantic.Summary{}, got.Summary)
	assert.False(t, got.HasEffects())
	assert.True(t, got.IsNoOp())
	assert.True(t, got.HasDrift())
}

func TestDriftAction_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action semantic.DriftAction
		want   string
	}{
		{action: semantic.DriftModified, want: "modified"},
		{action: semantic.DriftMissing, want: "missing"},
		{action: semantic.DriftUnexpected, want: "unexpected"},
		{want: "DriftAction(0)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.action.String())
		})
	}
}

func mustPlan(t *testing.T, header semantic.Header, action semantic.HelmAction) semantic.Plan {
	t.Helper()
	p, err := semantic.New(header, action, nil, nil)
	require.NoError(t, err)
	return p
}

func missingDrift(name string) semantic.DriftChange {
	return semantic.DriftChange{
		Resource: ref("ConfigMap", name),
		Action:   semantic.DriftMissing,
		Previous: &semantic.ResourceSnapshot{Object: cm(name, "prev")},
	}
}

func unexpectedDrift(name string) semantic.DriftChange {
	return semantic.DriftChange{
		Resource: ref("ConfigMap", name),
		Action:   semantic.DriftUnexpected,
		Live:     &semantic.ResourceSnapshot{Object: cm(name, "live")},
	}
}

func modifiedDrift(name string) semantic.DriftChange {
	return semantic.DriftChange{
		Resource: ref("ConfigMap", name),
		Action:   semantic.DriftModified,
		Previous: &semantic.ResourceSnapshot{Object: cm(name, "prev")},
		Live:     &semantic.ResourceSnapshot{Object: cm(name, "live")},
		Fields: []semantic.FieldChange{{
			Path:   "/data/key",
			Op:     semantic.FieldReplace,
			Before: "prev",
			After:  "live",
		}},
	}
}
