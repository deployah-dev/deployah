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

package target_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/target"
)

func TestClientConfig_IsLazyWhenKubeconfigMissing(t *testing.T) {
	t.Parallel()

	got := target.NewResolver(target.Config{
		KubeconfigPath:    filepath.Join(t.TempDir(), "missing-kubeconfig"),
		NamespaceOverride: "cli-ns",
	}).Resolve("")

	cc := got.ClientConfig()
	require.NotNil(t, cc)

	ns, overridden, err := cc.Namespace()
	require.NoError(t, err)
	assert.True(t, overridden)
	assert.Equal(t, "cli-ns", ns)

	_, err = cc.ClientConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")

	_, err = cc.RawConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
}

func TestClientConfig_PinsNamespaceOverKubeconfigContext(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "sandbox", "payments"))
	got := target.NewResolver(target.Config{
		KubeconfigPath:    path,
		ContextOverride:   "prod",
		NamespaceOverride: "cli-ns",
	}).Resolve("")

	assert.Equal(t, "cli-ns", got.Namespace())

	ns, overridden, err := got.ClientConfig().Namespace()
	require.NoError(t, err)
	assert.True(t, overridden)
	assert.Equal(t, "cli-ns", ns)
}

func TestClientConfig_MatchesRESTConfig(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	got := target.NewResolver(target.Config{
		KubeconfigPath:  path,
		ContextOverride: "prod",
	}).Resolve("")

	restCfg, err := got.RESTConfig()
	require.NoError(t, err)
	ccCfg, err := got.ClientConfig().ClientConfig()
	require.NoError(t, err)
	assert.Equal(t, restCfg.Host, ccCfg.Host)
	assert.Equal(t, "https://prod.example.test", ccCfg.Host)
}

func TestClientConfig_RawConfigIsTargetMerged(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "sandbox", "payments"))
	got := target.NewResolver(target.Config{
		KubeconfigPath:    path,
		ContextOverride:   "prod",
		NamespaceOverride: "cli-ns",
	}).Resolve("")

	raw, err := got.ClientConfig().RawConfig()
	require.NoError(t, err)
	assert.Equal(t, "prod", raw.CurrentContext)
	require.Contains(t, raw.Contexts, "prod")
	assert.Equal(t, "cli-ns", raw.Contexts["prod"].Namespace)
}

func TestClientConfig_EmptyKubeconfigIsError(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{}).Resolve("")
	cc := got.ClientConfig()
	require.NotNil(t, cc)

	ns, overridden, err := cc.Namespace()
	require.NoError(t, err)
	assert.True(t, overridden)
	assert.Equal(t, "default", ns)

	_, err = cc.ClientConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")

	_, err = cc.RawConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
}

func TestClientConfig_NilTarget(t *testing.T) {
	t.Parallel()

	var got *target.Target
	cc := got.ClientConfig()
	require.NotNil(t, cc)

	ns, overridden, err := cc.Namespace()
	require.NoError(t, err)
	assert.True(t, overridden)
	assert.Equal(t, "default", ns)

	cfg, err := got.RESTConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
	assert.Contains(t, err.Error(), "target is nil")
}
