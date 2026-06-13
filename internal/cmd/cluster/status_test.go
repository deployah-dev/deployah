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

package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

// TestLoadBalancerAddress verifies that the first IP (or hostname) is returned
// from a LoadBalancer status ingress list, and that empty lists return "".
func TestLoadBalancerAddress(t *testing.T) {
	tests := []struct {
		name    string
		ingress []corev1.LoadBalancerIngress
		want    string
	}{
		{"empty", nil, ""},
		{"ip", []corev1.LoadBalancerIngress{{IP: "172.18.0.4"}}, "172.18.0.4"},
		{"hostname", []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}}, "lb.example.com"},
		{"ip preferred over hostname", []corev1.LoadBalancerIngress{{IP: "10.0.0.1", Hostname: "host"}}, "10.0.0.1"},
		{"first ip when multiple", []corev1.LoadBalancerIngress{{IP: "10.0.0.1"}, {IP: "10.0.0.2"}}, "10.0.0.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, loadBalancerAddress(tc.ingress))
		})
	}
}

// TestIngressCurl_HTTP verifies the curl hint for HTTP ingress uses a Host header.
func TestIngressCurl_HTTP(t *testing.T) {
	cmd := ingressCurl("http", "my-app.local", "172.18.0.4")
	assert.Equal(t, "curl -H 'Host: my-app.local' http://172.18.0.4", cmd)
}

// TestIngressCurl_HTTPS_IP verifies the curl hint for HTTPS with an IP address
// uses --resolve to also set the SNI name.
func TestIngressCurl_HTTPS_IP(t *testing.T) {
	cmd := ingressCurl("https", "my-app.local", "172.18.0.4")
	assert.Equal(t, "curl -k --resolve my-app.local:443:172.18.0.4 https://my-app.local", cmd)
}

// TestIngressCurl_HTTPS_Hostname verifies that a hostname-based HTTPS address
// falls back to a plain curl request since --resolve requires an IP.
func TestIngressCurl_HTTPS_Hostname(t *testing.T) {
	cmd := ingressCurl("https", "my-app.local", "lb.example.com")
	assert.Equal(t, "curl -k https://my-app.local", cmd)
}

// TestHasTLS verifies that hasTLS returns true only when the Ingress declares
// at least one TLS block.
func TestHasTLS(t *testing.T) {
	noTLS := &networkingv1.Ingress{}
	assert.False(t, hasTLS(noTLS))

	withTLS := &networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"my-app.local"}}},
		},
	}
	assert.True(t, hasTLS(withTLS))
}

// TestHasPortMappedAccess reports true when at least one entry uses a
// localhost port mapping, and false otherwise.
func TestHasPortMappedAccess(t *testing.T) {
	assert.False(t, hasPortMappedAccess(nil))
	assert.False(t, hasPortMappedAccess([]accessEntry{
		{Address: "172.18.0.4"},
	}))
	assert.True(t, hasPortMappedAccess([]accessEntry{
		{Address: "172.18.0.4"},
		{Address: "127.0.0.1:32769"},
	}))
}
