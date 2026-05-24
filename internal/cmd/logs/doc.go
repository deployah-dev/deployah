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

// Package logs implements the deployah logs command.
//
// The command streams or tails pod logs for components deployed by Deployah,
// using Kubernetes selectors derived from Helm release metadata.
//
// Register the command with [Register] on a [nabat.dev/nabat.App] instance.
package logs
