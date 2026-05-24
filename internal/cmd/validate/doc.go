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

// Package validate implements the deployah validate command.
//
// The command loads deployah.yaml, applies schema validation and component
// checks, and reports configuration errors before deploy or delete run against
// a cluster.
//
// Register the command with [Register] on a [nabat.dev/nabat.App] instance.
package validate
