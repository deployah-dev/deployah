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

package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// minimalKubeconfig is a self-contained kubeconfig fixture used by tests
// that resolve the current context or a REST config, so they never touch
// the developer's real ~/.kube/config or KUBECONFIG env var.
const minimalKubeconfig = `apiVersion: v1
kind: Config
current-context: test-context
clusters:
- name: test-cluster
  cluster:
    server: https://example.com:6443
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
users:
- name: test-user
  user:
    token: fake-token
`

// minimalSpecYAML is a self-contained spec fixture with a valid apiVersion
// and a single component, reused by Spec/ParseManifest tests.
const minimalSpecYAML = `apiVersion: v1-alpha.5
project: demo
components:
  web:
    image: nginx:1.27
    port: 8080
`

// MockHelmClient is a mock implementation of [HelmClient] for testing.
type MockHelmClient struct {
	mock.Mock
}

// IsReachable implements [HelmClient].
func (m *MockHelmClient) IsReachable() error {
	args := m.Called()
	return args.Error(0)
}

// InstallApp implements [HelmClient].
func (m *MockHelmClient) InstallApp(ctx context.Context, dryRun bool, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) error {
	args := m.Called(ctx, dryRun, resolved, postRenderer)
	return args.Error(0)
}

// RenderManifests implements [HelmClient].
func (m *MockHelmClient) RenderManifests(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (*render.RenderResult, func(), error) {
	args := m.Called(ctx, resolved, postRenderer)
	if err := args.Error(2); err != nil {
		return nil, func() {}, err
	}
	if args.Get(0) == nil {
		return nil, func() {}, errors.New("mock: render result not set")
	}
	result, ok := args.Get(0).(*render.RenderResult)
	if !ok {
		return nil, func() {}, fmt.Errorf("unexpected mock return type %T", args.Get(0))
	}
	cleanup, cleanupOK := args.Get(1).(func())
	if !cleanupOK || cleanup == nil {
		cleanup = func() {}
	}
	return result, cleanup, nil
}

// RenderOffline implements [HelmClient].
func (m *MockHelmClient) RenderOffline(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (*render.RenderResult, func(), error) {
	args := m.Called(ctx, resolved, postRenderer)
	if err := args.Error(2); err != nil {
		return nil, func() {}, err
	}
	if args.Get(0) == nil {
		return nil, func() {}, errors.New("mock: render result not set")
	}
	result, ok := args.Get(0).(*render.RenderResult)
	if !ok {
		return nil, func() {}, fmt.Errorf("unexpected mock return type %T", args.Get(0))
	}
	cleanup, cleanupOK := args.Get(1).(func())
	if !cleanupOK || cleanup == nil {
		cleanup = func() {}
	}
	return result, cleanup, nil
}

// DeleteRelease implements [HelmClient].
func (m *MockHelmClient) DeleteRelease(ctx context.Context, project, environment string, wait bool) error {
	args := m.Called(ctx, project, environment, wait)
	return args.Error(0)
}

// GetRelease implements [HelmClient].
func (m *MockHelmClient) GetRelease(ctx context.Context, project, environment string) (*v1.Release, error) {
	args := m.Called(ctx, project, environment)
	if err := args.Error(1); err != nil {
		return nil, err
	}
	if args.Get(0) == nil {
		return nil, errors.New("mock: release not set")
	}
	rel, ok := args.Get(0).(*v1.Release)
	if !ok {
		return nil, fmt.Errorf("unexpected mock return type %T", args.Get(0))
	}
	return rel, nil
}

// ListReleases implements [HelmClient].
func (m *MockHelmClient) ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error) {
	args := m.Called(ctx, selector)
	if err := args.Error(1); err != nil {
		return nil, err
	}
	if args.Get(0) == nil {
		return nil, errors.New("mock: releases not set")
	}
	rels, ok := args.Get(0).([]*v1.Release)
	if !ok {
		return nil, fmt.Errorf("unexpected mock return type %T", args.Get(0))
	}
	return rels, nil
}

// GetReleaseHistory implements [HelmClient].
func (m *MockHelmClient) GetReleaseHistory(ctx context.Context, project, environment string) ([]*v1.Release, error) {
	args := m.Called(ctx, project, environment)
	if err := args.Error(1); err != nil {
		return nil, err
	}
	if args.Get(0) == nil {
		return nil, errors.New("mock: releases not set")
	}
	rels, ok := args.Get(0).([]*v1.Release)
	if !ok {
		return nil, fmt.Errorf("unexpected mock return type %T", args.Get(0))
	}
	return rels, nil
}

// RollbackRelease implements [HelmClient].
func (m *MockHelmClient) RollbackRelease(ctx context.Context, releaseName string, revision int, timeout time.Duration) error {
	args := m.Called(ctx, releaseName, revision, timeout)
	return args.Error(0)
}

// TestSessionWithDependencyInjection covers the named case.
func TestSessionWithDependencyInjection(t *testing.T) {
	t.Run("should use injected helm factory via Target", func(t *testing.T) {
		mockHelm := &MockHelmClient{}
		sess := New(WithHelmFactory(func(s *Session) (HelmClient, error) {
			return mockHelm, nil
		}))

		cluster, err := sess.Target(t.Context(), "")
		assert.NoError(t, err)

		helmClient, err := cluster.Helm()
		assert.NoError(t, err)
		assert.Equal(t, mockHelm, helmClient)
	})

	t.Run("should use injected kubernetes factory via Target", func(t *testing.T) {
		fakeCS := fake.NewSimpleClientset()
		sess := New(WithKubernetesFactory(func(*target.Target) (kubernetes.Interface, error) {
			return fakeCS, nil
		}))

		cluster, err := sess.Target(t.Context(), "")
		assert.NoError(t, err)

		k8sClient, err := cluster.Kubernetes()
		assert.NoError(t, err)
		assert.Equal(t, fakeCS, k8sClient)
	})

	t.Run("should use default configuration values", func(t *testing.T) {
		sess := New()
		assert.Equal(t, DefaultStorageDriver, sess.storageDriver)
		assert.Equal(t, DefaultTimeout, sess.timeout)
	})

	t.Run("should override configuration with options", func(t *testing.T) {
		customTimeout := 5 * time.Minute
		customNamespace := "test-namespace"

		sess := New(
			WithTimeout(customTimeout),
			WithNamespace(customNamespace),
			WithStorageDriver(HelmStorageDriverConfigMap),
		)

		assert.Equal(t, customTimeout, sess.timeout)
		assert.Equal(t, customNamespace, sess.namespace)
		assert.Equal(t, HelmStorageDriverConfigMap, sess.storageDriver)
	})

	t.Run("should memoize helm client within a cluster", func(t *testing.T) {
		mockHelm := &MockHelmClient{}
		callCount := 0

		sess := New(WithHelmFactory(func(s *Session) (HelmClient, error) {
			callCount++
			return mockHelm, nil
		}))

		cluster, err := sess.Target(t.Context(), "")
		require.NoError(t, err)
		c1, err1 := cluster.Helm()
		c2, err2 := cluster.Helm()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.Equal(t, c1, c2)
		assert.Equal(t, 1, callCount, "factory should be called only once")
	})

	t.Run("should handle helm factory errors", func(t *testing.T) {
		expectedError := errors.New("helm factory error")
		sess := New(WithHelmFactory(func(s *Session) (HelmClient, error) {
			return nil, expectedError
		}))

		cluster, err := sess.Target(t.Context(), "")
		require.NoError(t, err)
		client, err := cluster.Helm()

		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "helm client")
	})
}

