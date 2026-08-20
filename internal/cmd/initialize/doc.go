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

// Package initialize implements the deployah init command.
//
// Init is a TTY-only wizard: it walks through project name, environments,
// and components, then writes a sparse deployah.yaml (schema defaults
// omitted), merges missing keys into the platform file, and scaffolds
// .deployah extras directories. Dry-run mode previews the spec without
// writing files. There is no non-interactive defaults path.
//
// Register the command with [Register] on a [nabat.dev/nabat.App] instance.
package initialize
