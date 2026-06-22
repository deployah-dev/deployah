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
// [Session] carries the global CLI options (kubeconfig, namespace, spec path,
// etc.) and travels through [context.Context] so every command shares one
// configured environment per invocation. Construct one with [New], attach it
// via [WithContext], and retrieve it in handlers through [FromContext].
//
// To access cluster resources, call [Session.Target] with the target
// environment name. It eagerly resolves the Kubernetes context from the spec's
// environment field and returns a [Cluster] that lazily constructs the Helm
// and Kubernetes clients. This two-phase model makes it a compile-time error
// to request a cluster client without first resolving which cluster to use.
package session
