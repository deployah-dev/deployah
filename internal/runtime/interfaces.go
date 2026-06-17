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
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// RuntimeProvider defines the interface for runtime dependency management.
// This interface enables better testability by allowing mock implementations
// and provides a clear contract for runtime services.
type RuntimeProvider interface {
	// Helm returns a configured Helm client for Kubernetes operations
	Helm() (HelmClient, error)

	// Kubernetes returns a configured Kubernetes clientset
	Kubernetes() (KubernetesClient, error)

	// Spec loads and returns the parsed spec for the given environment
	Spec(ctx context.Context, environment string) (*spec.Spec, error)

	// DebugKeepTempChart returns whether temporary chart directories should be kept
	DebugKeepTempChart() bool

	// Close performs cleanup of resources held by the runtime
	Close() error
}

// HelmClient defines the interface for Helm operations.
// This abstraction allows for easier testing and alternative implementations.
type HelmClient interface {
	// IsReachable checks whether the configured Kubernetes cluster is reachable.
	IsReachable() error

	// InstallApp installs or upgrades an application using Helm
	InstallApp(ctx context.Context, manifest *spec.Spec, environment string, dryRun bool) error

	// DeleteRelease uninstalls a Helm release
	DeleteRelease(ctx context.Context, project, environment string) error

	// GetRelease retrieves information about a specific release
	GetRelease(ctx context.Context, project, environment string) (*v1.Release, error)

	// ListReleases returns a list of releases matching the given selector
	ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error)

	// GetReleaseHistory returns the history of a specific release
	GetReleaseHistory(ctx context.Context, project, environment string) ([]*v1.Release, error)

	// RollbackRelease rolls back a release to a previous revision
	RollbackRelease(ctx context.Context, releaseName string, revision int, timeout time.Duration) error
}

// KubernetesClient defines the interface for Kubernetes operations.
// This abstraction enables testing with mock Kubernetes clients.
type KubernetesClient interface {
	kubernetes.Interface
}

// SpecLoader defines the interface for spec loading operations.
// This enables testing with mock loaders and different loading strategies.
type SpecLoader interface {
	// Load reads and parses a spec file
	Load(ctx context.Context, path, envName string) (*spec.Spec, error)

	// Save writes a spec to a file
	Save(manifest *spec.Spec, path string) error

	// Validate validates a spec against its schema
	Validate(manifest *spec.Spec) error
}

// LoggerProvider defines the interface for logging operations.
// This enables structured logging with different implementations and levels.
type LoggerProvider interface {
	// Debug logs a debug-level message
	Debug(msg string, keyvals ...any)

	// Info logs an info-level message
	Info(msg string, keyvals ...any)

	// Warn logs a warning-level message
	Warn(msg string, keyvals ...any)

	// Error logs an error-level message
	Error(msg string, keyvals ...any)

	// Fatal logs a fatal-level message and exits
	Fatal(msg string, keyvals ...any)

	// With returns a new logger with the given key-value pairs
	With(keyvals ...any) LoggerProvider
}
