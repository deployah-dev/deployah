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

package drift

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	planengine "deployah.dev/deployah/internal/plan"
)

const driftDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
`

const driftDeploymentReplicas5 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 5
`

const driftDeploymentReplicas3 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 3
`

const driftDeploymentReplicas9 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 9
`

// stubPredictor implements [Predictor] with per-resource canned responses,
// keyed by resource label ("Kind/namespace/name").
type stubPredictor struct {
	predicted map[string]string
	live      map[string]string
	errs      map[string]error
	calls     []string
}

func (s *stubPredictor) Predict(_ context.Context, resourceYAML string) (predicted, live string, err error) {
	resources, splitErr := planengine.SplitResources(resourceYAML)
	if splitErr != nil || len(resources) != 1 {
		return "", "", errors.New("stubPredictor: expected exactly one resource")
	}
	label := resources[0].Label
	s.calls = append(s.calls, label)

	if e, ok := s.errs[label]; ok {
		return "", "", e
	}
	return s.predicted[label], s.live[label], nil
}

func specPlanWithChange(kind, namespace, name string, fields ...planengine.FieldDiff) *planengine.Plan {
	return &planengine.Plan{
		Changes: []planengine.Change{
			{Action: planengine.ActionChange, Kind: kind, Namespace: namespace, Name: name, Fields: fields},
		},
	}
}

// TestComputeDrift covers subtraction, ActionAdd skipping, incomplete
// marking, and the fresh-install short-circuit.
func TestComputeDrift(t *testing.T) {
	t.Parallel()

	const label = "Deployment/default/web"

	tests := []struct {
		name           string
		stub           *stubPredictor
		specPlan       *planengine.Plan
		wantDrift      bool
		wantCallsEmpty bool
		wantIncomplete int
		incompleteHas  []string
		check          func(t *testing.T, result *Result)
	}{
		{
			name:           "fresh install is no-op",
			stub:           &stubPredictor{},
			specPlan:       &planengine.Plan{Header: planengine.Header{FreshInstall: true}},
			wantCallsEmpty: true,
		},
		{
			name: "unexplained field is drift",
			stub: &stubPredictor{
				predicted: map[string]string{label: driftDeployment},
				live:      map[string]string{label: driftDeploymentReplicas5},
			},
			specPlan:  &planengine.Plan{},
			wantDrift: true,
			check: func(t *testing.T, result *Result) {
				t.Helper()
				require.Len(t, result.Changes, 1)
				assert.Equal(t, "Deployment", result.Changes[0].Kind)
				require.Len(t, result.Changes[0].Fields, 1)
				assert.Equal(t, "spec.replicas", result.Changes[0].Fields[0].Path)
				assert.Equal(t, "2", result.Changes[0].Fields[0].Old)
				assert.Equal(t, "5", result.Changes[0].Fields[0].New)
			},
		},
		{
			name: "identical predicted and live never double-reports",
			stub: &stubPredictor{
				predicted: map[string]string{label: driftDeployment},
				live:      map[string]string{label: driftDeployment},
			},
			specPlan: specPlanWithChange("Deployment", "default", "web",
				planengine.FieldDiff{Path: "spec.replicas", ChangeKind: planengine.FieldChanged, Old: "1", New: "2"},
			),
		},
		{
			name: "explained path is subtracted",
			stub: &stubPredictor{
				predicted: map[string]string{label: driftDeployment},
				live:      map[string]string{label: driftDeploymentReplicas3},
			},
			specPlan: specPlanWithChange("Deployment", "default", "web",
				planengine.FieldDiff{Path: "spec.replicas", ChangeKind: planengine.FieldChanged, Old: "1", New: "2"},
			),
		},
		{
			name: "ActionAdd with existing live skips field drift",
			stub: &stubPredictor{
				predicted: map[string]string{label: driftDeployment},
				live:      map[string]string{label: driftDeploymentReplicas9},
			},
			specPlan: &planengine.Plan{
				Changes: []planengine.Change{
					{Action: planengine.ActionAdd, Kind: "Deployment", Namespace: "default", Name: "web"},
				},
			},
			wantCallsEmpty: true,
		},
		{
			name: "no live baseline skips resource",
			stub: &stubPredictor{
				predicted: map[string]string{label: driftDeployment},
				live:      map[string]string{},
			},
			specPlan: &planengine.Plan{},
		},
		{
			name: "predict error marks incomplete",
			stub: &stubPredictor{
				errs: map[string]error{label: errors.New(`deployments.apps "web" is forbidden: User "ci" cannot patch resource`)},
			},
			specPlan:       &planengine.Plan{},
			wantIncomplete: 1,
			incompleteHas:  []string{label, "forbidden"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ComputeDrift(t.Context(), tt.stub, tt.specPlan, driftDeployment)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDrift, result.HasDrift())
			if tt.wantCallsEmpty {
				assert.Empty(t, tt.stub.calls)
			}
			require.Len(t, result.Incomplete, tt.wantIncomplete)
			for _, s := range tt.incompleteHas {
				require.NotEmpty(t, result.Incomplete)
				assert.Contains(t, result.Incomplete[0], s)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
