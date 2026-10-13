// Copyright 2026 The Deployah Authors
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
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/release/common"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestListReleases_LatestManagedMatchesSelector(t *testing.T) {
	t.Parallel()

	type listed struct {
		version int
		status  common.Status
	}
	tests := []struct {
		name     string
		selector string
		want     map[string]listed
	}{
		{
			name:     "project filter",
			selector: "deployah.dev/project=shop",
			want: map[string]listed{
				"shop-production": {version: 2, status: common.StatusDeployed},
				"shop-staging":    {version: 1, status: common.StatusFailed},
			},
		},
		{
			name: "managed releases only",
			want: map[string]listed{
				"shop-production":  {version: 2, status: common.StatusDeployed},
				"shop-staging":     {version: 1, status: common.StatusFailed},
				"other-production": {version: 1, status: common.StatusDeployed},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := seededReleaseList(t)
			selector := labels.NewSelector()
			if tt.selector != "" {
				parsed, err := labels.Parse(tt.selector)
				require.NoError(t, err)
				selector = parsed
			}

			got, err := c.ListReleases(t.Context(), selector)
			require.NoError(t, err)
			byName := map[string]*v1.Release{}
			for _, rel := range got {
				require.NotNil(t, rel)
				byName[rel.Name] = rel
			}
			require.Len(t, byName, len(tt.want))
			for name, want := range tt.want {
				rel, ok := byName[name]
				require.Truef(t, ok, "missing release %s", name)
				assert.Equal(t, want.version, rel.Version)
				assert.Equal(t, want.status, rel.Info.Status)
			}
			assert.NotContains(t, byName, "plain-helm")
		})
	}
}

func TestGetRelease_RejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()

	c, _ := memoryReleaseClient(t)
	_, err := c.GetRelease(t.Context(), "shop", "review/PR-1")
	require.Error(t, err)
	assert.ErrorContains(t, err, "lowercase DNS label")
}

func TestGetRelease_EmptyEnvironmentUsesReleaseName(t *testing.T) {
	t.Parallel()

	c, cfg := memoryReleaseClient(t)
	storeRelease(t, cfg, &v1.Release{
		Name:   GenerateReleaseName("shop", ""),
		Labels: managedLabels("shop", ""),
		Info:   &v1.Info{Status: common.StatusDeployed},
	})

	got, err := c.GetRelease(t.Context(), "shop", "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "shop", got.Name)
}

func TestGetRelease_NotFound(t *testing.T) {
	t.Parallel()

	c, _ := memoryReleaseClient(t)
	_, err := c.GetRelease(t.Context(), "shop", "production")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReleaseNotFound)
}

func TestGetRelease_ReturnsNewestRevision(t *testing.T) {
	t.Parallel()

	c, cfg := memoryReleaseClient(t)
	name := GenerateReleaseName("shop", "review/pr-42")
	storeRelease(t, cfg, &v1.Release{
		Name:     name,
		Version:  1,
		Manifest: "manifest: v1\n",
		Labels:   managedLabels("shop", "review"),
		Info:     &v1.Info{Status: common.StatusSuperseded},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:     name,
		Version:  2,
		Manifest: "manifest: v2\n",
		Labels:   managedLabels("shop", "review"),
		Info:     &v1.Info{Status: common.StatusFailed},
	})

	got, err := c.GetRelease(t.Context(), "shop", "review/pr-42")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, name, got.Name)
	assert.Equal(t, 2, got.Version)
	assert.Equal(t, "manifest: v2\n", got.Manifest)
	assert.Equal(t, common.StatusFailed, got.Info.Status)
}

