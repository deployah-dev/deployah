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

// ResolvedSpec is the result of the full load + platform + resolution
// pipeline for a specific environment. It is the primary input to
// [MapSpecToChartValues], the hostname guard, and cache key generation.
//
// The deployah.resolved block written to Helm chart values has this contract:
//
//	deployah:
//	  resolved:
//	    schemaVersion: "1"
//	    components:
//	      <name>:
//	        fqdn: api.example.com
//	        tlsMode: certManager
//	        storageClass: ""
type ResolvedSpec struct {
	// Spec is the application manifest with defaults applied.
	Spec *Spec
	// Env is the canonical environment identity used for this resolution.
	Env EnvIdentity
	// KubeContext is the Kubernetes context resolved from the platform file.
	// Empty when no platform file was loaded.
	KubeContext string
	// Components holds the per-component resolved data.
	Components map[string]ResolvedComponent
	// Warnings is the list of non-fatal resolution warnings.
	Warnings []string
}

// ResolvedComponent holds the platform-resolved values for a single component.
type ResolvedComponent struct {
	// FQDN is the fully qualified domain name resolved for this component.
	// Empty when the component has no expose block.
	FQDN string
	// TLSMode is the TLS provisioning strategy resolved from the platform.
	// Empty when the component has no TLS configuration.
	TLSMode TLSMode
	// TLSIssuer is the cert-manager issuer name (certManager mode only).
	TLSIssuer string
	// TLSSecretName is the pre-existing TLS secret name (secretName mode only).
	TLSSecretName string
	// StorageClass is the Kubernetes storage class name resolved from the
	// platform. Empty when the component has no storage block.
	StorageClass string
}

// ResolutionReport holds the provenance of each resolved field, enabling
// the resolve command and deploy --explain to trace where each value came from.
type ResolutionReport struct {
	// Env is the environment identity used for this resolution.
	Env EnvIdentity
	// Fields is the ordered list of resolved field provenance entries.
	Fields []ResolvedField
	// Warnings is the list of non-fatal warnings emitted during resolution.
	Warnings []string
	// ErrorCode is set when resolution produced a hard error.
	ErrorCode string
	// ErrorMessage is the human-readable error message when ErrorCode is set.
	ErrorMessage string
}

// ResolvedField holds provenance for a single resolved value.
type ResolvedField struct {
	// Component is the component name this field belongs to, or empty for
	// top-level fields.
	Component string
	// Path is the spec path, e.g. "expose.host".
	Path string
	// Value is the resolved value as a string.
	Value string
	// Source is a human-readable description of where the value came from,
	// e.g. "platform environments.production.domains.public.baseDomain".
	Source string
}

// Phase 1 error codes for use in the resolution report and JSON output.
// CAP_EXCEEDED is deferred to Phase 2.
const (
	ErrCodePlatformNotFound        = "PLATFORM_NOT_FOUND"
	ErrCodePlatformEnvNotFound     = "PLATFORM_ENV_NOT_FOUND"
	ErrCodeDomainGap               = "DOMAIN_GAP"
	ErrCodeFQDNCollision           = "FQDN_COLLISION"
	ErrCodeInvalidDNS              = "INVALID_DNS"
	ErrCodeStaticWildcardSubdomain = "STATIC_WILDCARD_SUBDOMAIN"
	ErrCodeContextMismatch         = "CONTEXT_MISMATCH"
	ErrCodeHostnameChanged         = "HOSTNAME_CHANGED"
)

// ResolutionError is a resolution error that carries a machine-readable code.
type ResolutionError struct {
	Code    string
	Message string
}

// Error returns the resolution error message.
func (e *ResolutionError) Error() string { return e.Message }
