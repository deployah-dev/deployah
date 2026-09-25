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
// decides whether Helm runs by comparing the previous release with that
// render, and predicts writes only when Helm will run.
//
// [ComputeDiff] is the older diff: it parses two rendered Kubernetes
// manifests, matches resources by (apiVersion, kind, namespace, name), and
// runs [github.com/homeport/dyff] field by field on resources present on
// both sides. [Plan] is that model. [RenderText] prints it, and
// [NewJSONDocument] encodes it as JSON. [DeploymentIntent] holds the flags
// a deploy uses to decide what it may change. [BuildPlan] and
// [ComputeDiff] stay while callers move to the semantic plan.
//
// [BuildSemanticPlan] renders with
// [SemanticBuildClient.RenderManifestsWithPrep], then picks install,
// upgrade, or none from the Helm operation and a comparison of the
// previous release (prep.Current) with the rendered manifest and hooks.
// Live state and predictor output do not make that choice. If Helm will
// not run, Namespace and Helm write prediction are skipped, so the plan
// has no Helm writes. If Helm will install or upgrade, Namespace
// prediction and [deployah.dev/deployah/internal/predict.Predict] still
// produce those writes. Chart CRDs are passed through to Helm. The result
// is a [deployah.dev/deployah/internal/plan/semantic.Plan]. Semantic types
// live in plan/semantic. New rendering lives in plan/view.
//
// Chart rendering is on [deployah.dev/deployah/internal/helm.Client].
// `deployah plan` and `deployah deploy` share that engine.
package plan