func TestGetReleaseHistory_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		project     string
		environment string
		want        string
	}{
		{name: "empty project", environment: "production", want: "project or environment cannot be empty"},
		{name: "empty environment", project: "shop", want: "project or environment cannot be empty"},
		{name: "invalid environment", project: "shop", environment: "review/PR-1", want: "lowercase DNS label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := memoryReleaseClient(t)
			_, err := c.GetReleaseHistory(t.Context(), tt.project, tt.environment)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestGetReleaseHistory_NotFound(t *testing.T) {
	t.Parallel()

	c, _ := memoryReleaseClient(t)
	_, err := c.GetReleaseHistory(t.Context(), "shop", "production")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReleaseNotFound)
}

func TestGetReleaseHistory_ReturnsEveryRevision(t *testing.T) {
	t.Parallel()

	c, cfg := memoryReleaseClient(t)
	name := GenerateReleaseName("shop", "production")
	storeRelease(t, cfg, &v1.Release{
		Name:     name,
		Version:  1,
		Manifest: "manifest: v1\n",
		Info:     &v1.Info{Status: common.StatusSuperseded},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:     name,
		Version:  2,
		Manifest: "manifest: v2\n",
		Info:     &v1.Info{Status: common.StatusDeployed},
	})

	got, err := c.GetReleaseHistory(t.Context(), "shop", "production")
	require.NoError(t, err)
	require.Len(t, got, 2)
	byVersion := map[int]string{}
	for _, rel := range got {
		require.NotNil(t, rel)
		byVersion[rel.Version] = rel.Manifest
	}
	assert.Equal(t, map[int]string{1: "manifest: v1\n", 2: "manifest: v2\n"}, byVersion)
}

func TestDeleteRelease_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		project     string
		environment string
		want        string
	}{
		{name: "empty project", environment: "production", want: "project or environment cannot be empty"},
		{name: "empty environment", project: "shop", want: "project or environment cannot be empty"},
		{name: "invalid environment", project: "shop", environment: "review/PR-1", want: "lowercase DNS label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := memoryReleaseClient(t)
			err := c.DeleteRelease(t.Context(), tt.project, tt.environment, false)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestDeleteRelease_MissingReleaseSucceeds(t *testing.T) {
	t.Parallel()

	c, cfg := memoryReleaseClient(t)
	require.NoError(t, c.DeleteRelease(t.Context(), "shop", "production", false))
	_, err := cfg.Releases.History(GenerateReleaseName("shop", "production"))
	assert.ErrorIs(t, err, driver.ErrReleaseNotFound)
}

func TestDeleteRelease_WaitStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		wait            bool
		wantStrategy    kube.WaitStrategy
		wantPropagation metav1.DeletionPropagation
	}{
		{
			name:            "return after hooks",
			wait:            false,
			wantStrategy:    kube.HookOnlyStrategy,
			wantPropagation: metav1.DeletePropagationBackground,
		},
		{
			name:            "wait for foreground deletion",
			wait:            true,
			wantStrategy:    kube.LegacyStrategy,
			wantPropagation: metav1.DeletePropagationForeground,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, cfg, kubeClient := recordingReleaseClient(t)
			name := GenerateReleaseName("shop", "production")
			storeRelease(t, cfg, &v1.Release{
				Name:     name,
				Version:  1,
				Manifest: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n",
				Info:     &v1.Info{Status: common.StatusDeployed},
			})

			require.NoError(t, c.DeleteRelease(t.Context(), "shop", "production", tt.wait))
			assert.Equal(t, tt.wantStrategy, kubeClient.strategy)
			assert.Equal(t, tt.wantPropagation, kubeClient.propagation)
			_, err := c.GetRelease(t.Context(), "shop", "production")
			assert.ErrorIs(t, err, ErrReleaseNotFound)
		})
	}
}

func TestRollbackRelease_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	c, _ := memoryReleaseClient(t)
	err := c.RollbackRelease(t.Context(), "", 1, time.Minute)
	require.EqualError(t, err, "release name cannot be empty")
}

func TestRollbackRelease_NotFound(t *testing.T) {
	t.Parallel()

	c, _ := memoryReleaseClient(t)
	err := c.RollbackRelease(t.Context(), "shop-production", 1, time.Minute)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReleaseNotFound)
}

