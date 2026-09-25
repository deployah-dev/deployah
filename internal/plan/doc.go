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

// Package plan computes and renders a preview of the changes a deploy
// would make. [BuildPlan] diffs the rendered manifest against the last
// successful Helm release. [BuildSemanticPlan] renders Desired state,
// derives whether Helm runs from Previous and Desired release intent,
// and predicts write consequences only when Helm will actually run.
//
// [ComputeDiff] is the legacy diff engine: it parses two rendered
// Kubernetes manifests, matches resources by (apiVersion, kind, namespace,
// name), and runs [github.com/homeport/dyff] field-by-field on resources
// present on both sides. [Plan] is the resulting domain model, consumed by
// a text renderer ([RenderText]) and a JSON renderer ([NewJSONDocument]).
// [DeploymentIntent] holds the mutation and executability flags a deploy
// would use. [BuildPlan] and [ComputeDiff] remain during migration to the
// semantic plan model.
//
// [BuildSemanticPlan] renders Desired state through
// [SemanticBuildClient.RenderManifestsWithPrep]. Whether Helm runs comes
// from the Helm operation and a comparison of the previous release
// (prep.Current) with the rendered manifest and hooks. Live state and
// predictor output do not take part in that decision. When Helm will not
// run, Namespace and Helm write prediction are skipped, and the plan has
// no Helm write consequences. When Helm will install or upgrade, the
// existing Namespace and [deployah.dev/deployah/internal/predict.Predict]
// bridge still produces those consequences. Chart CRD files are forwarded
// to Helm only. It returns a
// [deployah.dev/deployah/internal/plan/semantic.Plan]. Semantic types
// live in plan/semantic. New rendering lives in plan/view.
//
// Rendering the chart itself lives on [deployah.dev/deployah/internal/helm.Client]
// instead, since `deployah plan` and `deployah deploy` share that one
// rendering engine.
package plan
