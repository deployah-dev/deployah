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
	"deployah.dev/deployah/internal/render"

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

// HelmActionFromRender picks install, upgrade, or none for result using
// the same release-intent comparison as [BuildSemanticPlan]. It returns
// an error when result is nil or an upgrade render has no Previous.
func HelmActionFromRender(result *render.RenderResult) (semantic.HelmAction, error) {
	if result == nil {
		return 0, errors.New("render result is required")
	}
	prep := helm.ReleasePrep{Operation: helm.OperationInstall}
	if result.IsUpgrade {
		if result.Previous == nil {
			return 0, errors.New("upgrade render requires a previous release intent")
		}
		prep.Operation = helm.OperationUpgrade
		prep.Current = &v1.Release{
			Manifest: result.Previous.Manifest,
			Hooks:    releaseHooks(result.Previous.Hooks),
		}
	}
	return deriveHelmAction(prep, result.Manifest, result.Hooks)
}

// releaseHooks rebuilds temporary Helm hooks from declarative intents.
// Nil entries stay nil.
func releaseHooks(hooks []*render.HookIntent) []*v1.Hook {
	if hooks == nil {
		return nil
	}
	out := make([]*v1.Hook, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, h.Hook())
	}
	return out
}
