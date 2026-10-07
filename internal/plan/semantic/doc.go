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

// Package semantic is the unredacted model of one deployment plan.
//
// [New] is the only constructor. It ignores a caller's [TaskPlan.WillRun]
// and derives that flag from the Helm action and the task. This
// package does not render charts and does not read the cluster.
//
// Snapshots and field values stay complete. Serialization and secret
// redaction belong to deployah.dev/deployah/internal/plan/view.
// [deployah.dev/deployah/internal/plan/view.WriteJSON] is the only
// machine-readable output. encoding/json of these types is not a
// supported output path.
package semantic
