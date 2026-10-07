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

// Package plan builds a read-only deployment plan from a rendered
// release and, on upgrade, from live cluster objects.
//
// [BuildSemanticPlan] returns a
// [deployah.dev/deployah/internal/plan/semantic.Plan]. Task assembly
// records what changed. [deployah.dev/deployah/internal/plan/semantic.New]
// decides whether each task will run. Human text and JSON live in
// plan/view.
//
// [LastSuccessfulRelease] remains for deploy hostname and workload
// guards. [BuildSemanticPlan] does not use it.
//
// Chart rendering is on [deployah.dev/deployah/internal/helm.Client].
// `deployah plan` and `deployah deploy` share it.
package plan
