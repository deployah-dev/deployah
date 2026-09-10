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

// Package session holds per-command Kubernetes and Helm client state.
//
// [Session] is the invocation handle for one CLI run. It carries global
// options and travels through [context.Context] so every command shares one
// configured environment. Construct one with [New], attach it via
// [WithContext], and retrieve it in handlers through [FromContext].
//
// Spec and platform source loading is owned by [workspace.Workspace].
// Call [Session.Workspace] to load those sources. Kubernetes destination
// resolution is delegated to [target.Resolver]. Call [Session.Target] with
// the environment name to obtain a [Cluster] that holds the resolved
// [target.Target], a [HelmConfig] snapshot, client factories, and lazily
// constructed Helm and Kubernetes clients. Cluster does not embed
// [Session]. Cluster REST, Kubernetes, and Helm construction use the
// Target kubeconfig destination.
package session
