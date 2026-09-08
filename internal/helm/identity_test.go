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

package helm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation"

	"deployah.dev/deployah/internal/spec"
)

func identityResolved(project, env string) *spec.ResolvedSpec {
	return &spec.ResolvedSpec{
		Spec: &spec.Spec{
			APIVersion: spec.CurrentManifestVersion,
			Project:    project,
		},
		Env:        spec.NormalizeEnv(env),
		Components: map[string]spec.ResolvedComponent{},
		Tasks:      map[string]spec.ResolvedTask{},
	}
}

func TestReleaseIdentity_FromResolvedSpec(t *testing.T) {
	t.Parallel()

	resolved := identityResolved("shop", "staging")

	name, labels, err := releaseIdentity(resolved)
	require.NoError(t, err)
	assert.Equal(t, GenerateReleaseName("shop", "staging"), name)
	assert.Equal(t, map[string]string{
		"deployah.dev/project":     "shop",
		"deployah.dev/environment": "staging",
		"deployah.dev/managed-by":  "deployah",
		"deployah.dev/version":     spec.CurrentManifestVersion,
	}, labels)
}

func TestReleaseIdentity_WildcardUsesMapKey(t *testing.T) {
	t.Parallel()

	resolved := identityResolved("shop", "review/pr-123")

	name, labels, err := releaseIdentity(resolved)
	require.NoError(t, err)
	assert.Equal(t, GenerateReleaseName("shop", "review/pr-123"), name)
	assert.Equal(t, "review", labels["deployah.dev/environment"])
	assert.NotEqual(t, "review/pr-123", labels["deployah.dev/environment"])
	assert.NotEqual(t, "review-pr-123", labels["deployah.dev/environment"])
	require.Empty(t, validation.IsValidLabelValue(labels["deployah.dev/environment"]))
}

func TestReleaseIdentity_RejectsNil(t *testing.T) {
	t.Parallel()

	_, _, err := releaseIdentity(nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render requires resolved spec")

	_, _, err = releaseIdentity(&spec.ResolvedSpec{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "render requires resolved spec source")

	_, _, err = releaseIdentity(&spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "shop"},
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "spec.Resolve")
}

func TestRenderOffline_RejectsNilResolved(t *testing.T) {
	t.Parallel()

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)

	_, _, err = client.RenderOffline(t.Context(), nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render requires resolved spec")

	_, _, err = client.RenderOffline(t.Context(), &spec.ResolvedSpec{}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render requires resolved spec source")

	_, _, err = client.RenderOffline(t.Context(), &spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "shop"},
	}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "spec.Resolve")
}

func TestRenderOffline_ChartAndReleaseAgree(t *testing.T) {
	t.Parallel()

	m := envServiceSpec(t.TempDir(), spec.StringMap{"LOG_LEVEL": "debug"})
	resolved := resolveChart(t, m, "dev")
	require.Equal(t, "shop", resolved.Spec.Project)
	require.Equal(t, "dev", resolved.Env.Original)

	wantName, wantLabels, err := releaseIdentity(resolved)
	require.NoError(t, err)

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}

	assert.Equal(t, wantName, result.ReleaseName)
	assert.Equal(t, GenerateReleaseName(resolved.Spec.Project, resolved.Env.Original), result.ReleaseName)

	dep := findRenderedDeployment(t, result.Manifest, "-api")
	assert.Equal(t, resolved.Spec.Project, dep.Labels[spec.LabelProject])
	assert.Equal(t, resolved.Env.MapKey, dep.Labels[spec.LabelEnvironment])
	assert.Equal(t, wantLabels["deployah.dev/project"], dep.Labels[spec.LabelProject])
	assert.Equal(t, wantLabels["deployah.dev/environment"], dep.Labels[spec.LabelEnvironment])
}

func TestPrepareChart_UsesResolvedEnvironment(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{"api": serviceComponent()},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	resolved, _, err := spec.Resolve(m, nil, spec.NormalizeEnv("staging"), spec.SubstitutionReport{})
	require.NoError(t, err)

	cache := NewChartCache(time.Hour)
	chartDir, err := PrepareChart(t.Context(), resolved, cache)
	require.NoError(t, err)
	t.Cleanup(func() { removeChartDirs(t, cache, resolved, "staging", chartDir) })

	vals := readChartValues(t, chartDir)
	api := mustNestedMap(t, vals, "api")
	labels, ok := api["commonLabels"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "shop", labels[spec.LabelProject])
	assert.Equal(t, "staging", labels[spec.LabelEnvironment])
}

func TestRenderOffline_WildcardEnvironmentLabels(t *testing.T) {
	t.Parallel()

	m := envServiceSpec(t.TempDir(), spec.StringMap{"LOG_LEVEL": "debug"})
	m.Environments["review"] = spec.Environment{}
	resolved := resolveChart(t, m, "review/pr-123")
	require.Equal(t, "review/pr-123", resolved.Env.Original)
	require.Equal(t, "review", resolved.Env.MapKey)

	name, labels, err := releaseIdentity(resolved)
	require.NoError(t, err)
	assert.Equal(t, GenerateReleaseName("shop", "review/pr-123"), name)
	assert.Equal(t, "review", labels["deployah.dev/environment"])
	require.Empty(t, validation.IsValidLabelValue(labels["deployah.dev/environment"]))

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}

	assert.Equal(t, name, result.ReleaseName)
	dep := findRenderedDeployment(t, result.Manifest, "-api")
	assertLogicalEnvironmentLabel(t, dep.Labels)
	assertLogicalEnvironmentLabel(t, dep.Spec.Template.Labels)
}

func TestRenderOffline_WildcardHookJobLabels(t *testing.T) {
	t.Parallel()

	m := taskSpec()
	m.Environments = map[string]spec.Environment{"review": {}}
	job := renderHookJob(t, m, "review/pr-123", "migrate")
	assertLogicalEnvironmentLabel(t, job.Labels)
	assertLogicalEnvironmentLabel(t, job.Spec.Template.Labels)
}

func TestRenderOffline_WildcardCronJobLabels(t *testing.T) {
	t.Parallel()

	m := scheduledRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnSchedule,
		Command: []string{"cleanup"},
	})
	m.Environments["review"] = spec.Environment{}
	cj := renderScheduledCronJob(t, m, "review/pr-123", "cleanup")
	assertLogicalEnvironmentLabel(t, cj.Labels)
	assertLogicalEnvironmentLabel(t, cj.Spec.JobTemplate.Spec.Template.Labels)
}

func assertLogicalEnvironmentLabel(t *testing.T, labels map[string]string) {
	t.Helper()
	got := labels[spec.LabelEnvironment]
	assert.Equal(t, "review", got)
	assert.NotEqual(t, "review/pr-123", got)
	assert.NotEqual(t, "review-pr-123", got)
	require.Empty(t, validation.IsValidLabelValue(got))
}

func TestInstallApp_RejectsNilResolved(t *testing.T) {
	t.Parallel()

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)

	err = client.InstallApp(t.Context(), false, nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render requires resolved spec")

	err = client.InstallApp(t.Context(), false, &spec.ResolvedSpec{}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render requires resolved spec source")

	err = client.InstallApp(t.Context(), false, &spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "shop"},
	}, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "spec.Resolve")
}
