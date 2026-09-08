package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type mockLister struct {
	releases []*v1.Release
	err      error
}

func (m *mockLister) ListReleases(_ context.Context, _ labels.Selector) ([]*v1.Release, error) {
	return m.releases, m.err
}

// Run returns an empty slice when no releases match.
func TestList_Run_NoReleases(t *testing.T) {
	l := action.NewList(&mockLister{releases: nil})
	releases, err := l.Run(t.Context(), action.ListParams{Project: "missing"})
	require.NoError(t, err)
	assert.Empty(t, releases)
}

// Run filters nil entries from lister results.
func TestList_Run_WithReleases(t *testing.T) {
	rels := []*v1.Release{
		{Name: "my-app-prod"},
		nil, // nil entries must be filtered
		{Name: "my-app-staging"},
	}
	l := action.NewList(&mockLister{releases: rels})
	releases, err := l.Run(t.Context(), action.ListParams{Project: "my-app"})
	require.NoError(t, err)
	assert.Len(t, releases, 2)
}

func TestList_Run_WildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-123")},
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
	}
	l := action.NewList(&mockLister{releases: rels})
	got, err := l.Run(t.Context(), action.ListParams{Project: "shop", Environment: "review/pr-123"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), got[0].Name)
}

func TestList_Run_NoProjectWildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{
			Name: helm.GenerateReleaseName("shop", "review/pr-123"),
			Labels: map[string]string{
				"deployah.dev/project":     "shop",
				"deployah.dev/environment": "review",
			},
		},
		{
			Name: helm.GenerateReleaseName("shop", "review/pr-456"),
			Labels: map[string]string{
				"deployah.dev/project":     "shop",
				"deployah.dev/environment": "review",
			},
		},
		{
			Name: helm.GenerateReleaseName("billing", "review/pr-123"),
			Labels: map[string]string{
				"deployah.dev/project":     "billing",
				"deployah.dev/environment": "review",
			},
		},
		{
			Name: helm.GenerateReleaseName("billing", "review/pr-456"),
			Labels: map[string]string{
				"deployah.dev/project":     "billing",
				"deployah.dev/environment": "review",
			},
		},
		nil,
	}
	l := action.NewList(&mockLister{releases: rels})
	got, err := l.Run(t.Context(), action.ListParams{Environment: "review/pr-123"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	assert.ElementsMatch(t, []string{
		helm.GenerateReleaseName("shop", "review/pr-123"),
		helm.GenerateReleaseName("billing", "review/pr-123"),
	}, names)
}

func TestList_Run_LogicalEnvironmentKeepsAllInstances(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-123")},
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
	}
	l := action.NewList(&mockLister{releases: rels})
	got, err := l.Run(t.Context(), action.ListParams{Project: "shop", Environment: "review"})
	require.NoError(t, err)
	require.Len(t, got, 2)
}

// Run wraps lister errors.
func TestList_Run_ListerError(t *testing.T) {
	l := action.NewList(&mockLister{err: fmt.Errorf("k8s unavailable")})
	_, err := l.Run(t.Context(), action.ListParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list releases")
}
