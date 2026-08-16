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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
