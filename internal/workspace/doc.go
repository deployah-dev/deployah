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

// Package workspace owns the Deployah project source locations for one
// invocation and loads the spec and optional platform configuration from
// those sources.
//
// A Workspace does not resolve Kubernetes destinations and does not
// construct Helm or Kubernetes clients.
//
// # Platform source precedence
//
// Source selection is snapshotted in [New]:
//
//  1. explicit [Config.PlatformPath]
//  2. DEPLOYAH_PLATFORM_FILE
//  3. deployah.platform.yaml next to the effective spec
//
// An explicit or environment-selected path is required.
// The default adjacent file is optional: [Workspace.Platform] returns
// (nil, nil) when it is missing.
package workspace
