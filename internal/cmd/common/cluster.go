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

package common

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/spec"
)

// ClusterHint returns an actionable suffix for errors that look like the
// target cluster or context is missing or unreachable. It returns an empty
// string for unrelated errors. Shared by `deployah deploy` and `deployah
// plan` so their connectivity error messages never drift apart.
func ClusterHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context") && (strings.Contains(msg, "does not exist") || strings.Contains(msg, "not found")),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "no configuration has been provided"),
		strings.Contains(msg, "couldn't get current server api group list"):
		return "\n\nHint: the target cluster/context may be unavailable. For a local cluster, run 'deployah cluster up' (and pass --context kind-deployah or set the environment's 'context' field)."
	default:
		return ""
	}
}

// HasExposeComponents reports whether any component in the spec declares an
// expose block, meaning platform resolution is required.
func HasExposeComponents(m *spec.Spec) bool {
	if m == nil {
		return false
	}
	for _, comp := range m.Components {
		if comp.Expose != nil {
			return true
		}
	}
	return false
}

// MaterializeSelfSignedTLS fetches or generates the self-signed TLS
// certificate for every resolved component before any chart render, so all
// renders in the invocation see identical certificate bytes. Fails closed
// when k8sErr is non-nil and a selfSigned component exists, rather than
// rotating a live secret on a transient clientset failure; pass a nil
// k8sClient with a nil k8sErr to force offline generation deliberately.
func MaterializeSelfSignedTLS(ctx context.Context, k8sClient kubernetes.Interface, k8sErr error, namespace string, resolved *spec.ResolvedSpec) error {
	if resolved == nil {
		return nil
	}
	if k8sErr != nil && k8s.HasSelfSignedComponents(resolved) {
		return fmt.Errorf("kubernetes client required to materialize self-signed TLS certificates: %w", k8sErr)
	}
	return k8s.MaterializeSelfSignedTLS(ctx, k8sClient, namespace, resolved)
}
