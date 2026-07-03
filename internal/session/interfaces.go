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
	"time"

	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// HelmClient defines the interface for Helm operations.
// It is kept in this package so [WithHelmFactory] tests can inject a mock
// without importing the concrete helm package.
type HelmClient interface {
	// IsReachable checks whether the configured Kubernetes cluster is reachable.
	IsReachable() error

	// InstallApp installs or upgrades an application using Helm. When resolved
	// is non-nil, TLS and hostname values are sourced from it rather than the
	// raw spec.
	InstallApp(ctx context.Context, manifest *spec.Spec, environment string, dryRun bool, resolved *spec.ResolvedSpec) error

	// DeleteRelease uninstalls a Helm release. When wait is true the call
	// blocks until all resources are fully removed using the legacy polling
	// strategy with foreground cascade deletion.
	DeleteRelease(ctx context.Context, project, environment string, wait bool) error

	// GetRelease retrieves information about a specific release.
	GetRelease(ctx context.Context, project, environment string) (*v1.Release, error)

	// ListReleases returns a list of releases matching the given selector.
	ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error)

	// GetReleaseHistory returns the history of a specific release.
	GetReleaseHistory(ctx context.Context, project, environment string) ([]*v1.Release, error)

	// RollbackRelease rolls back a release to a previous revision.
	RollbackRelease(ctx context.Context, releaseName string, revision int, timeout time.Duration) error
}
