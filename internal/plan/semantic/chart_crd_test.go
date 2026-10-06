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

func TestNew_ChartCRDLifecycle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		lifecycle   semantic.ChartCRDLifecycle
		willProcess bool
	}{
		{name: "process", lifecycle: semantic.ChartCRDProcess, willProcess: true},
		{name: "skip", lifecycle: semantic.ChartCRDSkip},
		{name: "upgrade", lifecycle: semantic.ChartCRDUpgrade},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{
				Header:     semantic.Header{FreshInstall: true},
				HelmAction: semantic.HelmInstall,
				ChartCRDs: []semantic.ChartCRD{{
					Source:      ".deployah/crds/widget.yaml",
					Kind:        "CustomResourceDefinition",
					Name:        "widgets.example.com",
					Lifecycle:   tc.lifecycle,
					WillProcess: tc.willProcess,
				}},
			})
			require.NoError(t, err)
			require.Len(t, p.ChartCRDs, 1)
			assert.Equal(t, "CustomResourceDefinition", p.ChartCRDs[0].Kind)
			assert.Equal(t, "widgets.example.com", p.ChartCRDs[0].Name)
			assert.Equal(t, tc.lifecycle, p.ChartCRDs[0].Lifecycle)
			assert.Equal(t, tc.willProcess, p.ChartCRDs[0].WillProcess)
			assert.Empty(t, p.Changes)
		})
	}
}

func TestNew_ChartCRDPreservesOrder(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{
		HelmAction: semantic.HelmUpgrade,
		ChartCRDs: []semantic.ChartCRD{
			{Kind: "CustomResourceDefinition", Name: "one.example.com", Lifecycle: semantic.ChartCRDUpgrade},
			{Kind: "CustomResourceDefinition", Name: "two.example.com", Index: 1, Lifecycle: semantic.ChartCRDUpgrade},
		},
	})
	require.NoError(t, err)
	require.Len(t, p.ChartCRDs, 2)
	assert.Equal(t, "one.example.com", p.ChartCRDs[0].Name)
	assert.Equal(t, "two.example.com", p.ChartCRDs[1].Name)
	assert.Equal(t, 0, p.ChartCRDs[0].Index)
	assert.Equal(t, 1, p.ChartCRDs[1].Index)
}

func TestNew_ChartCRDHasEffectsFollowsWillProcess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		header      semantic.Header
		action      semantic.HelmAction
		lifecycle   semantic.ChartCRDLifecycle
		willProcess bool
		wantEffects bool
		wantNoOp    bool
	}{
		{
			name:        "process",
			header:      semantic.Header{FreshInstall: true},
			action:      semantic.HelmInstall,
			lifecycle:   semantic.ChartCRDProcess,
			willProcess: true,
			wantEffects: true,
		},
		{
			name:      "skip",
			header:    semantic.Header{FreshInstall: true},
			action:    semantic.HelmInstall,
			lifecycle: semantic.ChartCRDSkip,
		},
		{
			name:      "upgrade",
			action:    semantic.HelmNone,
			lifecycle: semantic.ChartCRDUpgrade,
			wantNoOp:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Input{
				Header:     tc.header,
				HelmAction: tc.action,
				ChartCRDs: []semantic.ChartCRD{{
					Kind:        "CustomResourceDefinition",
					Name:        "widgets.example.com",
					Lifecycle:   tc.lifecycle,
					WillProcess: tc.willProcess,
				}},
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantEffects, p.HasEffects())
			assert.Equal(t, tc.wantNoOp, p.IsNoOp())
			assert.Empty(t, p.Changes)
			assert.Equal(t, semantic.Summary{}, p.Summary)
		})
	}
}

func TestNew_ChartCRDValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		crd     semantic.ChartCRD
		wantErr string
	}{
		{name: "wrong kind", crd: semantic.ChartCRD{Kind: "ConfigMap", Name: "x", Lifecycle: semantic.ChartCRDUpgrade}, wantErr: "kind must be CustomResourceDefinition"},
		{name: "missing name", crd: semantic.ChartCRD{Kind: "CustomResourceDefinition", Lifecycle: semantic.ChartCRDUpgrade}, wantErr: "name is required"},
		{name: "invalid lifecycle", crd: semantic.ChartCRD{Kind: "CustomResourceDefinition", Name: "x"}, wantErr: "invalid lifecycle"},
		{name: "process without willProcess", crd: semantic.ChartCRD{Kind: "CustomResourceDefinition", Name: "x", Lifecycle: semantic.ChartCRDProcess}, wantErr: "willProcess must be true"},
		{name: "skip with willProcess", crd: semantic.ChartCRD{Kind: "CustomResourceDefinition", Name: "x", Lifecycle: semantic.ChartCRDSkip, WillProcess: true}, wantErr: "willProcess must be false"},
		{name: "upgrade with willProcess", crd: semantic.ChartCRD{Kind: "CustomResourceDefinition", Name: "x", Lifecycle: semantic.ChartCRDUpgrade, WillProcess: true}, wantErr: "willProcess must be false"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := semantic.New(semantic.Input{
				HelmAction: semantic.HelmUpgrade,
				ChartCRDs:  []semantic.ChartCRD{tc.crd},
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
