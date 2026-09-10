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
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/target"
)

func TestRESTConfig_MissingExplicitKubeconfig(t *testing.T) {
	t.Parallel()

	got := target.NewResolver(target.Config{
		KubeconfigPath: filepath.Join(t.TempDir(), "missing-kubeconfig"),
	}).Resolve("")
	cfg, err := got.RESTConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
	assert.NotContains(t, err.Error(), "fake-token")
}

func TestRESTConfig_MalformedKubeconfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte("not: valid: [ kubeconfig"), 0o600))

	got := target.NewResolver(target.Config{KubeconfigPath: path}).Resolve("prod")
	assert.Equal(t, "prod", got.Context())
	assert.Equal(t, target.ContextSourcePlatform, got.ContextSource())

	cfg, err := got.RESTConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
}

func TestRESTConfig_MissingSelectedContext(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	got := target.NewResolver(target.Config{
		KubeconfigPath:  path,
		ContextOverride: "does-not-exist",
	}).Resolve("")
	assert.Equal(t, "does-not-exist", got.Context())
	assert.Equal(t, target.ContextSourceExplicit, got.ContextSource())

	cfg, err := got.RESTConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
}

func TestRESTConfig_ContextReferencesMissingCluster(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, `apiVersion: v1
kind: Config
current-context: broken
clusters: []
contexts:
- name: broken
  context:
    cluster: missing-cluster
    user: test-user
users:
- name: test-user
  user:
    token: fake-token
`)
	got := target.NewResolver(target.Config{KubeconfigPath: path}).Resolve("")
	cfg, err := got.RESTConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
	assert.NotContains(t, err.Error(), "fake-token")
}

func TestRESTConfig_EmptyKubeconfigIsError(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{}).Resolve("")
	assert.Empty(t, got.Context())
	assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())
	assert.Equal(t, "default", got.Namespace())

	cfg, err := got.RESTConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
}

func TestTarget_DoesNotReresolveAfterKUBECONFIGChange(t *testing.T) {
	a := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	b := writeKubeconfig(t, twoClusterKubeconfig("prod", "", ""))

	t.Setenv("KUBECONFIG", a)
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{}).Resolve("")
	assert.Equal(t, "dev", got.Context())
	assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())

	t.Setenv("KUBECONFIG", b)

	assert.Equal(t, "dev", got.Context())
	assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.test", cfg.Host)
}

func TestTarget_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "sandbox", ""))
	got := target.NewResolver(target.Config{KubeconfigPath: path}).Resolve("")

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			assert.Equal(t, "dev", got.Context())
			assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())
			assert.Equal(t, "sandbox", got.Namespace())
			cfg, err := got.RESTConfig()
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, "https://dev.example.test", cfg.Host)
		})
	}
	wg.Wait()
}
