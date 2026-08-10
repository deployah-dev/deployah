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

func TestEmitWorkloadWarnings_HPAOnStatefulWithPersistence(t *testing.T) {
	t.Parallel()
	c, _, _, stderr := nabatContextWithIO(t)
	manifest := &spec.Spec{
		Components: map[string]spec.Component{
			"cache": {
				Kind:        spec.ComponentKindStateful,
				Persistence: &spec.Persistence{Size: "5Gi", MountPath: "/data"},
				Autoscaling: &spec.Autoscaling{Enabled: true, MinReplicas: 1, MaxReplicas: 3},
			},
		},
	}
	emitWorkloadWarnings(c, manifest, "dev", map[string]map[string]any{})
	assert.Contains(t, stderr.String(), "retains PVCs on scale-down")
}

func TestEmitWorkloadWarnings_ExposeMultiReplicaStateful(t *testing.T) {
	t.Parallel()
	c, _, _, stderr := nabatContextWithIO(t)
	replicas := 3
	manifest := &spec.Spec{
		Components: map[string]spec.Component{
			"peer": {
				Kind:     spec.ComponentKindStateful,
				Replicas: &replicas,
				Expose:   &spec.Expose{},
			},
		},
	}
	emitWorkloadWarnings(c, manifest, "dev", map[string]map[string]any{})
	assert.Contains(t, stderr.String(), "expose with replicas > 1")
}

func TestEmitWorkloadWarnings_MountPathChange(t *testing.T) {
	t.Parallel()
	c, _, _, stderr := nabatContextWithIO(t)
	prev := map[string]map[string]any{
		"db": {
			"workloadKind":         "StatefulSet",
			"persistenceSize":      "10Gi",
			"persistenceMountPath": "/old/data",
		},
	}
	manifest := &spec.Spec{
		Components: map[string]spec.Component{
			"db": {
				Kind:        spec.ComponentKindStateful,
				Persistence: &spec.Persistence{Size: "10Gi", MountPath: "/new/data"},
			},
		},
	}
	emitWorkloadWarnings(c, manifest, "dev", prev)
	assert.Contains(t, stderr.String(), "persistence.mountPath change")
	assert.Contains(t, stderr.String(), "/old/data")
	assert.Contains(t, stderr.String(), "/new/data")
}

func TestEmitWorkloadWarnings_NoWarningsWhenClean(t *testing.T) {
	t.Parallel()
	c, _, _, stderr := nabatContextWithIO(t)
	manifest := &spec.Spec{
		Components: map[string]spec.Component{
			"web": {Kind: spec.ComponentKindStateless},
		},
	}
	emitWorkloadWarnings(c, manifest, "dev", map[string]map[string]any{})
	assert.Empty(t, stderr.String())
}

func TestComponentActiveInEnv(t *testing.T) {
	t.Parallel()
	assert.True(t, componentActiveInEnv(spec.Component{}, "dev"), "no filter = active everywhere")
	assert.True(t, componentActiveInEnv(spec.Component{Environments: []string{"dev"}}, "dev"))
	assert.False(t, componentActiveInEnv(spec.Component{Environments: []string{"staging"}}, "dev"))
	assert.True(t, componentActiveInEnv(spec.Component{Environments: []string{"review"}}, "review/pr-42"), "prefix match")
}

func TestPreviousResolvedComponents_NonMapComponent(t *testing.T) {
	t.Parallel()
	values := map[string]any{
		"deployah": map[string]any{
			"resolved": map[string]any{
				"components": map[string]any{
					"db":      map[string]any{"workloadKind": "StatefulSet"},
					"invalid": "not-a-map",
				},
			},
		},
	}
	got := previousResolvedComponents(values)
	assert.Len(t, got, 1)
	assert.Contains(t, got, "db")
}

func TestCheckWorkloadGuards_InactiveComponentSkipped(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("db", map[string]any{
		"workloadKind": "Deployment",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {
				Kind:         spec.ComponentKindStateful,
				Environments: []string{"staging"},
			},
		},
	}
	require.NoError(t, checkWorkloadGuards(manifest, "production", prev))
}

func TestCheckWorkloadGuards_SizeParseError(t *testing.T) {
	t.Parallel()
	prev := map[string]map[string]any{
		"db": {
			"workloadKind":    "StatefulSet",
			"persistenceSize": "not-a-quantity",
		},
	}
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"db": {
				Kind:        spec.ComponentKindStateful,
				Persistence: &spec.Persistence{Size: "10Gi", MountPath: "/data"},
			},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse previous persistence.size")
}

func TestCheckWorkloadGuards_RoleChangeRejected(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("api", map[string]any{
		"workloadKind": "Deployment",
		"role":         "service",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleWorker, Image: "worker:1"},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role change")
	assert.Contains(t, err.Error(), "service -> worker")
}

func TestCheckWorkloadGuards_SameRoleAllowed(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("api", map[string]any{
		"workloadKind": "Deployment",
		"role":         "worker",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleWorker, Image: "worker:1"},
		},
	}
	assert.NoError(t, checkWorkloadGuards(manifest, "production", prev))
}

func TestCheckWorkloadGuards_MissingPrevRoleTreatedAsService(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("api", map[string]any{
		"workloadKind": "Deployment",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleWorker, Image: "worker:1"},
		},
	}
	err := checkWorkloadGuards(manifest, "production", prev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role change")
	assert.Contains(t, err.Error(), "service -> worker")
}

func TestCheckWorkloadGuards_MissingPrevRoleSameAsServiceAllowed(t *testing.T) {
	t.Parallel()
	prev := previousResolvedComponents(releaseWithResolved("api", map[string]any{
		"workloadKind": "Deployment",
	}).Chart.Values)
	manifest := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleService, Image: "api:1", Port: 8080},
		},
	}
	assert.NoError(t, checkWorkloadGuards(manifest, "production", prev))
}
