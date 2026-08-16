// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package delete

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cli"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestBuildPreview_LeftoverJobsWithoutRelease(t *testing.T) {
	t.Parallel()

	p := buildPreview("shop", "dev", nil, []string{"shop-dev-backfill-abc"}, false)
	assert.Equal(t, "shop", p.Project)
	assert.Equal(t, "dev", p.Environment)
	assert.Empty(t, p.Release)
	assert.Equal(t, "not found", p.Status)
	assert.Equal(t, []string{"shop-dev-backfill-abc"}, p.Jobs)
}

func TestBuildPreview_ReleaseAndJobs(t *testing.T) {
	t.Parallel()

	rel := &v1.Release{Name: "shop-dev", Namespace: "default", Version: 3}
	p := buildPreview("shop", "dev", rel, []string{"shop-dev-backfill-abc", "shop-dev-migrate-xyz"}, false)
	assert.Equal(t, "shop-dev", p.Release)
	assert.Equal(t, "default", p.Namespace)
	assert.Equal(t, 3, p.Revision)
	assert.Equal(t, []string{"shop-dev-backfill-abc", "shop-dev-migrate-xyz"}, p.Jobs)
}

func TestNothingToDelete(t *testing.T) {
	t.Parallel()

	require.True(t, nothingToDelete(nil, nil))
	require.True(t, nothingToDelete(nil, []string{}))
	require.False(t, nothingToDelete(nil, []string{"job"}))
	require.False(t, nothingToDelete(&v1.Release{Name: "shop-dev"}, nil))
}

func TestBuildPreview_ReleaseInfoAndResources(t *testing.T) {
	t.Parallel()

	deployed := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	rel := &v1.Release{
		Name:      "shop-dev",
		Namespace: "default",
		Version:   2,
		Info: &v1.Info{
			Status:       common.StatusDeployed,
			LastDeployed: deployed,
		},
		Manifest: strings.Join([]string{
			"apiVersion: apps/v1",
			"kind: Deployment",
			"metadata:",
			"  name: shop-dev-api",
			"spec:",
			"  replicas: 2",
		}, "\n"),
	}
	p := buildPreview("shop", "dev", rel, nil, true)
	assert.Equal(t, "deployed", p.Status)
	assert.Equal(t, "2026-08-16 12:00:00 UTC", p.LastDeployed)
	require.Len(t, p.Resources, 1)
	assert.Equal(t, "Deployment", p.Resources[0].Kind)
	assert.Equal(t, "shop-dev-api", p.Resources[0].Name)
	assert.Equal(t, "replicas: 2", p.Resources[0].Detail)
}

func TestParseResources(t *testing.T) {
	t.Parallel()

	manifest := strings.Join([]string{
		"",
		"---",
		"not: valid yaml: [",
		"---",
		"apiVersion: v1",
		"kind: ConfigMap",
		"metadata:",
		"  name: shop-config",
		"---",
		"apiVersion: apps/v1",
		"kind: Deployment",
		"metadata:",
		"  name: api",
		"spec:",
		"  replicas: 3",
		"---",
		"apiVersion: v1",
		"kind: Service",
		"metadata:",
		"  name: api",
		"spec:",
		"  type: ClusterIP",
		"  ports:",
		"    - port: 8080",
		"---",
		"apiVersion: v1",
		"kind: Service",
		"metadata:",
		"  name: bare",
		"spec: {}",
		"---",
		"apiVersion: networking.k8s.io/v1",
		"kind: Ingress",
		"metadata:",
		"  name: api",
		"spec:",
		"  rules:",
		"    - host: api.example.com",
		"---",
		"apiVersion: v1",
		"kind: Secret",
		"metadata:",
		"  name: tls",
		"type: kubernetes.io/tls",
		"---",
		"apiVersion: v1",
		"kind: Secret",
		"metadata:",
		"  name: env",
		"---",
		"apiVersion: v1",
		"kind: PersistentVolumeClaim",
		"metadata:",
		"  name: data",
		"spec:",
		"  resources:",
		"    requests:",
		"      storage: 10Gi",
		"---",
		"apiVersion: apps/v1",
		"kind: StatefulSet",
		"metadata:",
		"  name: db",
		"spec:",
		"  replicas: 1",
	}, "\n")

	got := parseResources(manifest)
	require.Len(t, got, 9)
	assert.Equal(t, ResourceInfo{APIVersion: "v1", Kind: "ConfigMap", Name: "shop-config"}, got[0])
	assert.Equal(t, "replicas: 3", got[1].Detail)
	assert.Equal(t, "ClusterIP, port: 8080", got[2].Detail)
	assert.Equal(t, "ClusterIP", got[3].Detail)
	assert.Equal(t, "host: api.example.com", got[4].Detail)
	assert.Equal(t, "kubernetes.io/tls", got[5].Detail)
	assert.Equal(t, "Opaque", got[6].Detail)
	assert.Equal(t, "storage: 10Gi", got[7].Detail)
	assert.Equal(t, "replicas: 1", got[8].Detail)
}

