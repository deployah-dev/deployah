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

// Package render holds the chart render that Helm, plan, and
// [deployah.dev/deployah/internal/session.HelmClient] share. The type
// lives here so session can return a render without importing helm, and
// helm does not have to import session to build a client.
package render

import (
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// RenderResult is the client-side render of a chart for one project and
// environment. `deployah plan` diffs it against the last successful
// release.
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
	// IsUpgrade is true when an existing release was found and the render
	// used the upgrade action, so Revision is that release's version plus
	// one. It is false for a fresh install, where Revision is always 1.
	IsUpgrade bool
	// Revision is the release revision this render corresponds to.
	Revision int
}
