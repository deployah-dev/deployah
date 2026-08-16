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
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/release/common"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/spec"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestWrapHelmError_TypedClassification verifies typed Helm and Kubernetes
// errors map to the expected sentinels and user-facing messages.
func TestWrapHelmError_TypedClassification(t *testing.T) {
	t.Parallel()
	c := &Client{}

	tests := []struct {
		name    string
		err     error
		wantIs  error
		wantMsg string
	}{
		{
			name:   "helm driver not found",
			err:    driver.ErrReleaseNotFound,
			wantIs: ErrReleaseNotFound,
		},
		{
			name:   "helm driver already exists",
			err:    driver.ErrReleaseExists,
			wantIs: ErrReleaseAlreadyExists,
		},
		{
			name: "k8s not found",
			err: apierrors.NewNotFound(
				schema.GroupResource{Resource: "secrets"}, "myrel.v1",
			),
			wantIs: ErrReleaseNotFound,
		},
		{
			name: "k8s already exists",
			err: apierrors.NewAlreadyExists(
				schema.GroupResource{Resource: "secrets"}, "myrel.v1",
			),
			wantIs: ErrReleaseAlreadyExists,
		},
		{
			name: "k8s forbidden",
			err: apierrors.NewForbidden(
				schema.GroupResource{Resource: "secrets"}, "myrel.v1",
				errors.New("denied"),
			),
			wantMsg: "insufficient permissions",
		},
		{
			name:    "k8s unauthorized",
			err:     apierrors.NewUnauthorized("bad token"),
			wantMsg: "insufficient permissions",
		},
		{
			name:    "k8s timeout",
			err:     apierrors.NewTimeoutError("slow", 1),
			wantMsg: "operation timed out",
		},
		{
			name: "net op error",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("connection refused"),
			},
			wantMsg: "unable to connect to Kubernetes cluster",
		},
		{
			name:   "helm not found string",
			err:    errors.New("release: not found"),
			wantIs: ErrReleaseNotFound,
		},
		{
			name:    "helm connection refused string",
			err:     errors.New("connection refused"),
			wantMsg: "unable to connect to Kubernetes cluster",
		},
		{
			name:   "helm already exists string",
			err:    errors.New("cannot re-use a name that is still in use: already exists"),
			wantIs: ErrReleaseAlreadyExists,
		},
		{
			// Pending is handled by InstallApp's typed Status.IsPending
			// pre-check, not by string matching in wrapHelmError.
			name:    "helm pending string falls through",
			err:     errors.New("another operation (install/upgrade/rollback) is in progress"),
			wantMsg: "helm upgrade failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := c.wrapHelmError("upgrade", "app-prod", tt.err)
			require.Error(t, got)
			if tt.wantIs != nil {
				assert.ErrorIs(t, got, tt.wantIs)
			}
			if tt.wantMsg != "" {
				assert.ErrorContains(t, got, tt.wantMsg)
			}
			// Original error remains inspectable through the wrap.
			assert.ErrorIs(t, got, tt.err)
		})
	}
}

// TestWrapHelmError_PreservesWrappedCause verifies classification sees through
// a wrapping layer to the underlying Kubernetes API error.
func TestWrapHelmError_PreservesWrappedCause(t *testing.T) {
	t.Parallel()
	c := &Client{}
	cause := apierrors.NewForbidden(
		schema.GroupResource{Resource: "secrets"}, "x", errors.New("no"),
	)
	wrapped := fmt.Errorf("helm failed: %w", cause)
	got := c.wrapHelmError("install", "app-prod", wrapped)
	assert.ErrorIs(t, got, cause)
	assert.ErrorContains(t, got, "insufficient permissions")
}

// TestNewestRelease_SortsByVersion verifies newestRelease does not trust
// Helm's unsorted history list order.
func TestNewestRelease_SortsByVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []*v1.Release
		want int
	}{
		{name: "empty", in: nil, want: 0},
		{
			name: "unsorted",
			in: []*v1.Release{
				{Version: 1},
				{Version: 3},
				{Version: 2},
			},
			want: 3,
		},
		{
			name: "already newest first",
			in: []*v1.Release{
				{Version: 5},
				{Version: 1},
			},
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := newestRelease(tt.in)
			if tt.want == 0 {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Version)
		})
	}
}

// TestInstallApp_PendingReleaseRejects pins the typed pending pre-check: a
// release whose newest revision is pending must fail with
// [ErrReleasePending] before Helm upgrade runs.
func TestInstallApp_PendingReleaseRejects(t *testing.T) {
	t.Parallel()

	cfg := &action.Configuration{
		Releases:   storage.Init(driver.NewMemory()),
		KubeClient: &kubefake.PrintingKubeClient{Out: io.Discard},
	}
	settings := cli.New()
	settings.SetNamespace("default")

	c := &Client{
		config:     cfg,
		settings:   settings,
		timeout:    time.Minute,
		chartCache: NewChartCache(time.Hour),
	}

	manifest := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "pending-app",
		Components: map[string]spec.Component{"web": serviceComponent()},
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	releaseName := GenerateReleaseName(manifest.Project, "production")

	now := time.Now()
	ch := &chart.Chart{
		Metadata: &chart.Metadata{
			APIVersion: "v2",
			Name:       "hello",
			Version:    "0.1.0",
		},
		Templates: []*chartcommon.File{
			{Name: "templates/hello", Data: []byte("hello: world")},
		},
	}
	require.NoError(t, cfg.Releases.Create(&v1.Release{
		Name: releaseName,
		Info: &v1.Info{
			FirstDeployed: now,
			LastDeployed:  now,
			Status:        common.StatusDeployed,
			Description:   "deployed",
		},
		Chart:     ch,
		Version:   1,
		Namespace: "default",
	}))
	require.NoError(t, cfg.Releases.Create(&v1.Release{
		Name: releaseName,
		Info: &v1.Info{
			FirstDeployed: now,
			LastDeployed:  now,
			Status:        common.StatusPendingUpgrade,
			Description:   "preparing upgrade",
		},
		Chart:     ch,
		Version:   2,
		Namespace: "default",
	}))

	err := c.InstallApp(t.Context(), manifest, "production", false, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReleasePending)
}
