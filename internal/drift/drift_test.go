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

// stubLive implements [LiveReader] with per-resource canned responses,
// keyed by resource label ("Kind/namespace/name").
type stubLive struct {
	live  map[string]string
	errs  map[string]error
	calls []string
}

func (s *stubLive) Live(_ context.Context, resourceYAML string) (string, error) {
	resources, splitErr := planengine.SplitResources(resourceYAML)
	if splitErr != nil || len(resources) != 1 {
		return "", errors.New("stubLive: expected exactly one resource")
	}
	label := resources[0].Label
	s.calls = append(s.calls, label)

	if e, ok := s.errs[label]; ok {
		return "", e
	}
	return s.live[label], nil
}

func specPlanWithChange(kind, namespace, name string, fields ...planengine.FieldDiff) *planengine.Plan {
	return &planengine.Plan{
		Changes: []planengine.Change{
			{Action: planengine.ActionChange, Kind: kind, Namespace: namespace, Name: name, Fields: fields},
		},
	}
}

func TestComputeDrift_FreshInstall(t *testing.T) {
	t.Parallel()
	stub := &stubLive{}
	result, err := ComputeDrift(t.Context(), stub, &planengine.Plan{Header: planengine.Header{FreshInstall: true}}, driftDeployment)
	require.NoError(t, err)
	assert.False(t, result.HasDrift())
	assert.Empty(t, result.Incomplete)
	assert.Empty(t, stub.calls)
}

func TestComputeDrift_ActionAdd(t *testing.T) {
	t.Parallel()
	stub := &stubLive{
		live: map[string]string{"Deployment/default/web": driftDeploymentReplicas9},
	}
	specPlan := &planengine.Plan{
		Changes: []planengine.Change{
			{Action: planengine.ActionAdd, Kind: "Deployment", Namespace: "default", Name: "web"},
		},
	}
	result, err := ComputeDrift(t.Context(), stub, specPlan, driftDeployment)
	require.NoError(t, err)
	assert.False(t, result.HasDrift())
	assert.Empty(t, result.Incomplete)
	assert.Empty(t, stub.calls)
}

func TestComputeDrift_UnexplainedField(t *testing.T) {
	t.Parallel()
	stub := &stubLive{
		live: map[string]string{"Deployment/default/web": driftDeploymentReplicas5},
	}
	result, err := ComputeDrift(t.Context(), stub, &planengine.Plan{}, driftDeployment)
	require.NoError(t, err)
	require.True(t, result.HasDrift())
	require.Len(t, result.Changes, 1)
	assert.Equal(t, "Deployment", result.Changes[0].Kind)
	require.Len(t, result.Changes[0].Fields, 1)
	assert.Equal(t, "spec.replicas", result.Changes[0].Fields[0].Path)
	assert.Equal(t, "2", result.Changes[0].Fields[0].Old)
	assert.Equal(t, "5", result.Changes[0].Fields[0].New)
}

func TestComputeDrift(t *testing.T) {
	t.Parallel()

	const label = "Deployment/default/web"

	tests := []struct {
		name           string
		stub           *stubLive
		specPlan       *planengine.Plan
		wantDrift      bool
		wantIncomplete int
		incompleteHas  []string
	}{
		{
			name: "identical desired and live never double-reports",
			stub: &stubLive{
				live: map[string]string{label: driftDeployment},
			},
			specPlan: specPlanWithChange("Deployment", "default", "web",
				planengine.FieldDiff{Path: "spec.replicas", ChangeKind: planengine.FieldChanged, Old: "1", New: "2"},
			),
		},
		{
			name: "explained path is subtracted",
			stub: &stubLive{
				live: map[string]string{label: driftDeploymentReplicas3},
			},
			specPlan: specPlanWithChange("Deployment", "default", "web",
				planengine.FieldDiff{Path: "spec.replicas", ChangeKind: planengine.FieldChanged, Old: "1", New: "2"},
			),
		},
		{
			name: "no live baseline skips resource",
			stub: &stubLive{
				live: map[string]string{},
			},
			specPlan: &planengine.Plan{},
		},
		{
			name: "get error marks incomplete",
			stub: &stubLive{
				errs: map[string]error{label: errors.New(`deployments.apps "web" is forbidden: User "ci" cannot get resource`)},
			},
			specPlan:       &planengine.Plan{},
			wantIncomplete: 1,
			incompleteHas:  []string{label, "forbidden"},
		},
		{
			name: "malformed live YAML marks incomplete",
			stub: &stubLive{
				live: map[string]string{label: "not: valid: yaml: ["},
			},
			specPlan:       &planengine.Plan{},
			wantIncomplete: 1,
			incompleteHas:  []string{label, "decode resource"},
		},
		{
			name: "live-only defaulted fields are not drift",
			stub: &stubLive{
				live: map[string]string{label: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
  uid: abc
  resourceVersion: "9"
  annotations:
    extra: live-only
spec:
  replicas: 2
status:
  replicas: 5
`},
			},
			specPlan: &planengine.Plan{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ComputeDrift(t.Context(), tt.stub, tt.specPlan, driftDeployment)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDrift, result.HasDrift())
			require.Len(t, result.Incomplete, tt.wantIncomplete)
			for _, s := range tt.incompleteHas {
				assert.Contains(t, result.Incomplete[0], s)
			}
		})
	}
}

// TestComputeDrift_MalformedManifest verifies a currentManifest that fails
// to split into resources surfaces a wrapped error instead of a panic or a
// silently empty result.
func TestComputeDrift_MalformedManifest(t *testing.T) {
	t.Parallel()

	result, err := ComputeDrift(t.Context(), &stubLive{}, &planengine.Plan{}, "not: valid: yaml: [")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "split rendered manifest")
}

// TestResourceLabel verifies both the namespaced and cluster-scoped label
// formats, matching [planengine.SplitResources]'s [planengine.ResourceYAML.Label].
func TestResourceLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		kind      string
		namespace string
		resource  string
		want      string
	}{
		{name: "namespaced resource", kind: "Deployment", namespace: "default", resource: "web", want: "Deployment/default/web"},
		{name: "cluster-scoped resource has no namespace segment", kind: "ClusterRole", namespace: "", resource: "admin", want: "ClusterRole/admin"},
		{name: "empty kind and name still format consistently", kind: "", namespace: "", resource: "", want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, resourceLabel(tt.kind, tt.namespace, tt.resource))
		})
	}
}
