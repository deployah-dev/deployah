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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"

	diskcached "k8s.io/client-go/discovery/cached/disk"
)

func TestRESTClientGetter_MatchesTargetHostAndWrap(t *testing.T) {
	t.Parallel()

	path := writeDestKubeconfig(t, destTwoClusterKubeconfig("dev", "", ""))
	tgt := target.NewResolver(target.Config{
		KubeconfigPath:  path,
		ContextOverride: "prod",
	}).Resolve("")

	want, err := tgt.RESTConfig()
	require.NoError(t, err)

	getter := NewRESTClientGetter(tgt.ClientConfig())
	got, err := getter.ToRESTConfig()
	require.NoError(t, err)
	assert.Equal(t, want.Host, got.Host)
	assert.Equal(t, 100, got.Burst)
	assert.Equal(t, float32(0), got.QPS)
	assert.Equal(t, "Helm/4.3", got.UserAgent)
	assert.NotNil(t, got.WrapTransport)

	assert.Zero(t, want.Burst)
	assert.NotEqual(t, "Helm/4.3", want.UserAgent)
}

func TestRESTClientGetter_MissingExtraUsesDefault(t *testing.T) {
	def := writeDestKubeconfig(t, destSingleKubeconfig("home", "https://home.example.test"))
	t.Setenv("KUBECONFIG", def)
	t.Setenv("HOME", t.TempDir())

	tgt := target.NewResolver(target.Config{
		ExtraKubeconfigPaths: []string{filepath.Join(t.TempDir(), "missing-local")},
	}).Resolve("")
	want, err := tgt.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://home.example.test", want.Host)

	client, err := NewClient(
		WithRESTClientGetter(NewRESTClientGetter(tgt.ClientConfig())),
		WithNamespace(tgt.Namespace()),
	)
	require.NoError(t, err)
	got, err := client.restGetter.ToRESTConfig()
	require.NoError(t, err)
	assert.Equal(t, want.Host, got.Host)
}

func TestRESTClientGetter_MultiFileKubeconfig(t *testing.T) {
	a := writeDestKubeconfig(t, destSingleKubeconfig("dev", "https://dev.example.test"))
	b := writeDestKubeconfig(t, destSingleKubeconfig("prod", "https://prod.example.test"))
	t.Setenv("KUBECONFIG", a+string(os.PathListSeparator)+b)
	t.Setenv("HOME", t.TempDir())

	tgt := target.NewResolver(target.Config{ContextOverride: "prod"}).Resolve("")
	want, err := tgt.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://prod.example.test", want.Host)

	got, err := NewRESTClientGetter(tgt.ClientConfig()).ToRESTConfig()
	require.NoError(t, err)
	assert.Equal(t, want.Host, got.Host)
}

func TestRESTClientGetter_IgnoresLiveEnvAfterResolve(t *testing.T) {
	a := writeDestKubeconfig(t, destSingleKubeconfig("dev", "https://dev.example.test"))
	b := writeDestKubeconfig(t, destSingleKubeconfig("prod", "https://prod.example.test"))
	t.Setenv("KUBECONFIG", a)
	t.Setenv("HOME", t.TempDir())

	tgt := target.NewResolver(target.Config{}).Resolve("")
	t.Setenv("KUBECONFIG", b)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HELM_KUBEAPISERVER", "https://other.example")
	t.Setenv("HELM_KUBECONTEXT", "prod")
	t.Setenv("HELM_NAMESPACE", "helm-ns")
	t.Setenv("HELM_KUBETOKEN", "helm-token")

	want, err := tgt.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.test", want.Host)

	getter := NewRESTClientGetter(tgt.ClientConfig())
	got, err := getter.ToRESTConfig()
	require.NoError(t, err)
	assert.Equal(t, want.Host, got.Host)
	assert.NotEqual(t, "helm-token", got.BearerToken)

	ns, _, err := getter.ToRawKubeConfigLoader().Namespace()
	require.NoError(t, err)
	assert.Equal(t, tgt.Namespace(), ns)
}

func TestRESTClientGetter_PinsExplicitNamespace(t *testing.T) {
	t.Parallel()

	path := writeDestKubeconfig(t, destTwoClusterKubeconfig("dev", "sandbox", "payments"))
	tgt := target.NewResolver(target.Config{
		KubeconfigPath:    path,
		ContextOverride:   "prod",
		NamespaceOverride: "cli-ns",
	}).Resolve("")

	ns, overridden, err := NewRESTClientGetter(tgt.ClientConfig()).ToRawKubeConfigLoader().Namespace()
	require.NoError(t, err)
	assert.True(t, overridden)
	assert.Equal(t, "cli-ns", ns)
}

