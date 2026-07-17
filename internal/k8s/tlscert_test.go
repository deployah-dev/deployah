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
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	certutil "k8s.io/client-go/util/cert"
)

const tlsTestFQDN = "web.127.0.0.1.nip.io"

func leafOf(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	certs, err := certutil.ParseCertsPEM(certPEM)
	require.NoError(t, err)
	require.NotEmpty(t, certs)
	return certs[0]
}

func tlsSecret(certPEM, keyPEM []byte, secretType corev1.SecretType) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      selfSignedSecretName(tlsTestFQDN),
			Namespace: testNamespace,
		},
		Type: secretType,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}
}

// TestGenerateSelfSignedCert covers the named case.
func TestGenerateSelfSignedCert(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM, err := GenerateSelfSignedCert(tlsTestFQDN)
	require.NoError(t, err)
	require.NotEmpty(t, certPEM)
	require.NotEmpty(t, keyPEM)

	_, err = tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err, "cert and key must form a valid pair")

	leaf := leafOf(t, certPEM)
	assert.NoError(t, leaf.VerifyHostname(tlsTestFQDN))
	assert.WithinDuration(t, time.Now().Add(selfSignedCertMaxAge), leaf.NotAfter, 24*time.Hour)
}

// TestEnsureSelfSignedCert covers reuse versus regeneration paths.
func TestEnsureSelfSignedCert(t *testing.T) {
	t.Parallel()

	validCert, validKey, err := GenerateSelfSignedCert(tlsTestFQDN)
	require.NoError(t, err)

	expiringCert, expiringKey, err := certutil.GenerateSelfSignedCertKeyWithOptions(certutil.SelfSignedCertKeyOptions{
		Host:         tlsTestFQDN,
		AlternateDNS: []string{tlsTestFQDN},
		MaxAge:       1 * time.Hour, // well inside the 30-day renew window
	})
	require.NoError(t, err)

	wrongHostCert, wrongHostKey, err := GenerateSelfSignedCert("other.example.com")
	require.NoError(t, err)

	tests := []struct {
		name           string
		seed           *corev1.Secret
		wantReuse      []byte // when set, returned cert must equal this
		wantRegenerate []byte // when set, returned cert must differ from this
		checkLeaf      bool
	}{
		{
			name: "absent generates new",
		},
		{
			name:      "reuses existing valid secret",
			seed:      tlsSecret(validCert, validKey, corev1.SecretTypeTLS),
			wantReuse: validCert,
		},
		{
			name:           "regenerates when expiring soon",
			seed:           tlsSecret(expiringCert, expiringKey, corev1.SecretTypeTLS),
			wantRegenerate: expiringCert,
			checkLeaf:      true,
		},
		{
			name:           "regenerates for wrong hostname",
			seed:           tlsSecret(wrongHostCert, wrongHostKey, corev1.SecretTypeTLS),
			wantRegenerate: wrongHostCert,
			checkLeaf:      true,
		},
		{
			name: "regenerates for non-TLS secret type",
			seed: tlsSecret([]byte("not-actually-tls"), []byte("not-actually-tls"), corev1.SecretTypeOpaque),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var client *fake.Clientset
			if tt.seed == nil {
				client = fake.NewClientset()
			} else {
				client = fake.NewClientset(tt.seed)
			}

			gotCert, gotKey, ensureErr := EnsureSelfSignedCert(t.Context(), client, testNamespace, tlsTestFQDN)
			require.NoError(t, ensureErr)
			require.NotEmpty(t, gotCert)
			require.NotEmpty(t, gotKey)

			_, pairErr := tls.X509KeyPair(gotCert, gotKey)
			require.NoError(t, pairErr)

			if tt.wantReuse != nil {
				assert.Equal(t, tt.wantReuse, gotCert, "existing valid cert must be reused unchanged")
				assert.Equal(t, validKey, gotKey, "existing valid key must be reused unchanged")
			}
			if tt.wantRegenerate != nil {
				assert.NotEqual(t, tt.wantRegenerate, gotCert, "must regenerate instead of reuse")
			}
			if tt.checkLeaf {
				leaf := leafOf(t, gotCert)
				assert.NoError(t, leaf.VerifyHostname(tlsTestFQDN))
				assert.WithinDuration(t, time.Now().Add(selfSignedCertMaxAge), leaf.NotAfter, 24*time.Hour)
			}
		})
	}
}

// TestMaterializeSelfSignedTLS covers nil, offline generation, and online reuse.
func TestMaterializeSelfSignedTLS(t *testing.T) {
	t.Parallel()

	t.Run("nil resolved is a no-op", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, MaterializeSelfSignedTLS(t.Context(), fake.NewClientset(), testNamespace, nil))
	})

	t.Run("materializes only selfSigned components, offline with nil client", func(t *testing.T) {
		t.Parallel()
		resolved := &spec.ResolvedSpec{
			Components: map[string]spec.ResolvedComponent{
				"web": {FQDN: tlsTestFQDN, TLSMode: spec.TLSModeSelfSigned},
				"api": {FQDN: "api.example.com", TLSMode: spec.TLSModeCertManager},
			},
		}

		require.NoError(t, MaterializeSelfSignedTLS(t.Context(), nil, testNamespace, resolved))

		web := resolved.Components["web"]
		assert.NotEmpty(t, web.TLSCertPEM)
		assert.NotEmpty(t, web.TLSKeyPEM)

		api := resolved.Components["api"]
		assert.Empty(t, api.TLSCertPEM, "non-selfSigned components must not get a materialized cert")
	})

	t.Run("online reuses existing secret", func(t *testing.T) {
		t.Parallel()
		certPEM, keyPEM, genErr := GenerateSelfSignedCert(tlsTestFQDN)
		require.NoError(t, genErr)

		client := fake.NewClientset(tlsSecret(certPEM, keyPEM, corev1.SecretTypeTLS))
		resolved := &spec.ResolvedSpec{
			Components: map[string]spec.ResolvedComponent{
				"web": {FQDN: tlsTestFQDN, TLSMode: spec.TLSModeSelfSigned},
			},
		}

		require.NoError(t, MaterializeSelfSignedTLS(t.Context(), client, testNamespace, resolved))
		assert.Equal(t, certPEM, resolved.Components["web"].TLSCertPEM)
		assert.Equal(t, keyPEM, resolved.Components["web"].TLSKeyPEM)
	})
}
