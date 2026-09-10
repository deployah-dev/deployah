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

package target

import (
	"k8s.io/client-go/rest"
)

// defaultNamespace is used when no override or kubeconfig context
// namespace is available.
const defaultNamespace = "default"

// ContextSource identifies which rule selected [Target.Context].
type ContextSource uint8

const (
	// ContextSourceKubeconfig means the kubeconfig current-context was used.
	ContextSourceKubeconfig ContextSource = iota
	// ContextSourcePlatform means the caller supplied a platform context.
	ContextSourcePlatform
	// ContextSourceExplicit means an explicit context override was set.
	ContextSourceExplicit
)

// Target is an immutable resolved Kubernetes destination.
//
// After [Resolver.Resolve], Context, ContextSource, Namespace, and the
// kubeconfig loading snapshot do not change. Concurrent calls to the
// getters, [Target.ClientConfig], and [Target.RESTConfig] are safe.
type Target struct {
	contextName   string
	contextSource ContextSource
	namespace     string
	loading       loadingSnapshot
}

// Context returns the effective Kubernetes context name.
// An empty string means no context could be resolved.
func (t *Target) Context() string {
	if t == nil {
		return ""
	}
	return t.contextName
}

// ContextSource returns which rule selected [Target.Context].
func (t *Target) ContextSource() ContextSource {
	if t == nil {
		return ContextSourceKubeconfig
	}
	return t.contextSource
}

// Namespace returns the effective namespace.
// Precedence is explicit override, then the selected kubeconfig context
// namespace, then "default".
func (t *Target) Namespace() string {
	if t == nil || t.namespace == "" {
		return defaultNamespace
	}
	return t.namespace
}

// RESTConfig returns a kubeconfig-based REST config for this destination.
//
// It uses [Target.ClientConfig]. When Context is non-empty, the config pins
// that context. Namespace is pinned to [Target.Namespace]. Loading rules
// come from the snapshot taken at Resolve, not from the current process
// environment. It does not use in-cluster configuration. Load errors from
// the snapshotted rules are returned; kubeconfig file contents are read at
// call time.
func (t *Target) RESTConfig() (*rest.Config, error) {
	return t.ClientConfig().ClientConfig()
}