// TestConfigurationValidation covers the named case.
func TestConfigurationValidation(t *testing.T) {
	t.Run("should validate timeout bounds", func(t *testing.T) {
		tests := []struct {
			name     string
			timeout  time.Duration
			expected bool
		}{
			{"valid timeout", 5 * time.Minute, true},
			{"minimum timeout", HelmTimeoutMin, true},
			{"maximum timeout", HelmTimeoutMax, true},
			{"too short", 10 * time.Second, false},
			{"too long", 120 * time.Minute, false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, ValidateTimeout(tt.timeout))
			})
		}
	})

	t.Run("should validate storage drivers", func(t *testing.T) {
		tests := []struct {
			name     string
			driver   string
			expected bool
		}{
			{"secret driver", HelmStorageDriverSecret, true},
			{"configmap driver", HelmStorageDriverConfigMap, true},
			{"memory driver", HelmStorageDriverMemory, true},
			{"invalid driver", "invalid", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, ValidateStorageDriver(tt.driver))
			})
		}
	})
}

// TestContextOperations verifies WithContext / FromContext round-trip.
func TestContextOperations(t *testing.T) {
	t.Run("should store and retrieve session from context", func(t *testing.T) {
		sess := New()
		ctx := WithContext(t.Context(), sess)
		assert.Equal(t, sess, FromContext(ctx))
	})

	t.Run("should return nil for context without session", func(t *testing.T) {
		assert.Nil(t, FromContext(t.Context()))
	})
}

