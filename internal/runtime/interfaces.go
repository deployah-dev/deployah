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

	"github.com/deployah-dev/deployah/internal/manifest"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// RuntimeProvider defines the interface for runtime dependency management.
// This interface enables better testability by allowing mock implementations
// and provides a clear contract for runtime services.
type RuntimeProvider interface {
	// Helm returns a configured Helm client for Kubernetes operations
	Helm() (HelmClient, error)

	// Kubernetes returns a configured Kubernetes clientset
	Kubernetes() (KubernetesClient, error)

	// Manifest loads and returns the parsed manifest for the given environment
	Manifest(ctx context.Context, environment string) (*manifest.Manifest, error)

	// DebugKeepTempChart returns whether temporary chart directories should be kept
	DebugKeepTempChart() bool

	// Close performs cleanup of resources held by the runtime
	Close() error
}

// HelmClient defines the interface for Helm operations.
// This abstraction allows for easier testing and potential alternative implementations.
type HelmClient interface {
	// InstallApp installs or upgrades an application using Helm
	InstallApp(ctx context.Context, manifest *manifest.Manifest, environment string, dryRun bool) error

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

// ManifestLoader defines the interface for manifest loading operations.
// This enables testing with mock manifest loaders and different loading strategies.
type ManifestLoader interface {
	// Load reads and parses a manifest file
	Load(ctx context.Context, path string, envName string) (*manifest.Manifest, error)

	// Save writes a manifest to a file
	Save(manifest *manifest.Manifest, path string) error

	// Validate validates a manifest against its schema
	Validate(manifest *manifest.Manifest) error
}

// LoggerProvider defines the interface for logging operations.
// This enables structured logging with different implementations and levels.
type LoggerProvider interface {
	// Debug logs a debug-level message
	Debug(msg string, keyvals ...interface{})

	// Info logs an info-level message
	Info(msg string, keyvals ...interface{})

	// Warn logs a warning-level message
	Warn(msg string, keyvals ...interface{})

	// Error logs an error-level message
	Error(msg string, keyvals ...interface{})

	// Fatal logs a fatal-level message and exits
	Fatal(msg string, keyvals ...interface{})

	// With returns a new logger with the given key-value pairs
	With(keyvals ...interface{}) LoggerProvider
}
