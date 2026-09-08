package status

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type mockLister struct {
	releases []*v1.Release
	err      error
}

func (m *mockLister) ListReleases(_ context.Context, _ labels.Selector) ([]*v1.Release, error) {
	return m.releases, m.err
}

func labeled(name, project, original string) *v1.Release {
	env := spec.NormalizeEnv(original)
	return &v1.Release{
		Name: name,
		Labels: map[string]string{
			spec.LabelProject:     project,
			spec.LabelEnvironment: env.MapKey,
		},
		Chart: &chart.Chart{Values: map[string]any{
			"deployah": map[string]any{"environmentInstance": original},
		}},
	}
}

func TestStatusCommand_WildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "review/pr-456"), "shop", "review/pr-456"),
		labeled(helm.GenerateReleaseName("shop", "review/pr-123"), "shop", "review/pr-123"),
		nil,
	}
	got, err := statusReleases(t.Context(), &mockLister{releases: rels}, Options{
		Project:     "shop",
		Environment: "review/pr-123",
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), got[0].Name)

	_, rows, vms := statusOutput(got, cli.ReleaseToViewModel)
	require.Len(t, rows, 1)
	assert.Equal(t, "review/pr-123", rows[0][2])
	assert.Equal(t, "review/pr-123", vms[0].Instance)
}

func TestStatusCommand_LogicalEnvironmentKeepsAllInstances(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "review/pr-123"), "shop", "review/pr-123"),
		labeled(helm.GenerateReleaseName("shop", "review/pr-456"), "shop", "review/pr-456"),
	}
	got, err := statusReleases(t.Context(), &mockLister{releases: rels}, Options{
		Project:     "shop",
		Environment: "review",
	})
	require.NoError(t, err)
	require.Len(t, got, 2)

	_, rows, vms := statusOutput(got, cli.ReleaseToViewModel)
	require.Len(t, rows, 2)
	assert.NotEqual(t, rows[0][2], rows[1][2])
	assert.NotEqual(t, vms[0].Instance, vms[1].Instance)
}

func TestStatusCommand_ProductionUnchanged(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "production"), "shop", "production"),
		nil,
	}
	got, err := statusReleases(t.Context(), &mockLister{releases: rels}, Options{
		Project:     "shop",
		Environment: "production",
	})
	require.NoError(t, err)
	require.Len(t, got, 1)

	all, err := statusReleases(t.Context(), &mockLister{releases: []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "production"), "shop", "production"),
		labeled(helm.GenerateReleaseName("shop", "staging"), "shop", "staging"),
		nil,
	}}, Options{Project: "shop"})
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestWrapStatusError_NoReleasesHint(t *testing.T) {
	t.Parallel()

	err := wrapStatusError(action.ErrNoReleases)
	require.ErrorIs(t, err, action.ErrNoReleases)
	assert.Contains(t, err.Error(), "Hint:")

	other := errors.New("helm down")
	assert.Equal(t, other, wrapStatusError(other))
	assert.Nil(t, wrapStatusError(nil))
}
