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
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/release/common"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"

	"deployah.dev/deployah/internal/spec"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestNewInstallAction_ServerSideApply(t *testing.T) {
	t.Parallel()

	c := &Client{
		config:    new(action.Configuration),
		namespace: "default",
		timeout:   time.Minute,
	}
	got := c.newInstallAction("web-production", nil, nil, false)
	assert.True(t, got.ServerSideApply)
	assert.False(t, got.ForceConflicts)
	assert.False(t, got.TakeOwnership)
	assert.False(t, got.SkipCRDs)
}

func TestNewInstallAction_SkipCRDs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		skip bool
	}{
		{name: "skip false"},
		{name: "skip true", skip: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{
				config:    new(action.Configuration),
				namespace: "default",
				timeout:   time.Minute,
			}
			got := c.newInstallAction("web-production", nil, nil, tc.skip)
			assert.Equal(t, tc.skip, got.SkipCRDs)
		})
	}
}

func TestNewUpgradeAction_ServerSideApply(t *testing.T) {
	t.Parallel()

	c := &Client{
		config:    new(action.Configuration),
		namespace: "default",
		timeout:   time.Minute,
	}
	got := c.newUpgradeAction(nil, nil)
	assert.Equal(t, "true", got.ServerSideApply)
	assert.False(t, got.ForceConflicts)
	assert.False(t, got.TakeOwnership)
	assert.False(t, got.SkipCRDs)
}

func TestInstallApp_RejectsUnsupportedCSAHistory(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "csa-app")
	seedRelease(t, cfg, releaseName, 1, common.StatusDeployed, applyCSA)

	err := c.InstallApp(t.Context(), false, resolved, nil, nil, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedReleaseApplyMethod)
	assert.Equal(t, 1, historyLen(t, cfg, releaseName))
}

func TestRenderManifests_RejectsUnsupportedCSAHistory(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "csa-render")
	seedRelease(t, cfg, releaseName, 1, common.StatusDeployed, applyCSA)

	_, cleanup, err := c.RenderManifests(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedReleaseApplyMethod)
}

func TestInstallApp_NewestSSACurrentCSAIsRejected(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "mixed-app")
	seedRelease(t, cfg, releaseName, 1, common.StatusDeployed, applyCSA)
	seedRelease(t, cfg, releaseName, 2, common.StatusFailed, applySSA)

	err := c.InstallApp(t.Context(), false, resolved, nil, nil, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedReleaseApplyMethod)
	assert.Equal(t, 2, historyLen(t, cfg, releaseName))
}

func TestRenderManifests_NewestSSACurrentCSAIsRejected(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "mixed-render")
	seedRelease(t, cfg, releaseName, 1, common.StatusDeployed, applyCSA)
	seedRelease(t, cfg, releaseName, 2, common.StatusFailed, applySSA)

	_, cleanup, err := c.RenderManifests(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedReleaseApplyMethod)
}

func TestInstallApp_HistoryErrorIsNotInstall(t *testing.T) {
	t.Parallel()

	histErr := errors.New("secrets list forbidden")
	c, cfg, resolved, releaseName := memoryHelmApp(t, "rbac-app")
	failing := &kubefake.FailingKubeClient{ConnectionError: histErr}
	failing.Out = io.Discard
	cfg.KubeClient = failing

	err := c.InstallApp(t.Context(), false, resolved, nil, nil, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, histErr)
	assert.NotErrorIs(t, err, ErrUnsupportedReleaseApplyMethod)
	_, histLookupErr := cfg.Releases.History(releaseName)
	assert.ErrorIs(t, histLookupErr, driver.ErrReleaseNotFound)
}

func TestRenderManifests_HistoryErrorIsNotInstall(t *testing.T) {
	t.Parallel()

	histErr := errors.New("secrets list forbidden")
	c, cfg, resolved, releaseName := memoryHelmApp(t, "rbac-render")
	failing := &kubefake.FailingKubeClient{ConnectionError: histErr}
	failing.Out = io.Discard
	cfg.KubeClient = failing

	_, cleanup, err := c.RenderManifests(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, histErr)
	_, histLookupErr := cfg.Releases.History(releaseName)
	assert.ErrorIs(t, histLookupErr, driver.ErrReleaseNotFound)
}

func TestInstallApp_MissingReleaseIsInstall(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "fresh-app")

	require.NoError(t, c.InstallApp(t.Context(), false, resolved, nil, nil, false))
	assert.Equal(t, 1, historyLen(t, cfg, releaseName))
	rel, err := cfg.Releases.Last(releaseName)
	require.NoError(t, err)
	v1rel, err := releaserToV1(rel)
	require.NoError(t, err)
	assert.Equal(t, applySSA, v1rel.ApplyMethod)
}

