package action_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type mockLister struct {
	releases []*v1.Release
	err      error
	selector labels.Selector
	called   bool
	ctx      context.Context
}

func (m *mockLister) ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error) {
	m.called = true
	m.ctx = ctx
	m.selector = selector
	if m.err != nil {
		return nil, m.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.releases, nil
}

// Run returns an empty slice when no releases match.
func TestList_Run_NoReleases(t *testing.T) {
	t.Parallel()
	lister := &mockLister{releases: nil}
	l := action.NewList(lister)
	releases, err := l.Run(t.Context(), action.ListParams{Project: "missing"})
	require.NoError(t, err)
	assert.Equal(t, []*v1.Release{}, releases)
	assert.True(t, lister.called)
}

// Run filters nil entries from lister results and keeps input order.
func TestList_Run_WithReleases(t *testing.T) {
	t.Parallel()
	rels := []*v1.Release{
		{Name: "my-app-prod"},
		nil, // nil entries must be filtered
		{Name: "my-app-staging"},
	}
	l := action.NewList(&mockLister{releases: rels})
	releases, err := l.Run(t.Context(), action.ListParams{Project: "my-app"})
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.Equal(t, "my-app-prod", releases[0].Name)
	assert.Equal(t, "my-app-staging", releases[1].Name)
}

func TestList_Run_WildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-123")},
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
	}
	lister := &mockLister{releases: rels}
	got, err := action.NewList(lister).Run(t.Context(), action.ListParams{Project: "shop", Environment: "review/pr-123"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), got[0].Name)
	assertSelector(t, lister, map[string]string{
		"deployah.dev/project":     "shop",
		"deployah.dev/environment": "review",
	})
}

func TestList_Run_WildcardInstanceMissing(t *testing.T) {
	t.Parallel()
	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
		nil,
	}
	lister := &mockLister{releases: rels}
	got, err := action.NewList(lister).Run(t.Context(), action.ListParams{
		Project: "shop", Environment: "review/pr-123",
	})
	require.NoError(t, err)
	assert.Equal(t, []*v1.Release{}, got)
	assert.True(t, lister.called)
}

func TestList_Run_NoProjectWildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{
			Name: helm.GenerateReleaseName("shop", "review/pr-123"),
			Labels: map[string]string{
				spec.LabelProject:     "shop",
				spec.LabelEnvironment: "review",
			},
		},
		{
			Name: helm.GenerateReleaseName("shop", "review/pr-456"),
			Labels: map[string]string{
				spec.LabelProject:     "shop",
				spec.LabelEnvironment: "review",
			},
		},
		{
			Name: helm.GenerateReleaseName("billing", "review/pr-123"),
			Labels: map[string]string{
				spec.LabelProject:     "billing",
				spec.LabelEnvironment: "review",
			},
		},
		{
			Name: helm.GenerateReleaseName("billing", "review/pr-456"),
			Labels: map[string]string{
				spec.LabelProject:     "billing",
				spec.LabelEnvironment: "review",
			},
		},
		{
			// Name matches shop's instance, but there is no project label
			// to confirm it.
			Name: helm.GenerateReleaseName("shop", "review/pr-123"),
		},
		{
			Name: "shop-review--pr-123-old",
			Labels: map[string]string{
				spec.LabelProject:     "shop",
				spec.LabelEnvironment: "review",
			},
		},
		nil,
	}
	lister := &mockLister{releases: rels}
	got, err := action.NewList(lister).Run(t.Context(), action.ListParams{Environment: "review/pr-123"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	assert.ElementsMatch(t, []string{
		helm.GenerateReleaseName("shop", "review/pr-123"),
		helm.GenerateReleaseName("billing", "review/pr-123"),
	}, names)
	assertSelector(t, lister, map[string]string{
		"deployah.dev/environment": "review",
	})
}

func TestList_Run_LogicalEnvironmentKeepsAllInstances(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		{Name: helm.GenerateReleaseName("shop", "review/pr-123")},
		{Name: helm.GenerateReleaseName("shop", "review/pr-456")},
	}
	lister := &mockLister{releases: rels}
	got, err := action.NewList(lister).Run(t.Context(), action.ListParams{Project: "shop", Environment: "review"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, rels[0].Name, got[0].Name)
	assert.Equal(t, rels[1].Name, got[1].Name)
	assertSelector(t, lister, map[string]string{
		"deployah.dev/project":     "shop",
		"deployah.dev/environment": "review",
	})
}

// Run wraps lister errors.
func TestList_Run_ListerError(t *testing.T) {
	t.Parallel()
	errList := errors.New("k8s unavailable")
	lister := &mockLister{err: errList}
	l := action.NewList(lister)
	_, err := l.Run(t.Context(), action.ListParams{})
	require.ErrorIs(t, err, errList)
	assert.ErrorContains(t, err, "list releases")
	assert.True(t, lister.called)
}

func TestList_Run_Selector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		project     string
		environment string
		want        map[string]string
	}{
		{name: "no filters"},
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
			lister := &mockLister{}
			_, err := action.NewList(lister).Run(t.Context(), action.ListParams{
				Project:     tt.project,
				Environment: tt.environment,
			})
			require.NoError(t, err)
			assertSelector(t, lister, tt.want)
		})
	}
}

// assertSelector checks that the lister was queried with exactly these
// label requirements.
func assertSelector(t *testing.T, lister *mockLister, want map[string]string) {
	t.Helper()
	require.True(t, lister.called)
	// labels.NewSelector returns a nil slice in a non-nil interface.
	// require.NotNil treats that slice as nil, so an empty selector
	// has to be checked with ==.
	if lister.selector == nil {
		t.Fatalf("ListReleases selector = nil, want %#v", want)
	}
	reqs, selectable := lister.selector.Requirements()
	require.True(t, selectable)
	assert.Len(t, reqs, len(want))
	for key, value := range want {
		got, found := lister.selector.RequiresExactMatch(key)
		assert.True(t, found, "selector %q missing %s", lister.selector, key)
		assert.Equal(t, value, got, "selector %q label %s", lister.selector, key)
	}
}

func TestList_Run_RejectsBadFilters(t *testing.T) {
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
			_, err := action.NewList(lister).Run(t.Context(), action.ListParams{
				Project:     tt.project,
				Environment: tt.environment,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
			assert.False(t, lister.called)
		})
	}
}

func TestList_Run_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lister := &mockLister{releases: []*v1.Release{{Name: "shop-prod"}}}
	_, err := action.NewList(lister).Run(ctx, action.ListParams{Project: "shop"})
	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "list releases")
	assert.True(t, lister.called)
}
