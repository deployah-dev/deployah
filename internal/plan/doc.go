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

// Package plan computes and renders a preview of the changes a deploy would
// make, comparing the rendered manifest for an environment against the
// manifest stored in the last successful Helm release.
//
// [ComputeDiff] is the diff engine: it parses two rendered Kubernetes
// manifests, matches resources by (apiVersion, kind, namespace, name), and
// runs [github.com/homeport/dyff] field-by-field on resources present on
// both sides. [Plan] is the resulting domain model, consumed by a text
// renderer ([RenderText]) and a JSON renderer ([NewJSONDocument]).
//
// Rendering the chart itself lives on [deployah.dev/deployah/internal/helm.Client]
// instead, since `deployah plan` and `deployah deploy` share that one
// rendering engine.
package plan