// TestTarget covers Session-to-target delegation, not the full
// resolution matrix (that lives in internal/target).
func TestTarget(t *testing.T) {
	t.Run("global context flag wins over platform", func(t *testing.T) {
		t.Parallel()

		sess := New(WithKubeContext("my-context"))
		cluster, err := sess.Target(t.Context(), "prod")
		assert.NoError(t, err)
		assert.Equal(t, "my-context", cluster.Context())
		assert.Equal(t, target.ContextSourceExplicit, cluster.ContextSource())
	})

	t.Run("no platform file falls back to kubeconfig source", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, writeFile(path, minimalKubeconfig))
		sess := New(
			WithSpecPath("/nonexistent/path/deployah.yaml"),
			WithKubeconfig(path),
		)
		cluster, err := sess.Target(t.Context(), "prod")
		assert.NoError(t, err)
		assert.Equal(t, "test-context", cluster.Context())
		assert.Equal(t, target.ContextSourceKubeconfig, cluster.ContextSource())
	})

	t.Run("platform file context is applied when no --context flag", func(t *testing.T) {
		t.Parallel()

		platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
		require.NoError(t, writeFile(platformPath, platformContextYAML("prod-eks")))

		sess := New(WithPlatformFile(platformPath))
		cluster, err := sess.Target(t.Context(), "production")
		assert.NoError(t, err)
		assert.Equal(t, "prod-eks", cluster.Context())
		assert.Equal(t, target.ContextSourcePlatform, cluster.ContextSource())
	})

	t.Run("--context flag overrides platform file context", func(t *testing.T) {
		t.Parallel()

		platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
		require.NoError(t, writeFile(platformPath, platformContextYAML("prod-eks")))

		sess := New(
			WithPlatformFile(platformPath),
			WithKubeContext("my-override"),
		)
		cluster, err := sess.Target(t.Context(), "production")
		assert.NoError(t, err)
		assert.Equal(t, "my-override", cluster.Context())
		assert.Equal(t, target.ContextSourceExplicit, cluster.ContextSource())
	})

	t.Run("cluster namespace uses kubeconfig context namespace", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, writeFile(path, kubeconfigWithNamespace("payments")))
		sess := New(WithKubeconfig(path))
		cluster, err := sess.Target(t.Context(), "")
		require.NoError(t, err)
		assert.Equal(t, "payments", cluster.Namespace())
	})

	t.Run("explicit namespace wins over kubeconfig context namespace", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, writeFile(path, kubeconfigWithNamespace("payments")))
		sess := New(WithKubeconfig(path), WithNamespace("custom"))
		cluster, err := sess.Target(t.Context(), "")
		require.NoError(t, err)
		assert.Equal(t, "custom", cluster.Namespace())
	})
}

