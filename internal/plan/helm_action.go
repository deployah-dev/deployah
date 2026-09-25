// Copyright 2026 The Deployah Authors
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
	"errors"
	"fmt"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// deriveHelmAction picks install, upgrade, or none by comparing
// prep.Current with the rendered manifest and hooks. Live state never
// affects the result.
func deriveHelmAction(prep helm.ReleasePrep, desiredManifest string, desiredHooks []*v1.Hook) (semantic.HelmAction, error) {
	switch prep.Operation {
	case helm.OperationInstall:
		return semantic.HelmInstall, nil
	case helm.OperationUpgrade:
		if prep.Current == nil {
			return 0, errors.New("upgrade prep requires a current release")
		}
		changed, err := releaseIntentChanged(prep.Current.Manifest, prep.Current.Hooks, desiredManifest, desiredHooks)
		if err != nil {
			return 0, err
		}
		if changed {
			return semantic.HelmUpgrade, nil
		}
		return semantic.HelmNone, nil
	default:
		return 0, fmt.Errorf("invalid helm operation %d", prep.Operation)
	}
}
