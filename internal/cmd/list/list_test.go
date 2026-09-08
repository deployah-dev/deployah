package list

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"

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

func TestListCommand_WildcardInstanceIsolated(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "review/pr-123"), "shop", "review/pr-123"),
		labeled(helm.GenerateReleaseName("shop", "review/pr-456"), "shop", "review/pr-456"),
	}
	got, err := listReleases(t.Context(), &mockLister{releases: rels}, Options{
		Project:     "shop",
		Environment: "review/pr-123",
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), got[0].Name)

	_, rows, jsonData := listOutput(got)
	require.Len(t, rows, 1)
	assert.Equal(t, "review/pr-123", rows[0][2])
	assert.Equal(t, "review/pr-123", jsonData[0]["instance"])
	assert.Equal(t, helm.GenerateReleaseName("shop", "review/pr-123"), jsonData[0]["release"])
}

func TestListCommand_NoProjectKeepsMatchingInstances(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "review/pr-123"), "shop", "review/pr-123"),
		labeled(helm.GenerateReleaseName("shop", "review/pr-456"), "shop", "review/pr-456"),
		labeled(helm.GenerateReleaseName("billing", "review/pr-123"), "billing", "review/pr-123"),
		labeled(helm.GenerateReleaseName("billing", "review/pr-456"), "billing", "review/pr-456"),
	}
	got, err := listReleases(t.Context(), &mockLister{releases: rels}, Options{
		Environment: "review/pr-123",
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	assert.ElementsMatch(t, []string{
		helm.GenerateReleaseName("shop", "review/pr-123"),
		helm.GenerateReleaseName("billing", "review/pr-123"),
	}, names)
}

func TestListCommand_LogicalEnvironmentKeepsAllInstances(t *testing.T) {
	t.Parallel()

	rels := []*v1.Release{
		labeled(helm.GenerateReleaseName("shop", "review/pr-123"), "shop", "review/pr-123"),
		labeled(helm.GenerateReleaseName("shop", "review/pr-456"), "shop", "review/pr-456"),
	}
	got, err := listReleases(t.Context(), &mockLister{releases: rels}, Options{
		Project:     "shop",
		Environment: "review",
	})
	require.NoError(t, err)
	require.Len(t, got, 2)

	_, rows, jsonData := listOutput(got)
	require.Len(t, rows, 2)
	assert.NotEqual(t, rows[0][2], rows[1][2])
	assert.NotEqual(t, jsonData[0]["instance"], jsonData[1]["instance"])
}

func TestListCommand_NilIgnoredProductionUnchangedNoFilter(t *testing.T) {
	t.Parallel()

	prod := labeled(helm.GenerateReleaseName("shop", "production"), "shop", "production")
	staging := labeled(helm.GenerateReleaseName("shop", "staging"), "shop", "staging")

	got, err := listReleases(t.Context(), &mockLister{releases: []*v1.Release{prod, nil}}, Options{
		Project:     "shop",
		Environment: "production",
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, prod.Name, got[0].Name)

	all, err := listReleases(t.Context(), &mockLister{releases: []*v1.Release{prod, nil, staging}}, Options{Project: "shop"})
	require.NoError(t, err)
	require.Len(t, all, 2)

	_, rows, jsonData := listOutput(got)
	require.Len(t, rows, 1)
	assert.Equal(t, "production", rows[0][1])
	assert.Equal(t, "production", rows[0][2])
	assert.Equal(t, "production", jsonData[0]["instance"])
}
