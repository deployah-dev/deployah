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

package spec

// DefaultPlatformPath is the default filename for the platform configuration
// file, looked up relative to the manifest.
const DefaultPlatformPath = "deployah.platform.yaml"

// PlatformEnvVar is the environment variable that overrides platform file
// lookup. When set and the file does not exist, the error is surfaced
// immediately; the same-directory fallback is NOT tried.
const PlatformEnvVar = "DEPLOYAH_PLATFORM_FILE"

// PlatformConfig is the top-level structure of the platform file
// (deployah.platform.yaml). It is platform-owned and not subject to envsubst.
type PlatformConfig struct {
	// APIVersion is the platform schema version, e.g. "platform/v1-alpha.1".
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	// Environments is a map of environment names to their platform
	// configuration. Wildcard matching (prefix-split on "/") is applied by
	// [matchEnvKey].
	Environments map[string]PlatformEnvironment `json:"environments" yaml:"environments"`
}

// PlatformEnvironment holds platform-controlled settings for one environment.
type PlatformEnvironment struct {
	// Context is the Kubernetes context to use for this environment.
	Context string `json:"context,omitempty" yaml:"context,omitempty"`
	// Domains is a map of logical domain names to their configuration.
	// Developers reference domain keys in expose.domain.
	Domains map[string]PlatformDomain `json:"domains,omitempty" yaml:"domains,omitempty"`
	// StorageClasses maps logical names to Kubernetes storage classes.
	StorageClasses map[string]PlatformStorageClass `json:"storageClasses,omitempty" yaml:"storageClasses,omitempty"`
	// AllowStaticSubdomain suppresses the wildcard static-subdomain warning
	// for this environment key when set to true.
	AllowStaticSubdomain bool `json:"allowStaticSubdomain,omitempty" yaml:"allowStaticSubdomain,omitempty"`
}

// PlatformDomain holds the base domain and TLS configuration for a logical
// domain key.
type PlatformDomain struct {
	// BaseDomain is the DNS apex for this domain, e.g. "example.com".
	BaseDomain string `json:"baseDomain" yaml:"baseDomain"`
	// TLS holds the TLS mode and associated parameters.
	TLS *PlatformTLS `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// PlatformTLS holds explicit TLS configuration for a domain.
type PlatformTLS struct {
	// Mode is the TLS provisioning strategy.
	Mode TLSMode `json:"mode" yaml:"mode"`
	// Issuer is the cert-manager ClusterIssuer or Issuer name.
	// Required when Mode is [TLSModeCertManager].
	Issuer string `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	// SecretName is the pre-existing Kubernetes TLS secret name.
	// Required when Mode is [TLSModeSecretName].
	SecretName string `json:"secretName,omitempty" yaml:"secretName,omitempty"`
}

// TLSMode specifies the TLS provisioning strategy for a domain.
type TLSMode string

const (
	// TLSModeSelfSigned uses a chart-managed self-signed certificate with a
	// stable secret name. The chart performs a lookup-before-create so the
	// certificate is not regenerated on every deploy.
	TLSModeSelfSigned TLSMode = "selfSigned"
	// TLSModeSecretName uses a pre-existing Kubernetes TLS secret. The secret
	// must exist in the deployment namespace.
	TLSModeSecretName TLSMode = "secretName"
	// TLSModeCertManager provisions a certificate via cert-manager. A
	// pre-flight check verifies that the cert-manager.io/v1 API group exists
	// and that the referenced ClusterIssuer or Issuer object is present.
	TLSModeCertManager TLSMode = "certManager"
)

// PlatformStorageClass maps a logical name to a Kubernetes storage class.
type PlatformStorageClass struct {
	// ClassName is the actual Kubernetes storage class name.
	ClassName string `json:"className" yaml:"className"`
}

// LocalPlatformEnvironment returns a PlatformEnvironment configured for local
// development: kind-deployah context, nip.io base domain using ingressIP, and
// selfSigned TLS. Pass the host IP at which the Ingress controller is reachable
// (typically localkube.DefaultIngressIP).
func LocalPlatformEnvironment(ingressIP string) PlatformEnvironment {
	return PlatformEnvironment{
		Context: "kind-deployah",
		Domains: map[string]PlatformDomain{
			"public": {
				BaseDomain: ingressIP + ".nip.io",
				TLS:        &PlatformTLS{Mode: TLSModeSelfSigned},
			},
		},
	}
}
