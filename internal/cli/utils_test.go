package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// TestExtractDeployahLabels verifies project/environment extraction from
// release labels, including missing and absent label maps.
func TestExtractDeployahLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		labels      map[string]string
		wantProject string
		wantEnv     string
	}{
		{
			name:        "nil labels map",
			labels:      nil,
			wantProject: "unknown",
			wantEnv:     "unknown",
		},
		{
			name:        "empty labels map",
			labels:      map[string]string{},
			wantProject: "unknown",
			wantEnv:     "unknown",
		},
		{
			name: "missing project key",
			labels: map[string]string{
				"deployah.dev/environment": "production",
			},
			wantProject: "unknown",
			wantEnv:     "production",
		},
		{
			name: "missing environment key",
			labels: map[string]string{
				"deployah.dev/project": "my-app",
			},
			wantProject: "my-app",
			wantEnv:     "unknown",
		},
		{
			name: "both present",
			labels: map[string]string{
				"deployah.dev/project":     "my-app",
				"deployah.dev/environment": "staging",
			},
			wantProject: "my-app",
			wantEnv:     "staging",
		},
		{
			name: "empty string values treated as unknown",
			labels: map[string]string{
				"deployah.dev/project":     "",
				"deployah.dev/environment": "",
			},
			wantProject: "unknown",
			wantEnv:     "unknown",
		},
		{
			name: "unrelated labels ignored",
			labels: map[string]string{
				"app.kubernetes.io/name": "my-app",
			},
			wantProject: "unknown",
			wantEnv:     "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rel := &v1.Release{Labels: tt.labels}
			project, environment := extractDeployahLabels(rel)
			assert.Equal(t, tt.wantProject, project)
			assert.Equal(t, tt.wantEnv, environment)
		})
	}
}

// TestReleaseToViewModel verifies the release-to-view-model transformation,
// including releases with nil Info, zero timestamps, and populated config.
func TestReleaseToViewModel(t *testing.T) {
	t.Parallel()

	deployedAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		rel   *v1.Release
		check func(t *testing.T, vm ReleaseViewModel)
	}{
		{
			name: "nil info",
			rel: &v1.Release{
				Name:      "my-release",
				Namespace: "default",
				Version:   1,
			},
			check: func(t *testing.T, vm ReleaseViewModel) {
				t.Helper()
				assert.Equal(t, "my-release", vm.Release)
				assert.Equal(t, "default", vm.Namespace)
				assert.Equal(t, 1, vm.Revision)
				assert.Equal(t, "unknown", vm.Status)
				assert.Empty(t, vm.LastDeployed)
				assert.Empty(t, vm.Age)
				assert.Empty(t, vm.Description)
				assert.Empty(t, vm.Notes)
				assert.Nil(t, vm.Values)
				assert.Equal(t, "unknown", vm.Project)
				assert.Equal(t, "unknown", vm.Environment)
			},
		},
		{
			name: "zero LastDeployed leaves age and timestamp empty",
			rel: &v1.Release{
				Name: "my-release",
				Info: &v1.Info{
					Status: common.StatusDeployed,
				},
			},
			check: func(t *testing.T, vm ReleaseViewModel) {
				t.Helper()
				assert.Equal(t, "deployed", vm.Status)
				assert.Empty(t, vm.LastDeployed)
				assert.Empty(t, vm.Age)
			},
		},
		{
			name: "non-zero LastDeployed populates timestamp and age",
			rel: &v1.Release{
				Name: "my-release",
				Info: &v1.Info{
					Status:       common.StatusDeployed,
					LastDeployed: deployedAt,
					Description:  "install complete",
					Notes:        "thanks for installing",
				},
			},
			check: func(t *testing.T, vm ReleaseViewModel) {
				t.Helper()
				assert.Equal(t, deployedAt.Format(time.RFC3339), vm.LastDeployed)
				assert.NotEmpty(t, vm.Age)
				assert.Equal(t, "install complete", vm.Description)
				assert.Equal(t, "thanks for installing", vm.Notes)
			},
		},
		{
			name: "empty config yields nil Values",
			rel:  &v1.Release{Name: "my-release", Config: map[string]any{}},
			check: func(t *testing.T, vm ReleaseViewModel) {
				t.Helper()
				assert.Nil(t, vm.Values)
			},
		},
		{
			name: "non-empty config is preserved",
			rel: &v1.Release{
				Name:   "my-release",
				Config: map[string]any{"replicaCount": 3, "image": "nginx:latest"},
			},
			check: func(t *testing.T, vm ReleaseViewModel) {
				t.Helper()
				require.NotNil(t, vm.Values)
				assert.Equal(t, 3, vm.Values["replicaCount"])
				assert.Equal(t, "nginx:latest", vm.Values["image"])
			},
		},
		{
			name: "labels populate project and environment",
			rel: &v1.Release{
				Name: "my-release",
				Labels: map[string]string{
					"deployah.dev/project":     "acme",
					"deployah.dev/environment": "prod",
				},
			},
			check: func(t *testing.T, vm ReleaseViewModel) {
				t.Helper()
				assert.Equal(t, "acme", vm.Project)
				assert.Equal(t, "prod", vm.Environment)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.check(t, ReleaseToViewModel(tt.rel))
		})
	}
}

// TestReleaseToViewModelWithPods_NilClient verifies the boundary case where
// no Kubernetes client is available: pod fields fall back to zero values
// rather than panicking on a nil client dereference.
func TestReleaseToViewModelWithPods_NilClient(t *testing.T) {
	t.Parallel()

	rel := &v1.Release{
		Name:      "my-release",
		Namespace: "default",
		Version:   2,
	}

	vm := ReleaseToViewModelWithPods(t.Context(), nil, rel)

	assert.Equal(t, "my-release", vm.Release)
	assert.Equal(t, 0, vm.PodCount)
	assert.Equal(t, 0, vm.ReadyPods)
	assert.Equal(t, "0/0", vm.PodStatus)
}
