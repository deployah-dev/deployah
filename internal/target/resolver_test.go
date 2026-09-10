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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/target"
)

func TestResolve_ContextPrecedence(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))

	tests := []struct {
		name        string
		override    string
		platform    string
		wantContext string
		wantSource  target.ContextSource
		wantHost    string
	}{
		{
			name:        "explicit wins over platform",
			override:    "prod",
			platform:    "dev",
			wantContext: "prod",
			wantSource:  target.ContextSourceExplicit,
			wantHost:    "https://prod.example.test",
		},
		{
			name:        "platform used without override",
			platform:    "prod",
			wantContext: "prod",
			wantSource:  target.ContextSourcePlatform,
			wantHost:    "https://prod.example.test",
		},
		{
			name:        "kubeconfig current-context fallback",
			wantContext: "dev",
			wantSource:  target.ContextSourceKubeconfig,
			wantHost:    "https://dev.example.test",
		},
		{
			name:        "explicit remains effective when different from current-context",
			override:    "prod",
			wantContext: "prod",
			wantSource:  target.ContextSourceExplicit,
			wantHost:    "https://prod.example.test",
		},
		{
			name:        "platform remains effective when different from current-context",
			platform:    "prod",
			wantContext: "prod",
			wantSource:  target.ContextSourcePlatform,
			wantHost:    "https://prod.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := target.NewResolver(target.Config{
				KubeconfigPath:  path,
				ContextOverride: tt.override,
			}).Resolve(tt.platform)

			assert.Equal(t, tt.wantContext, got.Context())
			assert.Equal(t, tt.wantSource, got.ContextSource())
			cfg, err := got.RESTConfig()
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, cfg.Host)
		})
	}
}

func TestResolve_NamespacePrecedence(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "sandbox", "payments"))

	tests := []struct {
		name     string
		override string
		ns       string
		platform string
		want     string
		wantHost string
	}{
		{
			name:     "explicit namespace wins",
			override: "prod",
			ns:       "custom",
			want:     "custom",
			wantHost: "https://prod.example.test",
		},
		{
			name:     "namespace from explicit selected context",
			override: "prod",
			want:     "payments",
			wantHost: "https://prod.example.test",
		},
		{
			name:     "namespace from platform-selected context",
			platform: "prod",
			want:     "payments",
			wantHost: "https://prod.example.test",
		},
		{
			name:     "namespace from kubeconfig current-context",
			want:     "sandbox",
			wantHost: "https://dev.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := target.NewResolver(target.Config{
				KubeconfigPath:    path,
				ContextOverride:   tt.override,
				NamespaceOverride: tt.ns,
			}).Resolve(tt.platform)
			assert.Equal(t, tt.want, got.Namespace())
			cfg, err := got.RESTConfig()
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, cfg.Host)
		})
	}
}

func TestResolve_DefaultNamespace(t *testing.T) {
	t.Parallel()

	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	got := target.NewResolver(target.Config{KubeconfigPath: path}).Resolve("")
	assert.Equal(t, "default", got.Namespace())
}

func TestResolve_DefaultNamespaceWithoutKubeconfig(t *testing.T) {
	t.Parallel()

	got := target.NewResolver(target.Config{
		KubeconfigPath: filepath.Join(t.TempDir(), "missing"),
	}).Resolve("")
	assert.Equal(t, "default", got.Namespace())
	assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())
}

func TestResolve_ExplicitKubeconfigWinsOverExtras(t *testing.T) {
	t.Parallel()

	explicit := writeKubeconfig(t, singleContextKubeconfig("shared", "https://explicit.example.test", "dev"))
	extra := writeKubeconfig(t, singleContextKubeconfig("shared", "https://extra.example.test", "dev"))

	got := target.NewResolver(target.Config{
		KubeconfigPath:       explicit,
		ExtraKubeconfigPaths: []string{extra},
	}).Resolve("")

	assert.Equal(t, "dev", got.Context())
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://explicit.example.test", cfg.Host)
}

