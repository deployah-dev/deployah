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

// Package plan computes and renders read-only deployment plans.
// Planning looks at release changes. It does not decide whether
// `deployah deploy` runs Helm.
//
// [BuildSemanticPlan] renders with
// [SemanticBuildClient.RenderManifestsWithPrep]. It picks install,
// upgrade, or none from the Helm operation and from comparing the
// previous release with the render and its hooks. Resource changes
// compare the previous manifest with the rendered Desired manifest.
// Discovery supplies scope only. An existing release also reads Live
// with GET and LIST to compute Drift. Chart CRDs pass through to Helm.
// The result is a [deployah.dev/deployah/internal/plan/semantic.Plan].
// Those types live in plan/semantic. Their rendering lives in plan/view.
//
// [BuildPlan] diffs the rendered manifest against the last successful
// Helm release. [ComputeDiff] is the older diff. It parses two rendered
// manifests, matches by apiVersion, kind, namespace, and name, and runs
// [github.com/homeport/dyff] on resources present on both sides. [Plan]
// is that model. [RenderText] prints it, and [NewJSONDocument] encodes
// it as JSON. [DeploymentIntent] records resize and hostname flags. It
// does not decide whether deploy runs Helm. [BuildPlan] and [ComputeDiff]
// remain while callers move to the semantic plan.
//
// Chart rendering is on [deployah.dev/deployah/internal/helm.Client].
// `deployah plan` and `deployah deploy` share it.
package plan
