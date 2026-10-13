package action_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// Run returns an error when no releases match the project.
func TestStatus_Run_NotFound(t *testing.T) {
	t.Parallel()
	s := action.NewStatus(&mockLister{releases: nil})
	_, err := s.Run(t.Context(), action.StatusParams{Project: "ghost"})
	require.ErrorIs(t, err, action.ErrNoReleases)
	assert.ErrorContains(t, err, `project "ghost"`)
}

// Run returns releases sorted by name.
func TestStatus_Run_FoundAndSorted(t *testing.T) {
	t.Parallel()
	rels := []*v1.Release{
		{Name: "my-app-staging"},
		nil,
		{Name: "my-app-prod"},
	}
	s := action.NewStatus(&mockLister{releases: rels})
	releases, err := s.Run(t.Context(), action.StatusParams{Project: "my-app"})
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.Equal(t, "my-app-prod", releases[0].Name)
	assert.Equal(t, "my-app-staging", releases[1].Name)
}

// Run includes the environment in the not-found error message.
func TestStatus_Run_WithEnvironmentFilter(t *testing.T) {
	t.Parallel()
	s := action.NewStatus(&mockLister{releases: nil})
	_, err := s.Run(t.Context(), action.StatusParams{Project: "my-app", Environment: "prod"})
	require.ErrorIs(t, err, action.ErrNoReleases)
	assert.ErrorContains(t, err, `project "my-app"`)
	assert.ErrorContains(t, err, `in environment "prod"`)
}

func TestStatus_Run_WildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
		{Name: helm.GenerateReleaseName("shop", "review/pr-123")},
	}
	lister := &mockLister{releases: rels}
	got, err := action.NewStatus(lister).Run(t.Context(), action.StatusParams{Project: "shop", Environment: "review/pr-123"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), got[0].Name)
	assertSelector(t, lister, map[string]string{
		"deployah.dev/project":     "shop",
		"deployah.dev/environment": "review",
	})
}

func TestStatus_Run_WildcardInstanceMissing(t *testing.T) {
	t.Parallel()
	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
		nil,
	}
	_, err := action.NewStatus(&mockLister{releases: rels}).Run(t.Context(), action.StatusParams{
		Project: "shop", Environment: "review/pr-123",
	})
	require.ErrorIs(t, err, action.ErrNoReleases)
	assert.ErrorContains(t, err, `project "shop"`)
	assert.ErrorContains(t, err, `in environment "review/pr-123"`)
}

func TestStatus_Run_LogicalEnvironmentKeepsInstances(t *testing.T) {
	t.Parallel()
	later := helm.GenerateReleaseName("shop", "review/pr-456")
	earlier := helm.GenerateReleaseName("shop", "review/pr-123")
	rels := []*v1.Release{
		{Name: later},
		nil,
		{Name: earlier},
	}
	lister := &mockLister{releases: rels}
	got, err := action.NewStatus(lister).Run(t.Context(), action.StatusParams{
		Project: "shop", Environment: "review",
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	want := []string{later, earlier}
	slices.Sort(want)
	assert.Equal(t, want, []string{got[0].Name, got[1].Name})
	assertSelector(t, lister, map[string]string{
		"deployah.dev/project":     "shop",
		"deployah.dev/environment": "review",
	})
}

func TestStatus_Run_ListerError(t *testing.T) {
	t.Parallel()
	errList := errors.New("k8s unavailable")
	lister := &mockLister{err: errList}
	_, err := action.NewStatus(lister).Run(t.Context(), action.StatusParams{Project: "shop"})
	require.ErrorIs(t, err, errList)
	assert.ErrorContains(t, err, "list releases")
	assert.True(t, lister.called)
}

func TestStatus_Run_Selector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		project     string
		environment string
		want        map[string]string
	}{
		{name: "project only", project: "shop", want: map[string]string{"deployah.dev/project": "shop"}},
		{name: "environment only", environment: "production", want: map[string]string{"deployah.dev/environment": "production"}},
		{
			name:        "project and environment",
			project:     "shop",
			environment: "production",
			want: map[string]string{
				"deployah.dev/project":     "shop",
				"deployah.dev/environment": "production",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lister := &mockLister{releases: []*v1.Release{{Name: "kept"}}}
			_, err := action.NewStatus(lister).Run(t.Context(), action.StatusParams{
				Project:     tt.project,
				Environment: tt.environment,
			})
			require.NoError(t, err)
			assertSelector(t, lister, tt.want)
		})
	}
}

func TestStatus_Run_RejectsBadFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		project     string
		environment string
		want        string
	}{
		{
			name:        "nested wildcard",
			project:     "shop",
			environment: "review/foo/bar",
			want:        `environment "review/foo/bar" is invalid`,
		},
		{
			name:    "project is not a label value",
			project: "bad value!",
			want:    "build selector",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lister := &mockLister{releases: []*v1.Release{{Name: "should-not-be-returned"}}}
			_, err := action.NewStatus(lister).Run(t.Context(), action.StatusParams{
				Project:     tt.project,
				Environment: tt.environment,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			assert.False(t, lister.called)
		})
	}
}

func TestStatus_Run_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lister := &mockLister{releases: []*v1.Release{{Name: "shop-prod"}}}
	_, err := action.NewStatus(lister).Run(ctx, action.StatusParams{Project: "shop"})
	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "list releases")
	assert.True(t, lister.called)
}
