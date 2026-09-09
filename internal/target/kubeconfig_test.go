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

package target

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

func TestSnapshotLoading_RetainsDefaultLoadingRules(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	t.Setenv("HOME", t.TempDir())

	want := clientcmd.NewDefaultClientConfigLoadingRules()
	require.NotEmpty(t, want.MigrationRules)

	got := snapshotLoading(Config{}).clientConfigLoadingRules()
	assert.Empty(t, got.ExplicitPath)
	assert.Equal(t, want.Precedence, got.Precedence)
	assert.Equal(t, want.MigrationRules, got.MigrationRules)
	assert.Equal(t, want.WarnIfAllMissing, got.WarnIfAllMissing)
	assert.Equal(t, want.DoNotResolvePaths, got.DoNotResolvePaths)
}

func TestSnapshotLoading_ExplicitPathKeepsDefaultSemantics(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	t.Setenv("HOME", t.TempDir())

	explicit := filepath.Join(t.TempDir(), "explicit")
	extra := filepath.Join(t.TempDir(), "extra")
	want := clientcmd.NewDefaultClientConfigLoadingRules()
	require.NotEmpty(t, want.MigrationRules)

	got := snapshotLoading(Config{
		KubeconfigPath:       explicit,
		ExtraKubeconfigPaths: []string{extra},
	}).clientConfigLoadingRules()

	assert.Equal(t, explicit, got.ExplicitPath)
	assert.Equal(t, want.Precedence, got.Precedence)
	assert.NotContains(t, got.Precedence, extra)
	assert.Equal(t, want.MigrationRules, got.MigrationRules)
	assert.Equal(t, want.WarnIfAllMissing, got.WarnIfAllMissing)
	assert.Equal(t, want.DoNotResolvePaths, got.DoNotResolvePaths)
}

func TestSnapshotLoading_ReconstructedRulesAreIndependent(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	t.Setenv("HOME", t.TempDir())

	snap := snapshotLoading(Config{})
	require.NotEmpty(t, snap.rules.MigrationRules)
	require.NotEmpty(t, snap.rules.Precedence)

	got := snap.clientConfigLoadingRules()
	got.Precedence[0] = filepath.Join(t.TempDir(), "mutated")
	for dest := range got.MigrationRules {
		got.MigrationRules[dest] = filepath.Join(t.TempDir(), "mutated-source")
	}

	again := snap.clientConfigLoadingRules()
	assert.NotEqual(t, got.Precedence, again.Precedence)
	assert.Equal(t, snap.rules.MigrationRules, again.MigrationRules)
	assert.NotEqual(t, got.MigrationRules, again.MigrationRules)
}
