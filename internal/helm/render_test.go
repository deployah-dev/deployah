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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
)

// newTestConfiguration builds an *action.Configuration with an in-memory
// release store and a distinguishable "real" KubeClient/Capabilities pair,
// so restore behavior can be verified without a real cluster.
func newTestConfiguration(t *testing.T) *action.Configuration {
	t.Helper()
	cfg := &action.Configuration{
		KubeClient:   &kubefake.FailingKubeClient{}, // "real" client sentinel
		Releases:     storage.Init(driver.NewMemory()),
		Capabilities: &chartcommon.Capabilities{KubeVersion: chartcommon.KubeVersion{Version: "v1.30.0"}},
	}
	cfg.Releases.MaxHistory = 7
	return cfg
}

// TestRestoreConfigForDryRun verifies the core invariant restoreConfigForDryRun
// exists to protect: a client-side dry-run action that swaps KubeClient,
// Releases, and mutates Releases.MaxHistory in place must not leave any of
// that swap/mutation visible to a later real action sharing the same
// *action.Configuration. Without this restore, a real install/upgrade
// following a dry-run render would silently apply through a fake client and
// never persist a release record (see docs/plan-proposal.md's discussion of
// this exact regression).
func TestRestoreConfigForDryRun(t *testing.T) {
	t.Parallel()
	cfg := newTestConfiguration(t)
	originalKubeClient := cfg.KubeClient
	originalReleases := cfg.Releases
	originalCapabilities := cfg.Capabilities

	restore := restoreConfigForDryRun(cfg)

	// Simulate what Helm's Install action does in its
	// !interactWithServer(DryRunStrategy) branch: swap KubeClient and
	// Releases to fakes, and (independently, as Upgrade does) mutate
	// MaxHistory in place on whatever Releases object is current.
	cfg.KubeClient = &kubefake.PrintingKubeClient{}
	cfg.Releases = storage.Init(driver.NewMemory())
	cfg.Releases.MaxHistory = 1
	cfg.Capabilities = &chartcommon.Capabilities{KubeVersion: chartcommon.KubeVersion{Version: "v1.20.0"}}

	restore()

	assert.Same(t, originalKubeClient, cfg.KubeClient, "KubeClient must be restored to the pre-dry-run client")
	assert.Same(t, originalReleases, cfg.Releases, "Releases must be restored to the pre-dry-run storage")
	require.NotNil(t, cfg.Releases)
	assert.Equal(t, 7, cfg.Releases.MaxHistory, "MaxHistory must be restored even though only the Releases pointer (not this in-place mutation) would otherwise be undone")
	assert.NotSame(t, originalCapabilities, cfg.Capabilities, "restoreConfigForDryRun (unlike restoreCapabilitiesForDryRun) must leave Capabilities untouched, since Upgrade legitimately caches a real discovery result there")
}

// TestRestoreCapabilitiesForDryRun verifies the Install-only variant
// additionally restores Capabilities, undoing the fake value Helm's
// install action swaps in for a client-side dry run.
func TestRestoreCapabilitiesForDryRun(t *testing.T) {
	t.Parallel()
	cfg := newTestConfiguration(t)
	originalKubeClient := cfg.KubeClient
	originalReleases := cfg.Releases
	originalCapabilities := cfg.Capabilities

	restore := restoreCapabilitiesForDryRun(cfg)

	cfg.KubeClient = &kubefake.PrintingKubeClient{}
	cfg.Releases = storage.Init(driver.NewMemory())
	cfg.Capabilities = &chartcommon.Capabilities{KubeVersion: chartcommon.KubeVersion{Version: "v1.20.0"}}

	restore()

	assert.Same(t, originalKubeClient, cfg.KubeClient, "KubeClient must be restored")
	assert.Same(t, originalReleases, cfg.Releases, "Releases must be restored")
	assert.Same(t, originalCapabilities, cfg.Capabilities, "Capabilities must be restored for the Install dry-run path")
}
