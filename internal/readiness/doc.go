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

// Package readiness computes per-component pod readiness for a Helm
// release. [Poll] is the single source of truth, backing both the
// live-updating summary [deployah.dev/deployah/internal/cmd/deploy.DeployWatcher]
// prints during a deploy and the one-shot summary printed when a deploy is
// skipped because nothing changed.
package readiness
