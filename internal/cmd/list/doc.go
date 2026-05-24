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

// Package list implements the deployah list command.
//
// The command shows Helm releases and manifest components for the current
// project, optionally filtered by environment. Output is available as a table
// or structured JSON/YAML through shared CLI rendering helpers.
//
// Register the command with [Register] on a [nabat.dev/nabat.App] instance.
package list
