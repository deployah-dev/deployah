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

package cmdopts

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/spec"
)

// clusterHintSuffix is appended to connectivity errors so deploy and plan
// share the same recovery guidance.
const clusterHintSuffix = "\n\nHint: the target cluster/context may be unavailable. For a local cluster, run 'deployah cluster up' (and pass --context kind-deployah or set the environment's 'context' field)."

// ClusterHint returns an actionable suffix for errors that look like the
// target cluster or context is missing or unreachable. It returns an empty
// string for unrelated errors. Shared by `deployah deploy` and `deployah
// plan` so their connectivity error messages never drift apart.
func ClusterHint(err error) string {
	if !isClusterUnreachable(err) {
		return ""
	}
	return clusterHintSuffix
}

// isClusterUnreachable reports whether err (or a wrapped cause) indicates a
// missing kubeconfig, unknown context, or network failure reaching the API.
func isClusterUnreachable(err error) bool {
	if err == nil {
		return false
	}

	// clientcmd.IsEmptyConfig / IsContextNotFound do not walk wrappers, so
	// unwrap one level at a time before those checks.
	for e := err; e != nil; e = errors.Unwrap(e) {
		if clientcmd.IsEmptyConfig(e) || clientcmd.IsContextNotFound(e) {
			return true
		}
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		if slices.ContainsFunc(multi.Unwrap(), isClusterUnreachable) {
			return true
		}
	}

	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr != nil {
		return true
	}
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && dnsErr != nil {
		return true
	}
	if errno, ok := errors.AsType[syscall.Errno](err); ok &&
		(errno == syscall.ECONNREFUSED || errno == syscall.ECONNRESET) {
		return true
	}
	return false
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
