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

package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func releaseWithResolved(component string, fields map[string]any) *v1.Release {
	components := map[string]any{}
	if component != "" {
		components[component] = fields
	}
	return &v1.Release{
		Chart: &chart.Chart{
			Values: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{
						"schemaVersion": "1",
						"components":    components,
					},
				},
			},
		},
	}
}

func TestCheckWorkloadGuards_KindChangeRejected(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("db", map[string]any{
		"workloadKind": "Deployment",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {
				Kind: spec.ComponentKindStateful,
				Persistence: &spec.Persistence{
					Size:      "20Gi",
					MountPath: "/data",
				},
			},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind change")
	assert.Contains(t, err.Error(), "Deployment -> StatefulSet")
}

func TestCheckWorkloadGuards_SizeDecreaseRejected(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("db", map[string]any{
		"workloadKind":    "StatefulSet",
		"persistenceSize": "20Gi",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {
				Kind: spec.ComponentKindStateful,
				Persistence: &spec.Persistence{
					Size:      "10Gi",
					MountPath: "/data",
				},
			},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persistence.size decrease")
}

func TestCheckWorkloadGuards_SizeIncreaseAllowed(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("db", map[string]any{
		"workloadKind":    "StatefulSet",
		"persistenceSize": "10Gi",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {
				Kind: spec.ComponentKindStateful,
				Persistence: &spec.Persistence{
					Size:      "20Gi",
					MountPath: "/data",
				},
			},
		},
	}
	require.NoError(t, checkWorkloadGuards(manifest, "production", prev))
}

func TestCheckWorkloadGuards_PersistenceAddRejected(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("peer", map[string]any{
		"workloadKind": "StatefulSet",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"peer": {
				Kind: spec.ComponentKindStateful,
				Persistence: &spec.Persistence{
					Size:      "1Gi",
					MountPath: "/data",
				},
			},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "adding persistence")
}

func TestCheckWorkloadGuards_PersistenceRemoveRejected(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("db", map[string]any{
		"workloadKind":    "StatefulSet",
		"persistenceSize": "20Gi",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {Kind: spec.ComponentKindStateful},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing persistence")
}

func TestPersistenceSizeDecreased(t *testing.T) {
	t.Parallel()
	decreased, err := persistenceSizeDecreased("20Gi", "10Gi")
	require.NoError(t, err)
	assert.True(t, decreased)

	decreased, err = persistenceSizeDecreased("10Gi", "20Gi")
	require.NoError(t, err)
	assert.False(t, decreased)
}

func TestHasStatefulWithPersistence(t *testing.T) {
	t.Parallel()
	identityOnly := &spec.Spec{
		Components: map[string]spec.Component{
			"peer": {Kind: spec.ComponentKindStateful},
		},
	}
	assert.False(t, hasStatefulWithPersistence(identityOnly, "dev"))

	withDisk := &spec.Spec{
		Components: map[string]spec.Component{
			"db": {
				Kind:        spec.ComponentKindStateful,
				Persistence: &spec.Persistence{Size: "1Gi", MountPath: "/data"},
			},
		},
	}
	assert.True(t, hasStatefulWithPersistence(withDisk, "dev"))
}
