package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/deployah-dev/deployah/internal/action"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type mockLister struct {
	releases []*v1.Release
	err      error
}

func (m *mockLister) ListReleases(_ context.Context, _ labels.Selector) ([]*v1.Release, error) {
	return m.releases, m.err
}

func TestList_Run_NoReleases(t *testing.T) {
	l := action.NewList(&mockLister{releases: nil})
	releases, err := l.Run(context.Background(), action.ListParams{Project: "missing"})
	require.NoError(t, err)
	assert.Empty(t, releases)
}

func TestList_Run_WithReleases(t *testing.T) {
	rels := []*v1.Release{
		{Name: "my-app-prod"},
		nil, // nil entries must be filtered
		{Name: "my-app-staging"},
	}
	l := action.NewList(&mockLister{releases: rels})
	releases, err := l.Run(context.Background(), action.ListParams{Project: "my-app"})
	require.NoError(t, err)
	assert.Len(t, releases, 2)
}

func TestList_Run_ListerError(t *testing.T) {
	l := action.NewList(&mockLister{err: fmt.Errorf("k8s unavailable")})
	_, err := l.Run(context.Background(), action.ListParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list releases")
}
