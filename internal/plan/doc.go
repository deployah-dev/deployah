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
// are Previous to Desired. [RESTMapper] supplies scope only. An
// existing release reads Live through [LiveReader] with GET and LIST,
// so Drift is Previous to Live. Chart CRD lifecycle comes from the
// loaded documents, not from object diffs. The result is a
// [deployah.dev/deployah/internal/plan/semantic.Plan] from
// [deployah.dev/deployah/internal/plan/semantic.New]. That constructor
// derives whether each task will run. Task assembly in this package
// leaves WillRun false. Those types live in plan/semantic. Their
// rendering lives in plan/view.
//
// [LastSuccessfulRelease] remains for deploy hostname and workload
// guards. [BuildSemanticPlan] does not use it.
//
// Chart rendering is on [deployah.dev/deployah/internal/helm.Client].
// `deployah plan` and `deployah deploy` share it.
package plan
