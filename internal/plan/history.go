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

package plan

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"helm.sh/helm/v4/pkg/release/common"

	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// historyClient is the subset of
// [deployah.dev/deployah/internal/session.HelmClient] that
// [LastSuccessfulRelease] needs. Depending on this narrower interface, rather
// than the full HelmClient, keeps this file's tests free of unrelated methods
// like DeleteRelease or RollbackRelease.
type historyClient interface {
	GetReleaseHistory(ctx context.Context, project, environment string) ([]*v1.Release, error)
}

// LastSuccessfulRelease walks a release's history, newest revision first,
// and returns the newest revision whose status is "deployed" or
// "superseded": the manifest a plan should diff the current render
// against. warning is set when the newest revision itself isn't
// successful, so the caller can surface that alongside the older
// successful revision actually used for the diff.
func LastSuccessfulRelease(ctx context.Context, client historyClient, project, environment string) (release *v1.Release, warning string, err error) {
	history, err := client.GetReleaseHistory(ctx, project, environment)
	if err != nil {
		if errors.Is(err, helm.ErrReleaseNotFound) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("fetching release history: %w", err)
	}

	// The Kubernetes API (backing Helm's secret/configmap storage drivers)
	// does not guarantee list order, so history must be sorted explicitly:
	// newest revision (highest Version) first.
	releases := slices.Clone(history)
	slices.SortFunc(releases, func(a, b *v1.Release) int {
		return b.Version - a.Version
	})

	if len(releases) == 0 {
		// No history at all: treat as a fresh install, where every
		// resource in the current render is an addition.
		return nil, "", nil
	}

	latest := releases[0]
	if status := latest.Info.Status; status != common.StatusDeployed && status != common.StatusSuperseded {
		// Newest revision isn't itself successful (failed, or an
		// install/upgrade/rollback still in progress): warn so the caller
		// can surface this alongside the older successful revision below.
		warning = fmt.Sprintf("latest revision %d is %s; comparing against the last successful revision instead", latest.Version, status)
	}

	for _, rel := range releases {
		if rel.Info.Status == common.StatusDeployed || rel.Info.Status == common.StatusSuperseded {
			return rel, warning, nil
		}
	}

	// History exists but no revision ever succeeded (e.g. every attempt
	// failed). Treat like a fresh install for diffing purposes, but keep
	// the warning so the caller knows this is not really a first deploy.
	if warning == "" {
		warning = fmt.Sprintf("no successful revision found in history (latest revision %d is %s)", latest.Version, latest.Info.Status)
	}
	return nil, warning, nil
}
