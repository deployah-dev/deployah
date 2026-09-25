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

// Package render holds the client-side chart render result shared by the
// Helm engine, session's [deployah.dev/deployah/internal/session.HelmClient],
// and plan/deploy callers. Keeping [RenderResult] here avoids a dependency
// cycle (session constructs helm clients; helm must not import session)
// while letting session expose render methods without importing helm.
package render

import (
	"slices"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// RenderResult is the client-side render of a chart for one project and
// environment, used both to diff against the last release (`deployah plan`)
// and, on match, as input to the real install/upgrade (`deployah deploy`).
type RenderResult struct {
	// ReleaseName is the Helm release name computed for project/environment.
	ReleaseName string
	// Namespace is the target namespace for the release.
	Namespace string
	// ChartPath is the prepared chart directory backing this render. It
	// stays valid until the caller runs the cleanup func returned alongside
	// this result.
	ChartPath string
	// Manifest is the rendered, "---"-concatenated Kubernetes YAML.
	Manifest string
	// Hooks are the Helm hooks declared by the chart for this release.
	Hooks []*v1.Hook
	// Previous is the declarative Helm release this render upgrades from.
	// It is nil on a fresh install. On an upgrade it is Helm's current
	// upgrade baseline, not the newest revision and not the last
	// successful release.
	Previous *ReleaseIntent
	// IsUpgrade is true when an existing release was found and the render
	// used the upgrade action, so Revision is that release's version plus
	// one. It is false for a fresh install, where Revision is always 1.
	IsUpgrade bool
	// Revision is the release revision this render corresponds to.
	Revision int
}

// ReleaseIntent is the declarative part of a Helm release used to compare
// a previous release with the desired render. It has no runtime state such
// as status, revision, or hook execution.
type ReleaseIntent struct {
	// Manifest is the rendered, "---"-concatenated Kubernetes YAML.
	Manifest string
	// Hooks are the chart hooks for this release, in release order. A nil
	// entry is kept so comparison can fail closed.
	Hooks []*HookIntent
}

// HookIntent is the declarative part of one Helm hook. It omits execution
// state such as LastRun.
type HookIntent struct {
	// Name is the hook name.
	Name string
	// Kind is the Kubernetes kind of the hook resource.
	Kind string
	// Path is the chart-relative path of the hook template.
	Path string
	// Manifest is the rendered hook YAML.
	Manifest string
	// Events are the Helm hook events, in release order.
	Events []v1.HookEvent
	// Weight is the hook sort weight.
	Weight int
	// DeletePolicies say when Helm deletes the hook resource.
	DeletePolicies []v1.HookDeletePolicy
	// OutputLogPolicies say when hook logs are copied to the main process.
	OutputLogPolicies []v1.HookOutputLogPolicy
}

// HookIntents copies the declarative fields of hooks, in order. A nil hook
// stays nil. Slice fields are cloned. LastRun is left behind.
func HookIntents(hooks []*v1.Hook) []*HookIntent {
	if hooks == nil {
		return nil
	}
	out := make([]*HookIntent, 0, len(hooks))
	for _, h := range hooks {
		if h == nil {
			out = append(out, nil)
			continue
		}
		out = append(out, &HookIntent{
			Name:              h.Name,
			Kind:              h.Kind,
			Path:              h.Path,
			Manifest:          h.Manifest,
			Events:            slices.Clone(h.Events),
			Weight:            h.Weight,
			DeletePolicies:    slices.Clone(h.DeletePolicies),
			OutputLogPolicies: slices.Clone(h.OutputLogPolicies),
		})
	}
	return out
}

// Hook returns a Helm hook with this intent's declarative fields. Slice
// fields are cloned. A nil intent returns nil.
func (h *HookIntent) Hook() *v1.Hook {
	if h == nil {
		return nil
	}
	return &v1.Hook{
		Name:              h.Name,
		Kind:              h.Kind,
		Path:              h.Path,
		Manifest:          h.Manifest,
		Events:            slices.Clone(h.Events),
		Weight:            h.Weight,
		DeletePolicies:    slices.Clone(h.DeletePolicies),
		OutputLogPolicies: slices.Clone(h.OutputLogPolicies),
	}
}
