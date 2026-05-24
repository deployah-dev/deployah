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

// Package manifest parses, validates, and manipulates Deployah manifest files.
//
// It loads YAML manifests, resolves environments and env files, applies
// schema defaults, substitutes variables, and validates against embedded
// JSON schemas.
//
// # Loading and saving
//
//   - [Load]: read a manifest, resolve an environment, substitute variables
//   - [Save]: write a manifest to YAML
//
// # Validation
//
//   - [ValidateManifest]: validate manifest data against a schema version
//   - [ValidateEnvironments]: validate environment definitions
//   - [ValidateManifestComponents]: check component resources and autoscaling
//
// # Defaults
//
//   - [FillManifestWithDefaults]: apply schema defaults to a [Manifest]
//   - [CreateManifestWithDefaults]: create a new manifest with defaults
package manifest
