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

// Package target resolves and represents the Kubernetes destination used by
// a Deployah invocation.
//
// A Target owns Kubernetes context, namespace, and kubeconfig resolution.
// It does not load Deployah specs or platform files and does not construct
// Helm or Kubernetes clients.
//
// # Context precedence
//
//  1. explicit context override (--context)
//  2. platform environment context name supplied to [Resolver.Resolve]
//  3. kubeconfig current-context
//
// [Target.Context] is the effective context name. [Target.ContextSource]
// records which rule selected it. When Context is non-empty,
// [Target.ClientConfig] and [Target.RESTConfig] pin that name so the
// destination cannot change after Resolve. Both use the snapshotted
// kubeconfig loading rules, pin [Target.Namespace], and do not select
// in-cluster configuration.
//
// # Namespace precedence
//
//  1. explicit namespace override
//  2. namespace of the selected kubeconfig context
//  3. "default"
//
// A resolved Target is immutable. Kubeconfig loading rules are snapshotted
// at Resolve time so later KUBECONFIG or HOME changes do not retarget it.
package target