func TestClusterHelmUsesResolvedTarget(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, writeFile(path, kubeconfigWithNamespace("payments")))

	var gotNS, gotCtx string
	sess := New(
		WithKubeconfig(path),
		WithHelmFactory(func(s *Session) (HelmClient, error) {
			gotNS = s.namespace
			gotCtx = s.kubeContext
			return &MockHelmClient{}, nil
		}),
	)
	cluster, err := sess.Target(t.Context(), "")
	require.NoError(t, err)
	_, err = cluster.Helm()
	require.NoError(t, err)
	assert.Equal(t, "payments", gotNS)
	assert.Equal(t, "production", gotCtx)
}

func platformContextYAML(contextName string) string {
	return `apiVersion: platform/v1-alpha.3
environments:
  production:
    context: ` + contextName + `
    domains:
      main:
        baseDomain: example.com
`
}

func kubeconfigWithNamespace(ns string) string {
	return `apiVersion: v1
kind: Config
current-context: production
clusters:
- name: production-cluster
  cluster:
    server: https://example.com:6443
contexts:
- name: production
  context:
    cluster: production-cluster
    user: test-user
    namespace: ` + ns + `
users:
- name: test-user
  user:
    token: fake-token
`
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// TestCommandPolicy verifies the WithCommandPolicy option round-trips
// through the CommandPolicy accessor, including the unset default.
func TestCommandPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want CommandPolicy
	}{
		{name: "defaults to lenient when unset", want: PolicyLenient},
		{name: "strict policy round-trips", opts: []Option{WithCommandPolicy(PolicyStrict)}, want: PolicyStrict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, New(tt.opts...).CommandPolicy())
		})
	}
}

// TestDebugKeepTempChart verifies the WithDebug option round-trips through
// the DebugKeepTempChart accessor.
func TestDebugKeepTempChart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want bool
	}{
		{name: "defaults to false when unset", want: false},
		{name: "true round-trips", opts: []Option{WithDebug(true)}, want: true},
		{name: "explicit false round-trips", opts: []Option{WithDebug(false)}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, New(tt.opts...).DebugKeepTempChart())
		})
	}
}

// TestTimeoutAccessor verifies the Timeout accessor returns the configured
// value, including the package default when unset.
func TestTimeoutAccessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want time.Duration
	}{
		{name: "defaults to package default", want: DefaultTimeout},
		{name: "custom timeout round-trips", opts: []Option{WithTimeout(90 * time.Second)}, want: 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, New(tt.opts...).Timeout())
		})
	}
}

// TestSpecPathAccessor verifies SpecPath returns the configured spec path,
// or [spec.DefaultSpecPath] when unset.
func TestSpecPathAccessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "default filename when unset", want: spec.DefaultSpecPath},
		{name: "returns configured path", opts: []Option{WithSpecPath("/tmp/deployah.yaml")}, want: "/tmp/deployah.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, New(tt.opts...).SpecPath())
		})
	}
}

func TestPlatformPathAccessor(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "default filename when unset", want: spec.DefaultPlatformPath},
		{name: "explicit platform file", opts: []Option{WithPlatformFile("/tmp/custom.platform.yaml")}, want: "/tmp/custom.platform.yaml"},
		{name: "same directory as spec", opts: []Option{WithSpecPath(filepath.Join("app", "deployah.yaml"))}, want: filepath.Join("app", spec.DefaultPlatformPath)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(spec.PlatformEnvVar, "")
			assert.Equal(t, tt.want, New(tt.opts...).PlatformPath())
		})
	}
}

func TestSessionConstructsWorkspaceAfterOptions(t *testing.T) {
	t.Parallel()

	specPath := filepath.Join(t.TempDir(), "deployah.yaml")
	platformPath := filepath.Join(t.TempDir(), "custom.platform.yaml")
	sess := New(WithSpecPath(specPath), WithPlatformFile(platformPath))
	assert.Equal(t, specPath, sess.SpecPath())
	assert.Equal(t, platformPath, sess.PlatformPath())
}

