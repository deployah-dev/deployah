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

// Package runtime holds per-command Kubernetes and Helm client state.
//
// [Runtime] lazily constructs Helm and Kubernetes clients from global CLI
// options, caches the loaded manifest, and travels through [context.Context]
// so every command shares one configured environment per invocation.
//
// Construct a runtime with [New], attach it via [WithContext], and retrieve
// it in handlers through [FromContext].
package runtime