func TestResolve_ExtraPathOrderIsDeterministic(t *testing.T) {
	extraA := writeKubeconfig(t, singleContextKubeconfig("shared", "https://a.example.test", "shared"))
	extraB := writeKubeconfig(t, singleContextKubeconfig("shared", "https://b.example.test", "shared"))
	def := writeKubeconfig(t, singleContextKubeconfig("shared", "https://default.example.test", "shared"))

	t.Setenv("KUBECONFIG", def)
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{
		ContextOverride:      "shared",
		ExtraKubeconfigPaths: []string{extraA, extraB},
	}).Resolve("")
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://a.example.test", cfg.Host)
}

func TestResolve_MissingExtraPathDoesNotFail(t *testing.T) {
	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	t.Setenv("KUBECONFIG", path)
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{
		ExtraKubeconfigPaths: []string{filepath.Join(t.TempDir(), "missing-extra.yaml")},
	}).Resolve("")
	assert.Equal(t, "dev", got.Context())
	assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())
	_, err := got.RESTConfig()
	require.NoError(t, err)
}

func TestResolve_KUBECONFIGWithoutExplicitPath(t *testing.T) {
	path := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	t.Setenv("KUBECONFIG", path)
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{}).Resolve("")
	assert.Equal(t, "dev", got.Context())
	assert.Equal(t, target.ContextSourceKubeconfig, got.ContextSource())
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.test", cfg.Host)
}

func TestResolve_ExplicitPathWinsOverKUBECONFIG(t *testing.T) {
	explicit := writeKubeconfig(t, twoClusterKubeconfig("prod", "", ""))
	other := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	t.Setenv("KUBECONFIG", other)

	got := target.NewResolver(target.Config{KubeconfigPath: explicit}).Resolve("")
	assert.Equal(t, "prod", got.Context())
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://prod.example.test", cfg.Host)
}

func TestResolve_ExtraKubeconfigSuppliesContext(t *testing.T) {
	def := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	extra := writeKubeconfig(t, singleContextKubeconfig("only-extra", "https://extra-only.example.test", "only-extra"))
	t.Setenv("KUBECONFIG", def)
	t.Setenv("HOME", t.TempDir())

	got := target.NewResolver(target.Config{
		ContextOverride:      "only-extra",
		ExtraKubeconfigPaths: []string{extra},
	}).Resolve("")
	assert.Equal(t, "only-extra", got.Context())
	assert.Equal(t, target.ContextSourceExplicit, got.ContextSource())
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://extra-only.example.test", cfg.Host)
}

func TestResolve_ExtraPathsImmutableAfterNewResolver(t *testing.T) {
	extraA := writeKubeconfig(t, singleContextKubeconfig("shared", "https://a.example.test", "shared"))
	extraB := writeKubeconfig(t, singleContextKubeconfig("shared", "https://b.example.test", "shared"))
	def := writeKubeconfig(t, twoClusterKubeconfig("dev", "", ""))
	t.Setenv("KUBECONFIG", def)
	t.Setenv("HOME", t.TempDir())

	paths := []string{extraA}
	resolver := target.NewResolver(target.Config{
		ContextOverride:      "shared",
		ExtraKubeconfigPaths: paths,
	})
	paths[0] = extraB

	got := resolver.Resolve("")
	cfg, err := got.RESTConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://a.example.test", cfg.Host)
}

func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func twoClusterKubeconfig(current, devNS, prodNS string) string {
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
    user: test-user` + contextNamespaceYAML(devNS) + `
- name: prod
  context:
    cluster: prod-cluster
    user: test-user` + contextNamespaceYAML(prodNS) + `
users:
- name: test-user
  user:
    token: fake-token
`
}

func singleContextKubeconfig(cluster, server, contextName string) string {
	return `apiVersion: v1
kind: Config
current-context: ` + contextName + `
clusters:
- name: ` + cluster + `
  cluster:
    server: ` + server + `
contexts:
- name: ` + contextName + `
  context:
    cluster: ` + cluster + `
    user: test-user
users:
- name: test-user
  user:
    token: fake-token
`
}

func contextNamespaceYAML(ns string) string {
	if ns == "" {
		return ""
	}
	return `
    namespace: ` + ns
}
