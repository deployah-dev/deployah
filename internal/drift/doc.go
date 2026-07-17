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

// Package drift detects cluster drift: changes made to live resources
// outside of Deployah that Deployah's own spec-edit plan would not
// otherwise report.
//
// [Client] predicts each resource via a server-side apply dry-run and
// fetches its live state. [ComputeDrift] diffs the two with
// [deployah.dev/deployah/internal/plan.ComputeDiff] and subtracts every
// field path already explained by the spec-edit plan, so only changes the
// cluster picked up on its own remain. This backs `deployah plan --drift`:
//
//	A = diff(render, last successful release)   # the spec edit
//	B = diff(predicted, live)                    # total delta
//	drift = B minus paths(A)                     # per resource, per field path
package drift