func TestRenderManifests_MissingReleaseIsInstall(t *testing.T) {
	t.Parallel()

	c, _, resolved, _ := memoryHelmApp(t, "fresh-render")

	result, cleanup, err := c.RenderManifests(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsUpgrade)
	assert.Equal(t, 1, result.Revision)
}

func TestRenderManifestsWithPrep_FreshInstall(t *testing.T) {
	t.Parallel()

	c, _, resolved, _ := memoryHelmApp(t, "fresh-prep")
	result, prep, cleanup, err := c.RenderManifestsWithPrep(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, OperationInstall, prep.Operation)
	assert.Nil(t, prep.Current)
	assert.Equal(t, 1, prep.NextRevision)
	assert.False(t, result.IsUpgrade)
	assert.Equal(t, 1, result.Revision)
}

func TestRenderManifestsWithPrep_Upgrade(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "upgrade-prep")
	seedRelease(t, cfg, releaseName, 2, common.StatusDeployed, applySSA)

	result, prep, cleanup, err := c.RenderManifestsWithPrep(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, prep.Current)
	assert.Equal(t, OperationUpgrade, prep.Operation)
	assert.Equal(t, 2, prep.Current.Version)
	assert.Equal(t, 3, prep.NextRevision)
	assert.True(t, result.IsUpgrade)
	assert.Equal(t, 3, result.Revision)
}

func TestRenderManifestsWithPrep_NewestDiffersFromCurrent(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "newest-current")
	seedRelease(t, cfg, releaseName, 3, common.StatusDeployed, applySSA)
	seedRelease(t, cfg, releaseName, 4, common.StatusFailed, applySSA)

	result, prep, cleanup, err := c.RenderManifestsWithPrep(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, prep.Newest)
	require.NotNil(t, prep.Current)
	assert.Equal(t, OperationUpgrade, prep.Operation)
	assert.Equal(t, 4, prep.Newest.Version)
	assert.Equal(t, 3, prep.Current.Version)
	assert.Equal(t, 5, prep.NextRevision)
	assert.True(t, result.IsUpgrade)
	assert.Equal(t, 5, result.Revision)
}

func TestRenderManifests_MatchesWithPrepResult(t *testing.T) {
	t.Parallel()

	c, cfg, resolved, releaseName := memoryHelmApp(t, "compat-render")
	seedRelease(t, cfg, releaseName, 1, common.StatusDeployed, applySSA)

	withPrep, prep, cleanup, err := c.RenderManifestsWithPrep(t.Context(), resolved, nil, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)

	wrapped, wrappedCleanup, err := c.RenderManifests(t.Context(), resolved, nil, nil)
	if wrappedCleanup != nil {
		t.Cleanup(wrappedCleanup)
	}
	require.NoError(t, err)
	require.NotNil(t, withPrep)
	require.NotNil(t, wrapped)
	assert.Equal(t, OperationUpgrade, prep.Operation)
	assert.Equal(t, withPrep.IsUpgrade, wrapped.IsUpgrade)
	assert.Equal(t, withPrep.Revision, wrapped.Revision)
	assert.Equal(t, withPrep.ReleaseName, wrapped.ReleaseName)
	assert.Equal(t, withPrep.Namespace, wrapped.Namespace)
}

func memoryHelmApp(t *testing.T, project string) (*Client, *action.Configuration, *spec.ResolvedSpec, string) {
	t.Helper()

	cfg := &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   &kubefake.PrintingKubeClient{Out: io.Discard},
		Capabilities: chartcommon.DefaultCapabilities.Copy(),
	}
	c := &Client{
		config:     cfg,
		namespace:  "default",
		timeout:    time.Minute,
		chartCache: NewChartCache(time.Hour),
	}

	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    project,
		Components: map[string]spec.Component{"web": serviceComponent()},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	resolved, _, err := spec.Resolve(manifest, nil, spec.NormalizeEnv("production"), spec.SubstitutionReport{})
	require.NoError(t, err)
	return c, cfg, resolved, GenerateReleaseName(project, "production")
}

func seedRelease(t *testing.T, cfg *action.Configuration, name string, version int, status common.Status, applyMethod string) {
	t.Helper()

	now := time.Now()
	require.NoError(t, cfg.Releases.Create(&v1.Release{
		Name: name,
		Info: &v1.Info{
			FirstDeployed: now,
			LastDeployed:  now,
			Status:        status,
			Description:   string(status),
		},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{
				APIVersion: "v2",
				Name:       "hello",
				Version:    "0.1.0",
			},
			Templates: []*chartcommon.File{
				{Name: "templates/hello", Data: []byte("hello: world")},
			},
		},
		Version:     version,
		Namespace:   "default",
		ApplyMethod: applyMethod,
	}))
}

func historyLen(t *testing.T, cfg *action.Configuration, name string) int {
	t.Helper()
	rels, err := cfg.Releases.History(name)
	require.NoError(t, err)
	return len(rels)
}