func TestSessionPlatformDelegation(t *testing.T) {
	t.Parallel()

	platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	require.NoError(t, writeFile(platformPath, platformContextYAML("prod-eks")))
	sess := New(WithPlatformFile(platformPath))
	p, err := sess.Platform()
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "prod-eks", spec.PlatformEnvContext(p, "production"))
}

func TestSessionPlatformMemoized(t *testing.T) {
	t.Parallel()

	platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	require.NoError(t, writeFile(platformPath, platformContextYAML("prod-eks")))
	sess := New(WithPlatformFile(platformPath))

	first, err := sess.Platform()
	require.NoError(t, err)
	require.NotNil(t, first)

	require.NoError(t, os.Remove(platformPath))
	second, err := sess.Platform()
	require.NoError(t, err)
	assert.Same(t, first, second)
}

func TestCloneWithContextSharesWorkspace(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "a.platform.yaml")
	pathB := filepath.Join(t.TempDir(), "b.platform.yaml")
	require.NoError(t, writeFile(pathA, platformContextYAML("context-a")))
	require.NoError(t, writeFile(pathB, platformContextYAML("context-b")))

	t.Setenv(spec.PlatformEnvVar, pathA)
	sess := New()
	clone := sess.cloneWithContext("other")
	assert.Same(t, sess.workspace, clone.workspace)

	t.Setenv(spec.PlatformEnvVar, pathB)
	assert.Equal(t, pathA, clone.PlatformPath())
	p, err := clone.Platform()
	require.NoError(t, err)
	assert.Equal(t, "context-a", spec.PlatformEnvContext(p, "production"))
}

// TestKubeContextAccessor verifies KubeContext returns the explicit
// override only, independent of kubeconfig resolution.
func TestKubeContextAccessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "empty when unset", want: ""},
		{name: "returns configured override", opts: []Option{WithKubeContext("staging-context")}, want: "staging-context"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, New(tt.opts...).KubeContext())
		})
	}
}

// TestSpec verifies Spec loads a real manifest from disk and wraps load
// errors when the file is missing.
func TestSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(t *testing.T) *Session
		wantErr     bool
		errContains string
		check       func(t *testing.T, m *spec.Spec)
	}{
		{
			name: "loads a valid manifest from disk",
			setup: func(t *testing.T) *Session {
				t.Helper()
				specPath := filepath.Join(t.TempDir(), "deployah.yaml")
				require.NoError(t, writeFile(specPath, minimalSpecYAML))
				return New(WithSpecPath(specPath))
			},
			check: func(t *testing.T, m *spec.Spec) {
				t.Helper()
				require.NotNil(t, m)
				assert.Equal(t, "demo", m.Project)
			},
		},
		{
			name: "wraps the underlying load error for a missing file",
			setup: func(t *testing.T) *Session {
				t.Helper()
				return New(WithSpecPath(filepath.Join(t.TempDir(), "missing.yaml")))
			},
			wantErr:     true,
			errContains: "failed to load spec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, err := tt.setup(t).Spec(t.Context(), "")
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, m)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			tt.check(t, m)
		})
	}
}

// TestParseManifest verifies ParseManifest's happy path and its behavior
// when the underlying file cannot be read.
func TestParseManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) *Session
		wantErr bool
		check   func(t *testing.T, m *spec.Spec)
	}{
		{
			name: "parses a valid manifest",
			setup: func(t *testing.T) *Session {
				t.Helper()
				specPath := filepath.Join(t.TempDir(), "deployah.yaml")
				require.NoError(t, writeFile(specPath, minimalSpecYAML))
				return New(WithSpecPath(specPath))
			},
			check: func(t *testing.T, m *spec.Spec) {
				t.Helper()
				require.NotNil(t, m)
				assert.Equal(t, "demo", m.Project)
			},
		},
		{
			name: "errors for a missing file",
			setup: func(t *testing.T) *Session {
				t.Helper()
				return New(WithSpecPath(filepath.Join(t.TempDir(), "missing.yaml")))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, err := tt.setup(t).ParseManifest()
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, m)
				return
			}
			require.NoError(t, err)
			tt.check(t, m)
		})
	}
}