func TestRollbackRelease_MissingRevisionLeavesHistory(t *testing.T) {
	t.Parallel()

	c, cfg := memoryReleaseClient(t)
	storeRelease(t, cfg, &v1.Release{
		Name:    "shop-production",
		Version: 1,
		Info:    &v1.Info{Status: common.StatusDeployed},
	})

	err := c.RollbackRelease(t.Context(), "shop-production", 9, time.Minute)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no 9 version")
	assert.Equal(t, 1, historyLen(t, cfg, "shop-production"))
}

func TestRollbackRelease_RestoresTargetManifest(t *testing.T) {
	t.Parallel()

	c, cfg := memoryReleaseClient(t)
	const name = "shop-production"
	storeRelease(t, cfg, &v1.Release{
		Name:     name,
		Version:  1,
		Manifest: "manifest: v1\n",
		Info:     &v1.Info{Status: common.StatusSuperseded},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:     name,
		Version:  2,
		Manifest: "manifest: v2\n",
		Info:     &v1.Info{Status: common.StatusDeployed},
	})

	require.NoError(t, c.RollbackRelease(t.Context(), name, 1, time.Minute))

	last, err := cfg.Releases.Last(name)
	require.NoError(t, err)
	got, err := releaserToV1(last)
	require.NoError(t, err)
	assert.Equal(t, 3, got.Version)
	assert.Equal(t, "manifest: v1\n", got.Manifest)
	assert.Equal(t, common.StatusDeployed, got.Info.Status)

	history, err := c.GetReleaseHistory(t.Context(), "shop", "production")
	require.NoError(t, err)
	require.Len(t, history, 3)
	for _, rel := range history {
		if rel.Version == 2 {
			assert.Equal(t, common.StatusSuperseded, rel.Info.Status)
		}
	}
}

func TestReleaseOperations_UnreachableCluster(t *testing.T) {
	t.Parallel()

	cause := errors.New("api server unreachable")

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		c := unreachableReleaseClient(t, cause)
		_, err := c.ListReleases(t.Context(), labels.NewSelector())
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, ErrReleaseNotFound)
		assert.ErrorContains(t, err, "helm list failed")
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		c := unreachableReleaseClient(t, cause)
		_, err := c.GetRelease(t.Context(), "shop", "production")
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, ErrReleaseNotFound)
		assert.ErrorContains(t, err, "helm get failed")
	})

	t.Run("history", func(t *testing.T) {
		t.Parallel()
		c := unreachableReleaseClient(t, cause)
		_, err := c.GetReleaseHistory(t.Context(), "shop", "production")
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, ErrReleaseNotFound)
		assert.ErrorContains(t, err, "helm history failed")
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		c := unreachableReleaseClient(t, cause)
		err := c.DeleteRelease(t.Context(), "shop", "production", false)
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, ErrReleaseNotFound)
		assert.ErrorContains(t, err, "helm uninstall failed")
	})

	t.Run("rollback", func(t *testing.T) {
		t.Parallel()
		c := unreachableReleaseClient(t, cause)
		err := c.RollbackRelease(t.Context(), "shop-production", 1, time.Minute)
		require.Error(t, err)
		assert.ErrorIs(t, err, cause)
		assert.NotErrorIs(t, err, ErrReleaseNotFound)
		assert.ErrorContains(t, err, "helm rollback failed")
	})
}

func memoryReleaseClient(t *testing.T) (*Client, *action.Configuration) {
	t.Helper()
	cfg := &action.Configuration{
		Releases:   storage.Init(driver.NewMemory()),
		KubeClient: &kubefake.PrintingKubeClient{Out: io.Discard},
	}
	return &Client{
		config:     cfg,
		namespace:  "default",
		timeout:    time.Minute,
		chartCache: NewChartCache(time.Hour),
	}, cfg
}

