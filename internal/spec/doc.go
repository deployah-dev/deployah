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

// Package spec parses, validates, and manipulates Deployah spec files.
//
// It loads YAML specs, resolves environments and env files, applies
// schema defaults, substitutes variables, and validates against embedded
// JSON schemas.
//
// # Loading and saving
//
//   - [Load]: read a spec, resolve an environment, substitute variables
//   - [Save]: write a spec to YAML
//
// # Validation
//
//   - [ValidateSpec]: validate spec data against a schema version
//   - [ValidateEnvironments]: validate environment definitions
//   - [ValidateSpecComponents]: check component resources and autoscaling
//
// # Defaults
//
//   - [FillSpecWithDefaults]: apply schema defaults to a [Spec]
//   - [CreateSpecWithDefaults]: create a new spec with defaults
package spec