// TestCurrentKubeContext verifies context resolution from an explicit
// kubeconfig path, from extra kubeconfig paths, and the empty-string
// fallback when no kubeconfig is readable.
func TestCurrentKubeContext(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) *Session
		want  string
	}{
		{
			name: "explicit kubeconfig path resolves current context",
			setup: func(t *testing.T) *Session {
				t.Helper()
				path := filepath.Join(t.TempDir(), "kubeconfig")
				require.NoError(t, writeFile(path, minimalKubeconfig))
				return New(WithKubeconfig(path))
			},
			want: "test-context",
		},
		{
			name: "missing kubeconfig path returns empty string",
			setup: func(t *testing.T) *Session {
				t.Helper()
				return New(WithKubeconfig(filepath.Join(t.TempDir(), "missing-kubeconfig")))
			},
			want: "",
		},
		{
			name: "extra kubeconfig paths are honored without an explicit path",
			setup: func(t *testing.T) *Session {
				t.Helper()
				t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
				t.Setenv("HOME", t.TempDir())

				extraPath := filepath.Join(t.TempDir(), "extra-kubeconfig")
				require.NoError(t, writeFile(extraPath, minimalKubeconfig))
				return New(WithExtraKubeconfigPaths(extraPath))
			},
			want: "test-context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv in the "extra kubeconfig" case forbids t.Parallel.
			assert.Equal(t, tt.want, tt.setup(t).CurrentKubeContext())
		})
	}
}

// TestClusterRESTConfig verifies Cluster.RESTConfig falls back to
// kubeconfig resolution when no in-cluster config is available (always the
// case in this test environment), and that it surfaces a clear error when
// the kubeconfig cannot be resolved either.
func TestClusterRESTConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(t *testing.T) *Session
		wantErr     bool
		errContains string
		check       func(t *testing.T, cfg *rest.Config)
	}{
		{
			name: "resolves from kubeconfig when reachable",
			setup: func(t *testing.T) *Session {
				t.Helper()
				path := filepath.Join(t.TempDir(), "kubeconfig")
				require.NoError(t, writeFile(path, minimalKubeconfig))
				return New(WithKubeconfig(path))
			},
			check: func(t *testing.T, cfg *rest.Config) {
				t.Helper()
				require.NotNil(t, cfg)
				assert.Equal(t, "https://example.com:6443", cfg.Host)
			},
		},
		{
			name: "errors with guidance when kubeconfig is unresolvable",
			setup: func(t *testing.T) *Session {
				t.Helper()
				return New(WithKubeconfig(filepath.Join(t.TempDir(), "missing-kubeconfig")))
			},
			wantErr:     true,
			errContains: "failed to build kubernetes config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cluster, err := tt.setup(t).Target(t.Context(), "")
			require.NoError(t, err)

			cfg, err := cluster.RESTConfig()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

// TestIntegrationWithMocks covers the named case.
func TestIntegrationWithMocks(t *testing.T) {
	t.Run("full workflow with mock helm client", func(t *testing.T) {
		mockHelm := &MockHelmClient{}
		mockHelm.On("InstallApp", mock.Anything, false, mock.Anything, mock.Anything).Return(nil)

		sess := New(WithHelmFactory(func(s *Session) (HelmClient, error) {
			return mockHelm, nil
		}))

		cluster, err := sess.Target(t.Context(), "")
		assert.NoError(t, err)

		helmClient, err := cluster.Helm()
		assert.NoError(t, err)

		err = helmClient.InstallApp(t.Context(), false, nil, nil)
		assert.NoError(t, err)
		mockHelm.AssertExpectations(t)
	})
}