func TestRenderDryRunPreview(t *testing.T) {
	t.Parallel()

	t.Run("nothing to delete", func(t *testing.T) {
		t.Parallel()
		c := nabatContext(t)
		require.NoError(t, renderDryRunPreview(c, "shop", "dev", nil, nil, false, cli.OutputFormatTree))
	})

	t.Run("leftover jobs tree", func(t *testing.T) {
		t.Parallel()
		c := nabatContext(t)
		require.NoError(t, renderDryRunPreview(c, "shop", "dev", nil, []string{"shop-dev-backfill-abc"}, false, cli.OutputFormatTree))
	})

	t.Run("release with resources", func(t *testing.T) {
		t.Parallel()
		c := nabatContext(t)
		rel := &v1.Release{
			Name:      "shop-dev",
			Namespace: "default",
			Version:   1,
			Manifest: strings.Join([]string{
				"apiVersion: apps/v1",
				"kind: Deployment",
				"metadata:",
				"  name: api",
				"spec:",
				"  replicas: 1",
				"---",
				"apiVersion: v1",
				"kind: Service",
				"metadata:",
				"  name: api",
				"spec:",
				"  ports:",
				"    - port: 80",
			}, "\n"),
		}
		require.NoError(t, renderDryRunPreview(c, "shop", "dev", rel, []string{"shop-dev-migrate-xyz"}, true, cli.OutputFormatTree))
	})

	t.Run("json leftover jobs", func(t *testing.T) {
		t.Parallel()
		c := nabatContext(t)
		require.NoError(t, renderDryRunPreview(c, "shop", "dev", nil, []string{"job-a"}, false, cli.OutputFormatJSON))
	})

	t.Run("yaml leftover jobs", func(t *testing.T) {
		t.Parallel()
		c := nabatContext(t)
		require.NoError(t, renderDryRunPreview(c, "shop", "dev", nil, []string{"job-a"}, false, cli.OutputFormatYAML))
	})
}

func nabatContext(t *testing.T) *nabat.Context {
	t.Helper()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	return nabattest.Context(t, app)
}

func TestDeleteConfirmPrompt(t *testing.T) {
	t.Parallel()

	rel := &v1.Release{Name: "shop-dev"}
	tests := []struct {
		name        string
		project     string
		environment string
		targetCtx   string
		release     *v1.Release
		jobs        []string
		want        string
	}{
		{
			name:        "release without context",
			project:     "shop",
			environment: "dev",
			release:     rel,
			want:        "Delete project 'shop' in environment 'dev'?",
		},
		{
			name:        "release with context",
			project:     "shop",
			environment: "dev",
			targetCtx:   "kind-deployah",
			release:     rel,
			jobs:        []string{"shop-dev-backfill-abc"},
			want:        "Delete project 'shop' in environment 'dev' (context: kind-deployah)?",
		},
		{
			name:        "leftover jobs only",
			project:     "shop",
			environment: "dev",
			jobs:        []string{"shop-dev-backfill-abc", "shop-dev-migrate-xyz"},
			want:        "Delete leftover Jobs for project 'shop' in environment 'dev': shop-dev-backfill-abc, shop-dev-migrate-xyz?",
		},
		{
			name:        "leftover jobs with context",
			project:     "shop",
			environment: "dev",
			targetCtx:   "kind-deployah",
			jobs:        []string{"shop-dev-backfill-abc"},
			want:        "Delete leftover Jobs for project 'shop' in environment 'dev': shop-dev-backfill-abc (context: kind-deployah)?",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deleteConfirmPrompt(tt.project, tt.environment, tt.targetCtx, tt.release, tt.jobs)
			assert.Equal(t, tt.want, got)
		})
	}
}
