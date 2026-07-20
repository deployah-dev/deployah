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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

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
const minimalSpecYAML = `apiVersion: v1-alpha.2
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
func (m *MockHelmClient) InstallApp(ctx context.Context, manifest *spec.Spec, environment string, dryRun bool, resolved *spec.ResolvedSpec) error {
	args := m.Called(ctx, manifest, environment, dryRun, resolved)
	return args.Error(0)
}

// RenderManifests implements [HelmClient].
func (m *MockHelmClient) RenderManifests(ctx context.Context, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) (*render.RenderResult, func(), error) {
	args := m.Called(ctx, manifest, environment, resolved)
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
func (m *MockHelmClient) RenderOffline(ctx context.Context, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) (*render.RenderResult, func(), error) {
	args := m.Called(ctx, manifest, environment, resolved)
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

		cluster, err := sess.Target(context.Background(), "")
		assert.NoError(t, err)

		helmClient, err := cluster.Helm()
		assert.NoError(t, err)
		assert.Equal(t, mockHelm, helmClient)
	})

	t.Run("should use injected kubernetes factory via Target", func(t *testing.T) {
		fakeCS := fake.NewSimpleClientset()
		sess := New(WithKubernetesFactory(func(s *Session) (kubernetes.Interface, error) {
			return fakeCS, nil
		}))

		cluster, err := sess.Target(context.Background(), "")
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

		cluster, err := sess.Target(context.Background(), "")
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

		cluster, err := sess.Target(context.Background(), "")
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
		ctx := WithContext(context.Background(), sess)
		assert.Equal(t, sess, FromContext(ctx))
	})

	t.Run("should return nil for context without session", func(t *testing.T) {
		assert.Nil(t, FromContext(context.Background()))
	})
}

// TestTarget covers the named case.
func TestTarget(t *testing.T) {
	t.Run("empty env returns cluster with empty context", func(t *testing.T) {
		sess := New()
		cluster, err := sess.Target(context.Background(), "")
		assert.NoError(t, err)
		assert.NotNil(t, cluster)
		assert.Equal(t, "", cluster.kubeContext)
	})

	t.Run("global context flag wins over platform", func(t *testing.T) {
		sess := New(WithKubeContext("my-context"))
		cluster, err := sess.Target(context.Background(), "prod")
		assert.NoError(t, err)
		assert.Equal(t, "my-context", cluster.kubeContext)
	})

	t.Run("no platform file falls back to default context", func(t *testing.T) {
		sess := New(WithSpecPath("/nonexistent/path/deployah.yaml"))
		cluster, err := sess.Target(context.Background(), "prod")
		assert.NoError(t, err)
		assert.NotNil(t, cluster)
		assert.Equal(t, "", cluster.kubeContext)
	})

	t.Run("platform file context is applied when no --context flag", func(t *testing.T) {
		platformDir := t.TempDir()
		platformPath := platformDir + "/deployah.platform.yaml"

		platformYAML := `apiVersion: platform/v1-alpha.1
environments:
  production:
    context: prod-eks
    domains:
      main:
        baseDomain: example.com
`
		require.NoError(t, writeFile(platformPath, platformYAML))

		sess := New(WithPlatformFile(platformPath))
		cluster, err := sess.Target(context.Background(), "production")
		assert.NoError(t, err)
		assert.Equal(t, "prod-eks", cluster.kubeContext)
	})

	t.Run("--context flag overrides platform file context", func(t *testing.T) {
		platformDir := t.TempDir()
		platformPath := platformDir + "/deployah.platform.yaml"

		platformYAML := `apiVersion: platform/v1-alpha.1
environments:
  production:
    context: prod-eks
    domains:
      main:
        baseDomain: example.com
`
		require.NoError(t, writeFile(platformPath, platformYAML))

		sess := New(
			WithPlatformFile(platformPath),
			WithKubeContext("my-override"),
		)
		cluster, err := sess.Target(context.Background(), "production")
		assert.NoError(t, err)
		assert.Equal(t, "my-override", cluster.kubeContext)
	})

	t.Run("cluster namespace falls back to default", func(t *testing.T) {
		sess := New()
		cluster, err := sess.Target(context.Background(), "")
		require.NoError(t, err)
		assert.Equal(t, DefaultNamespace, cluster.Namespace())
	})

	t.Run("cluster namespace uses session value", func(t *testing.T) {
		sess := New(WithNamespace("my-ns"))
		cluster, err := sess.Target(context.Background(), "")
		require.NoError(t, err)
		assert.Equal(t, "my-ns", cluster.Namespace())
	})
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
// including the empty default.
func TestSpecPathAccessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []Option
		want string
	}{
		{name: "empty when unset", want: ""},
		{name: "returns configured path", opts: []Option{WithSpecPath("/tmp/deployah.yaml")}, want: "/tmp/deployah.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, New(tt.opts...).SpecPath())
		})
	}
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

// TestClose verifies Close clears the memoized platform config without
// error, so a subsequent Platform call re-resolves rather than returning a
// stale cached value.
func TestClose(t *testing.T) {
	platformPath := filepath.Join(t.TempDir(), "deployah.platform.yaml")
	platformYAML := `apiVersion: platform/v1-alpha.1
environments:
  production:
    domains:
      main:
        baseDomain: example.com
`
	require.NoError(t, writeFile(platformPath, platformYAML))

	sess := New(WithPlatformFile(platformPath))

	_, err := sess.Platform()
	require.NoError(t, err)
	require.NotNil(t, sess.platform, "platform should be memoized after first load")

	require.NoError(t, sess.Close())
	assert.Nil(t, sess.platform, "Close should clear the memoized platform config")
}

// TestSpec verifies Spec's guard clause and its happy-path loading of a
// real manifest from disk.
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
			name:        "errors when spec path is unset",
			setup:       func(t *testing.T) *Session { t.Helper(); return New() },
			wantErr:     true,
			errContains: "spec path must be set",
		},
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

			m, err := tt.setup(t).Spec(context.Background(), "")
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

			cluster, err := tt.setup(t).Target(context.Background(), "")
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
		testManifest := &spec.Spec{Project: "test-project"}

		mockHelm.On("InstallApp", mock.Anything, testManifest, "production", false, mock.Anything).Return(nil)

		sess := New(WithHelmFactory(func(s *Session) (HelmClient, error) {
			return mockHelm, nil
		}))

		cluster, err := sess.Target(context.Background(), "")
		assert.NoError(t, err)

		helmClient, err := cluster.Helm()
		assert.NoError(t, err)

		err = helmClient.InstallApp(context.Background(), testManifest, "production", false, nil)
		assert.NoError(t, err)
		mockHelm.AssertExpectations(t)
	})
}
