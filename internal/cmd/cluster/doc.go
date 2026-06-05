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

// Package cluster provides the "deployah cluster" command group, which manages
// a single local Kubernetes cluster for development.
//
// It is an opinionated, single-cluster facade over the internal/localkube
// package: every subcommand operates on one fixed cluster (clusterName), so
// developers never type a cluster name. The cluster lifecycle is intentionally
// decoupled from deployah environments; the bridge between "I created a local
// cluster" and "deployah deploy targets it" is the Kubernetes context
// (kind-<clusterName>), selectable via the global --context flag or an
// environment's "context" field.
package cluster
