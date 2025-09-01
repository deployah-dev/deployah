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

package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// MockHelmClient is a mock implementation of HelmClient for testing
type MockHelmClient struct {
	mock.Mock
}

func (m *MockHelmClient) InstallApp(ctx context.Context, manifest *manifest.Manifest, environment string, dryRun bool) error {
	args := m.Called(ctx, manifest, environment, dryRun)
	return args.Error(0)
}

func (m *MockHelmClient) DeleteRelease(ctx context.Context, project, environment string) error {
	args := m.Called(ctx, project, environment)
	return args.Error(0)
}

func (m *MockHelmClient) GetRelease(ctx context.Context, project, environment string) (*v1.Release, error) {
	args := m.Called(ctx, project, environment)
	return args.Get(0).(*v1.Release), args.Error(1)
}

func (m *MockHelmClient) ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error) {
	args := m.Called(ctx, selector)
	return args.Get(0).([]*v1.Release), args.Error(1)
}

func (m *MockHelmClient) GetReleaseHistory(ctx context.Context, project, environment string) ([]*v1.Release, error) {
	args := m.Called(ctx, project, environment)
	return args.Get(0).([]*v1.Release), args.Error(1)
}

func (m *MockHelmClient) RollbackRelease(ctx context.Context, releaseName string, revision int, timeout time.Duration) error {
	args := m.Called(ctx, releaseName, revision, timeout)
	return args.Error(0)
}

// MockKubernetesClient is a mock implementation of KubernetesClient for testing
type MockKubernetesClient struct {
	kubernetes.Interface
	mock.Mock
}

// MockLoggerProvider is a mock implementation of LoggerProvider for testing
type MockLoggerProvider struct {
	mock.Mock
	logs []LogEntry
}

type LogEntry struct {
	Level   string
	Message string
	KeyVals []interface{}
}

func (m *MockLoggerProvider) Debug(msg string, keyvals ...interface{}) {
	m.logs = append(m.logs, LogEntry{Level: "debug", Message: msg, KeyVals: keyvals})
	m.Called(msg, keyvals)
}

func (m *MockLoggerProvider) Info(msg string, keyvals ...interface{}) {
	m.logs = append(m.logs, LogEntry{Level: "info", Message: msg, KeyVals: keyvals})
	m.Called(msg, keyvals)
}

func (m *MockLoggerProvider) Warn(msg string, keyvals ...interface{}) {
	m.logs = append(m.logs, LogEntry{Level: "warn", Message: msg, KeyVals: keyvals})
	m.Called(msg, keyvals)
}

func (m *MockLoggerProvider) Error(msg string, keyvals ...interface{}) {
	m.logs = append(m.logs, LogEntry{Level: "error", Message: msg, KeyVals: keyvals})
	m.Called(msg, keyvals)
}

func (m *MockLoggerProvider) Fatal(msg string, keyvals ...interface{}) {
	m.logs = append(m.logs, LogEntry{Level: "fatal", Message: msg, KeyVals: keyvals})
	m.Called(msg, keyvals)
}

func (m *MockLoggerProvider) With(keyvals ...interface{}) LoggerProvider {
	args := m.Called(keyvals)
	return args.Get(0).(LoggerProvider)
}

func (m *MockLoggerProvider) GetLogs() []LogEntry {
	return m.logs
}