func TestRESTClientGetter_DiscoveryIsLazy(t *testing.T) {
	t.Setenv("KUBECACHEDIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	path := writeDestKubeconfig(t, destSingleKubeconfig("dev", "https://dev.example.test"))
	tgt := target.NewResolver(target.Config{KubeconfigPath: path}).Resolve("")
	getter := NewRESTClientGetter(tgt.ClientConfig())

	dc, err := getter.ToDiscoveryClient()
	require.NoError(t, err)
	_, ok := dc.(*diskcached.CachedDiscoveryClient)
	require.True(t, ok)
	dc.Invalidate()

	mapper, err := getter.ToRESTMapper()
	require.NoError(t, err)
	require.NotNil(t, mapper)
}

func TestRESTClientGetter_UnconfiguredDiscoveryFails(t *testing.T) {
	t.Parallel()

	getter := NewRESTClientGetter(nil)
	_, err := getter.ToDiscoveryClient()
	require.ErrorIs(t, err, ErrDestinationNotConfigured)
	_, err = getter.ToRESTMapper()
	require.ErrorIs(t, err, ErrDestinationNotConfigured)
}

func TestRESTClientGetter_NoInClusterFallback(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	tgt := target.NewResolver(target.Config{}).Resolve("")
	_, err := NewRESTClientGetter(tgt.ClientConfig()).ToRESTConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to build kubernetes config")
}

func TestNewClient_HELMNamespaceDoesNotOverrideStoredNamespace(t *testing.T) {
	t.Setenv("HELM_NAMESPACE", "helm-ns")

	client, err := NewClient(WithNamespace("deployah-ns"))
	require.NoError(t, err)
	assert.Equal(t, "deployah-ns", client.Namespace())
}

func TestNewClient_UnconfiguredGetterIgnoresAmbientKubeconfig(t *testing.T) {
	path := writeDestKubeconfig(t, destSingleKubeconfig("prod", "https://prod.example.test"))
	t.Setenv("KUBECONFIG", path)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HELM_KUBEAPISERVER", "https://other.example")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	tests := []struct {
		name string
		opts []Option
	}{
		{name: "no getter", opts: []Option{WithNamespace("default")}},
		{name: "nil getter", opts: []Option{WithRESTClientGetter(nil), WithNamespace("default")}},
	}
	m := envServiceSpec(t.TempDir(), spec.StringMap{"LOG_LEVEL": "debug"})
	resolved := resolveChart(t, m, "dev")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.opts...)
			require.NoError(t, err)
			require.NotNil(t, client.restGetter)

			_, err = client.restGetter.ToRESTConfig()
			require.ErrorIs(t, err, ErrDestinationNotConfigured)

			err = client.IsReachable()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDestinationNotConfigured)

			result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil)
			require.NoError(t, err)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}
			require.NotNil(t, result)
			assert.Equal(t, "default", result.Namespace)
		})
	}
}

func TestNewClient_OfflineRenderWithoutKubeconfig(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing-kubeconfig"))
	t.Setenv("HOME", t.TempDir())

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)

	m := envServiceSpec(t.TempDir(), spec.StringMap{"LOG_LEVEL": "debug"})
	resolved := resolveChart(t, m, "dev")
	result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NotNil(t, result)
	assert.Equal(t, "default", result.Namespace)
}

func writeDestKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func destTwoClusterKubeconfig(current, devNS, prodNS string) string {
	return `apiVersion: v1
kind: Config
current-context: ` + current + `
clusters:
- name: dev-cluster
  cluster:
    server: https://dev.example.test
- name: prod-cluster
  cluster:
    server: https://prod.example.test
contexts:
- name: dev
  context:
    cluster: dev-cluster
    user: test-user` + destContextNamespaceYAML(devNS) + `
- name: prod
  context:
    cluster: prod-cluster
    user: test-user` + destContextNamespaceYAML(prodNS) + `
users:
- name: test-user
  user:
    token: fake-token
`
}

func destSingleKubeconfig(contextName, server string) string {
	return `apiVersion: v1
kind: Config
current-context: ` + contextName + `
clusters:
- name: ` + contextName + `
  cluster:
    server: ` + server + `
contexts:
- name: ` + contextName + `
  context:
    cluster: ` + contextName + `
    user: test-user
users:
- name: test-user
  user:
    token: fake-token
`
}

func destContextNamespaceYAML(ns string) string {
	if ns == "" {
		return ""
	}
	return `
    namespace: ` + ns
}
