package action_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// Run returns an error when no releases match the project.
func TestStatus_Run_NotFound(t *testing.T) {
	s := action.NewStatus(&mockLister{releases: nil})
	_, err := s.Run(t.Context(), action.StatusParams{Project: "ghost"})
	require.ErrorIs(t, err, action.ErrNoReleases)
	assert.Contains(t, err.Error(), `project "ghost"`)
}

// Run returns releases sorted by name.
func TestStatus_Run_FoundAndSorted(t *testing.T) {
	rels := []*v1.Release{
		{Name: "my-app-staging"},
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
	s := action.NewStatus(&mockLister{releases: nil})
	_, err := s.Run(t.Context(), action.StatusParams{Project: "my-app", Environment: "prod"})
	require.ErrorIs(t, err, action.ErrNoReleases)
	assert.Contains(t, err.Error(), `in environment "prod"`)
}

func TestStatus_Run_WildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
		{Name: helm.GenerateReleaseName("shop", "review/pr-123")},
	}
	s := action.NewStatus(&mockLister{releases: rels})
	got, err := s.Run(t.Context(), action.StatusParams{Project: "shop", Environment: "review/pr-123"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), got[0].Name)
}