func TestRuntimeWithDependencyInjection(t *testing.T) {
	t.Run("should use injected dependencies", func(t *testing.T) {
		// Arrange
		mockHelm := &MockHelmClient{}
		mockK8s := &MockKubernetesClient{}
		mockLogger := &MockLoggerProvider{}

		helmFactory := func(r *Runtime) (HelmClient, error) {
			return mockHelm, nil
		}

		k8sFactory := func(r *Runtime) (KubernetesClient, error) {
			return mockK8s, nil
		}

		runtime := New(
			WithHelmFactory(helmFactory),
			WithKubernetesFactory(k8sFactory),
			WithLogger(mockLogger),
		)

		// Act
		helmClient, err := runtime.Helm()
		k8sClient, err2 := runtime.Kubernetes()

		// Assert
		assert.NoError(t, err)
		assert.NoError(t, err2)
		assert.Equal(t, mockHelm, helmClient)
		assert.Equal(t, mockK8s, k8sClient)
	})

	t.Run("should use default configuration values", func(t *testing.T) {
		// Arrange & Act
		runtime := New()

		// Assert
		assert.Equal(t, DefaultStorageDriver, runtime.storageDriver)
		assert.Equal(t, DefaultTimeout, runtime.timeout)
	})

	t.Run("should override configuration with options", func(t *testing.T) {
		// Arrange
		customTimeout := 5 * time.Minute
		customNamespace := "test-namespace"

		// Act
		runtime := New(
			WithTimeout(customTimeout),
			WithNamespace(customNamespace),
			WithStorageDriver(HelmStorageDriverConfigMap),
		)

		// Assert
		assert.Equal(t, customTimeout, runtime.timeout)
		assert.Equal(t, customNamespace, runtime.namespace)
		assert.Equal(t, HelmStorageDriverConfigMap, runtime.storageDriver)
	})

	t.Run("should memoize clients", func(t *testing.T) {
		// Arrange
		mockHelm := &MockHelmClient{}
		callCount := 0

		helmFactory := func(r *Runtime) (HelmClient, error) {
			callCount++
			return mockHelm, nil
		}

		runtime := New(WithHelmFactory(helmFactory))

		// Act
		client1, err1 := runtime.Helm()
		client2, err2 := runtime.Helm()

		// Assert
		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.Equal(t, client1, client2)
		assert.Equal(t, 1, callCount, "Factory should be called only once due to memoization")
	})

	t.Run("should handle factory errors", func(t *testing.T) {
		// Arrange
		expectedError := errors.New("helm factory error")
		helmFactory := func(r *Runtime) (HelmClient, error) {
			return nil, expectedError
		}

		runtime := New(WithHelmFactory(helmFactory))

		// Act
		client, err := runtime.Helm()

		// Assert
		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "failed to create helm client")
	})
}

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
				result := ValidateTimeout(tt.timeout)
				assert.Equal(t, tt.expected, result)
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
				result := ValidateStorageDriver(tt.driver)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

func TestRuntimeProvider(t *testing.T) {
	t.Run("should implement RuntimeProvider interface", func(t *testing.T) {
		// Arrange
		runtime := New()

		// Act & Assert - This test verifies interface compliance
		var provider RuntimeProvider = runtime
		assert.NotNil(t, provider)

		// Test interface methods exist
		assert.NotNil(t, provider.DebugKeepTempChart)
		assert.NotNil(t, provider.Close)
	})

	t.Run("should handle context operations", func(t *testing.T) {
		// Arrange
		runtime := New()
		ctx := context.Background()

		// Act
		ctxWithRuntime := WithRuntime(ctx, runtime)
		retrievedRuntime := FromRuntime(ctxWithRuntime)

		// Assert
		assert.Equal(t, runtime, retrievedRuntime)
	})

	t.Run("should return nil for context without runtime", func(t *testing.T) {
		// Arrange
		ctx := context.Background()

		// Act
		retrievedRuntime := FromRuntime(ctx)

		// Assert
		assert.Nil(t, retrievedRuntime)
	})
}

func TestIntegrationWithMocks(t *testing.T) {
	t.Run("should demonstrate full workflow with mocks", func(t *testing.T) {
		// Arrange
		mockHelm := &MockHelmClient{}
		mockLogger := &MockLoggerProvider{}

		// Set up expectations
		testManifest := &manifest.Manifest{
			Project: "test-project",
		}

		mockHelm.On("InstallApp", mock.Anything, testManifest, "production", false).Return(nil)
		mockLogger.On("Info", mock.AnythingOfType("string"), mock.Anything).Return()

		// Create runtime with mocks
		runtime := New(
			WithHelmFactory(func(r *Runtime) (HelmClient, error) {
				return mockHelm, nil
			}),
			WithLogger(mockLogger),
		)

		// Act
		helmClient, err := runtime.Helm()
		assert.NoError(t, err)

		err = helmClient.InstallApp(context.Background(), testManifest, "production", false)

		// Assert
		assert.NoError(t, err)
		mockHelm.AssertExpectations(t)
	})
}
