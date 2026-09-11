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
	"errors"
	"fmt"

	"helm.sh/helm/v4/pkg/release/common"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ErrNoDeployedRevision is returned by [PrepareRelease] when history exists
// but Helm would not upgrade: the newest revision is not pending, no
// revision is deployed, and the newest status is not failed or superseded.
var ErrNoDeployedRevision = errors.New("no deployed revision to upgrade from")

// Operation is whether Helm would install or upgrade given release history.
type Operation int

const (
	// OperationInstall is a first install: no history exists.
	OperationInstall Operation = iota + 1
	// OperationUpgrade is an upgrade of an existing release, including a
	// failed-only history with no deployed revision.
	OperationUpgrade
)

// ApplyMethod is how Helm would apply Kubernetes resources.
type ApplyMethod string

const (
	// ApplyMethodSSA is Helm server-side apply ("ssa").
	ApplyMethodSSA ApplyMethod = "ssa"
	// ApplyMethodCSA is Helm client-side apply ("csa").
	ApplyMethodCSA ApplyMethod = "csa"
)

// ReleasePrep is the install-or-upgrade decision Helm's prepareUpgrade
// would make from release history. Newest and Current can differ; callers
// must use those releases' Version fields, not a generic revision.
type ReleasePrep struct {
	// Operation is install when history is empty, otherwise upgrade.
	Operation Operation
	// Newest is Helm's last revision (highest Version), or nil on install.
	Newest *v1.Release
	// Current is the upgrade manifest baseline, or nil on install.
	Current *v1.Release
	// ApplyMethod is SSA on install, otherwise derived from Newest.
	ApplyMethod ApplyMethod
	// NextRevision is 1 on install, otherwise Newest.Version+1.
	NextRevision int
}

// PrepareRelease decides install vs upgrade from history, matching Helm
// v4.3.0 prepareUpgrade. It does not mutate history.
//
// An empty or nil slice is an install. History-fetch errors such as
// [ErrReleaseNotFound] stay at the caller: treat not-found as empty
// history, then call PrepareRelease.
//
// It returns [ErrReleasePending] when the newest revision is pending, and
// [ErrNoDeployedRevision] when Helm would refuse the upgrade.
func PrepareRelease(history []*v1.Release) (ReleasePrep, error) {
	newest := newestRelease(history)
	if newest == nil {
		return ReleasePrep{
			Operation:    OperationInstall,
			ApplyMethod:  ApplyMethodSSA,
			NextRevision: 1,
		}, nil
	}
	if newest.Info == nil {
		return ReleasePrep{}, fmt.Errorf("newest revision %d has no status", newest.Version)
	}
	if newest.Info.Status.IsPending() {
		return ReleasePrep{}, fmt.Errorf("revision %d: %w", newest.Version, ErrReleasePending)
	}

	current, err := currentUpgradeBaseline(history, newest)
	if err != nil {
		return ReleasePrep{}, err
	}

	return ReleasePrep{
		Operation:    OperationUpgrade,
		Newest:       newest,
		Current:      current,
		ApplyMethod:  ApplyMethodFor(OperationUpgrade, newest.ApplyMethod),
		NextRevision: newest.Version + 1,
	}, nil
}

// ApplyMethodFor returns the apply method Helm would use. Install is always
// SSA. Upgrade with Helm's "auto" ServerSideApply is SSA only when
// newestApplyMethod is "ssa"; empty, "csa", and any other value are CSA.
func ApplyMethodFor(op Operation, newestApplyMethod string) ApplyMethod {
	if op == OperationInstall {
		return ApplyMethodSSA
	}
	if newestApplyMethod == string(ApplyMethodSSA) {
		return ApplyMethodSSA
	}
	return ApplyMethodCSA
}

// currentUpgradeBaseline returns Helm's current release for an upgrade:
// Newest when it is deployed; otherwise the highest-version deployed
// revision; otherwise Newest when it is failed or superseded.
func currentUpgradeBaseline(history []*v1.Release, newest *v1.Release) (*v1.Release, error) {
	if newest.Info.Status == common.StatusDeployed {
		return newest, nil
	}
	if deployed := newestDeployed(history); deployed != nil {
		return deployed, nil
	}
	switch newest.Info.Status {
	case common.StatusFailed, common.StatusSuperseded:
		return newest, nil
	default:
		return nil, fmt.Errorf("newest revision %d is %s: %w", newest.Version, newest.Info.Status, ErrNoDeployedRevision)
	}
}

// newestDeployed returns the highest-version deployed release, or nil.
// It does not mutate history.
func newestDeployed(history []*v1.Release) *v1.Release {
	var best *v1.Release
	for _, rel := range history {
		if rel == nil || rel.Info == nil || rel.Info.Status != common.StatusDeployed {
			continue
		}
		if best == nil || rel.Version > best.Version {
			best = rel
		}
	}
	return best
}
