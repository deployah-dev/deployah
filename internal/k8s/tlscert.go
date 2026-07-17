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

package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certutil "k8s.io/client-go/util/cert"
)

// selfSignedCertMaxAge is the validity window for a generated self-signed
// certificate. Chosen to comfortably outlive typical local/dev cluster
// lifetimes without requiring rotation.
const selfSignedCertMaxAge = 3650 * 24 * time.Hour

// selfSignedCertRenewBefore is how far ahead of expiry a reused certificate
// is treated as no longer usable, so it gets regenerated before it actually
// expires.
const selfSignedCertRenewBefore = 30 * 24 * time.Hour

// selfSignedSecretName returns the deterministic TLS secret name for fqdn,
// matching the naming convention the chart's ingress template expects
// (`<hostname>-tls`).
func selfSignedSecretName(fqdn string) string {
	return fqdn + "-tls"
}

// EnsureSelfSignedCert returns a PEM cert/key pair for fqdn, reusing the
// existing `kubernetes.io/tls` Secret named `<fqdn>-tls` in namespace when
// present and not close to expiry. Otherwise it generates a new self-signed
// pair. Reuse (rather than regenerating on every call) is what keeps the
// rendered manifest identical across plan/apply/re-deploy: a fresh keypair
// every render would defeat both the apply-time verification and
// skip-on-no-change.
func EnsureSelfSignedCert(ctx context.Context, client kubernetes.Interface, namespace, fqdn string) (certPEM, keyPEM []byte, err error) {
	secret, getErr := client.CoreV1().Secrets(namespace).Get(ctx, selfSignedSecretName(fqdn), metav1.GetOptions{})
	switch {
	case getErr == nil:
		if reusable, crt, key := reusableCert(secret, fqdn); reusable {
			return crt, key, nil
		}
	case apierrors.IsNotFound(getErr):
		// Fall through to generation.
	default:
		return nil, nil, fmt.Errorf("get TLS secret %s: %w", selfSignedSecretName(fqdn), getErr)
	}

	return GenerateSelfSignedCert(fqdn)
}

// reusableCert reports whether secret holds a still-valid TLS keypair for
// fqdn, returning the stored PEM bytes when it does.
func reusableCert(secret *corev1.Secret, fqdn string) (ok bool, certPEM, keyPEM []byte) {
	if secret.Type != corev1.SecretTypeTLS {
		return false, nil, nil
	}
	crt := secret.Data[corev1.TLSCertKey]
	key := secret.Data[corev1.TLSPrivateKeyKey]
	if len(crt) == 0 || len(key) == 0 {
		return false, nil, nil
	}

	pair, parseErr := tls.X509KeyPair(crt, key)
	if parseErr != nil {
		return false, nil, nil
	}
	leaf, parseErr := x509.ParseCertificate(pair.Certificate[0])
	if parseErr != nil {
		return false, nil, nil
	}
	if time.Until(leaf.NotAfter) < selfSignedCertRenewBefore {
		return false, nil, nil
	}
	if err := leaf.VerifyHostname(fqdn); err != nil {
		return false, nil, nil
	}

	return true, crt, key
}

// GenerateSelfSignedCert generates a fresh self-signed PEM cert/key pair for
// fqdn without any cluster access. Used for the plan --offline render, where
// there is no cluster to fetch a reusable secret from.
func GenerateSelfSignedCert(fqdn string) (certPEM, keyPEM []byte, err error) {
	certPEM, keyPEM, err = certutil.GenerateSelfSignedCertKeyWithOptions(certutil.SelfSignedCertKeyOptions{
		Host:         fqdn,
		AlternateDNS: []string{fqdn},
		MaxAge:       selfSignedCertMaxAge,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("generate self-signed certificate for %s: %w", fqdn, err)
	}
	return certPEM, keyPEM, nil
}

// HasSelfSignedComponents reports whether resolved has at least one exposed
// component whose TLSMode is selfSigned, meaning a caller must have a
// working Kubernetes clientset (or explicitly accept offline generation)
// before rendering.
func HasSelfSignedComponents(resolved *spec.ResolvedSpec) bool {
	if resolved == nil {
		return false
	}
	for _, rc := range resolved.Components {
		if rc.TLSMode == spec.TLSModeSelfSigned && rc.FQDN != "" {
			return true
		}
	}
	return false
}

// MaterializeSelfSignedTLS fills TLSCertPEM/TLSKeyPEM on every resolved
// component whose TLSMode is selfSigned. Call it once per CLI invocation,
// before any chart render, so the plan render, apply-time verification
// render, and real apply all see identical certificate bytes. Pass a nil
// client to force offline generation (no cluster access), as plan --offline
// does.
func MaterializeSelfSignedTLS(ctx context.Context, client kubernetes.Interface, namespace string, resolved *spec.ResolvedSpec) error {
	if resolved == nil {
		return nil
	}

	for name, rc := range resolved.Components {
		if rc.TLSMode != spec.TLSModeSelfSigned || rc.FQDN == "" {
			continue
		}

		var (
			certPEM, keyPEM []byte
			err             error
		)
		if client != nil {
			certPEM, keyPEM, err = EnsureSelfSignedCert(ctx, client, namespace, rc.FQDN)
		} else {
			certPEM, keyPEM, err = GenerateSelfSignedCert(rc.FQDN)
		}
		if err != nil {
			return fmt.Errorf("component %s: %w", name, err)
		}

		rc.TLSCertPEM = certPEM
		rc.TLSKeyPEM = keyPEM
		resolved.Components[name] = rc
	}

	return nil
}