func unreachableReleaseClient(t *testing.T, cause error) *Client {
	t.Helper()
	c, cfg := memoryReleaseClient(t)
	failing := &kubefake.FailingKubeClient{ConnectionError: cause}
	failing.Out = io.Discard
	cfg.KubeClient = failing
	return c
}

type recordingKube struct {
	*kubefake.FailingKubeClient
	strategy    kube.WaitStrategy
	propagation metav1.DeletionPropagation
}

func (r *recordingKube) GetWaiter(ws kube.WaitStrategy) (kube.Waiter, error) {
	r.strategy = ws
	return r.FailingKubeClient.GetWaiter(ws)
}

func (r *recordingKube) GetWaiterWithOptions(ws kube.WaitStrategy, opts ...kube.WaitOption) (kube.Waiter, error) {
	r.strategy = ws
	return r.FailingKubeClient.GetWaiterWithOptions(ws, opts...)
}

func (r *recordingKube) Delete(resources kube.ResourceList, propagation metav1.DeletionPropagation) (*kube.Result, []error) {
	r.propagation = propagation
	return r.FailingKubeClient.Delete(resources, propagation)
}

func recordingReleaseClient(t *testing.T) (*Client, *action.Configuration, *recordingKube) {
	t.Helper()
	c, cfg := memoryReleaseClient(t)
	// Mapping is set because Helm logs the kind while deleting.
	// Client stays nil so the ownership check treats the object as
	// part of this release.
	var resources kube.ResourceList
	resources.Append(&resource.Info{
		Name:      "app",
		Namespace: "default",
		Mapping: &meta.RESTMapping{
			Resource:         schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			GroupVersionKind: schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
			Scope:            meta.RESTScopeNamespace,
		},
	})
	failing := &kubefake.FailingKubeClient{DummyResources: resources}
	failing.Out = io.Discard
	kubeClient := &recordingKube{FailingKubeClient: failing}
	cfg.KubeClient = kubeClient
	return c, cfg, kubeClient
}

func seededReleaseList(t *testing.T) *Client {
	t.Helper()
	c, cfg := memoryReleaseClient(t)
	storeRelease(t, cfg, &v1.Release{
		Name:    "shop-production",
		Version: 1,
		Labels:  managedLabels("shop", "production"),
		Info:    &v1.Info{Status: common.StatusSuperseded},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:    "shop-production",
		Version: 2,
		Labels:  managedLabels("shop", "production"),
		Info:    &v1.Info{Status: common.StatusDeployed},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:    "shop-staging",
		Version: 1,
		Labels:  managedLabels("shop", "staging"),
		Info:    &v1.Info{Status: common.StatusFailed},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:    "other-production",
		Version: 1,
		Labels:  managedLabels("other", "production"),
		Info:    &v1.Info{Status: common.StatusDeployed},
	})
	storeRelease(t, cfg, &v1.Release{
		Name:    "plain-helm",
		Version: 1,
		Labels:  map[string]string{"managed-by": "helm"},
		Info:    &v1.Info{Status: common.StatusDeployed},
	})
	return c
}

func managedLabels(project, environment string) map[string]string {
	return map[string]string{
		"deployah.dev/project":     project,
		"deployah.dev/environment": environment,
		"deployah.dev/managed-by":  "deployah",
	}
}

func storeRelease(t *testing.T, cfg *action.Configuration, rel *v1.Release) {
	t.Helper()
	if rel.Info == nil {
		rel.Info = &v1.Info{}
	}
	if rel.Info.Status == "" {
		rel.Info.Status = common.StatusDeployed
	}
	if rel.Info.FirstDeployed.IsZero() {
		rel.Info.FirstDeployed = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		rel.Info.LastDeployed = rel.Info.FirstDeployed
	}
	if rel.Chart == nil {
		rel.Chart = &chart.Chart{
			Metadata: &chart.Metadata{APIVersion: "v2", Name: "app", Version: "0.1.0"},
		}
	}
	if rel.Namespace == "" {
		rel.Namespace = "default"
	}
	require.NoError(t, cfg.Releases.Create(rel))
}
