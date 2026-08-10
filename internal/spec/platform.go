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

import (
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
)

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
	// APIVersion is the platform schema version, e.g. "platform/v1-alpha.3".
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	// Profiles maps logical profile names to deployment policy. Profiles are
	// org-wide (root-level), not per-environment. A profile named "default" is
	// prepended automatically when a component omits profiles.
	Profiles map[string]PlatformProfile `json:"profiles,omitempty" yaml:"profiles,omitempty"`
	// Environments is a map of environment names to their platform
	// configuration. Wildcard matching (prefix-split on "/") is applied by
	// [matchEnvKey].
	Environments map[string]PlatformEnvironment `json:"environments" yaml:"environments"`
}

// DefaultProfileName is the profile name that is automatically prepended when
// a component omits the profiles field.
const DefaultProfileName = "default"

// PlatformProfile is a named deployment policy applied to components that
// select it. Multiple profiles merge left to right.
type PlatformProfile struct {
	// NodeSelector is Kubernetes nodeSelector labels for pod placement.
	NodeSelector map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector,omitempty"`
	// Tolerations are Kubernetes tolerations for pod scheduling.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty" yaml:"tolerations,omitempty"`
	// PodLabels are additional labels applied to pods.
	PodLabels map[string]string `json:"podLabels,omitempty" yaml:"podLabels,omitempty"`
	// PodAnnotations are additional annotations applied to pods.
	PodAnnotations map[string]string `json:"podAnnotations,omitempty" yaml:"podAnnotations,omitempty"`
	// SecurityContext is a Kubernetes PodSecurityContext (pod-level).
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty" yaml:"securityContext,omitempty"`
	// ContainerSecurityContext is a Kubernetes SecurityContext applied to
	// all containers.
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty" yaml:"containerSecurityContext,omitempty"`
	// StorageClass is a logical storage class key from the target
	// environment's storageClasses map.
	StorageClass string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
	// PVCRetentionPolicy overrides StatefulSet PVC retention for stateful
	// components. Nil means chart defaults (Retain/Retain).
	PVCRetentionPolicy *PVCRetentionPolicy `json:"pvcRetentionPolicy,omitempty" yaml:"pvcRetentionPolicy,omitempty"`
	// AllowedDomains restricts which domain keys a component may expose on.
	// nil means no constraint. A non-nil empty list means deny-all (no domain
	// is allowed). Multiple profiles intersect. Neither JSON nor YAML uses
	// omitempty, so deny-all serializes as [] rather than becoming nil.
	AllowedDomains []string `json:"allowedDomains" yaml:"allowedDomains"`
	// MaxResources is a ceiling on component resource requests.
	MaxResources *ProfileMaxResources `json:"maxResources,omitempty" yaml:"maxResources,omitempty"`
	// Metrics holds Prometheus Operator monitor defaults owned by the
	// platform (discovery labels, scrape timing, relabelings). Nested under
	// metrics so profile root stays free of scrape-specific field names.
	Metrics *ProfileMetrics `json:"metrics,omitempty" yaml:"metrics,omitempty"`
}

// ProfileMetrics is the platform-owned Prometheus scrape policy for a
// profile. When a component enables metrics, MonitorLabels must be set on
// the merged profile so Prometheus Operator can discover the monitor CR.
type ProfileMetrics struct {
	// MonitorLabels are required discovery labels for ServiceMonitor and
	// PodMonitor resources. Typically matches the Prometheus Operator
	// release selector (e.g. release: kube-prometheus-stack).
	MonitorLabels map[string]string `json:"monitorLabels,omitempty" yaml:"monitorLabels,omitempty"`
	// MonitorNamespace is the namespace where the monitor CR is created.
	// Empty means the app namespace.
	MonitorNamespace string `json:"monitorNamespace,omitempty" yaml:"monitorNamespace,omitempty"`
	// Interval is the default scrape interval (e.g. "30s").
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	// ScrapeTimeout is the default scrape timeout (e.g. "10s").
	ScrapeTimeout string `json:"scrapeTimeout,omitempty" yaml:"scrapeTimeout,omitempty"`
	// JobLabel is the ServiceMonitor/PodMonitor jobLabel field.
	JobLabel string `json:"jobLabel,omitempty" yaml:"jobLabel,omitempty"`
	// HonorLabels maps to honorLabels on monitor endpoints.
	HonorLabels *bool `json:"honorLabels,omitempty" yaml:"honorLabels,omitempty"`
	// Annotations are applied to the monitor CR.
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	// Relabelings are Prometheus relabel configs on the scrape endpoint.
	Relabelings []any `json:"relabelings,omitempty" yaml:"relabelings,omitempty"`
	// MetricRelabelings are Prometheus metricRelabelings on the scrape endpoint.
	MetricRelabelings []any `json:"metricRelabelings,omitempty" yaml:"metricRelabelings,omitempty"`
}

// PVCRetentionPolicy controls StatefulSet persistentVolumeClaimRetentionPolicy.
type PVCRetentionPolicy struct {
	// WhenDeleted is Retain or Delete when the StatefulSet is deleted.
	WhenDeleted string `json:"whenDeleted,omitempty" yaml:"whenDeleted,omitempty"`
	// WhenScaled is Retain or Delete when the StatefulSet is scaled down.
	WhenScaled string `json:"whenScaled,omitempty" yaml:"whenScaled,omitempty"`
}

// ProfileMaxResources caps component resource requests.
type ProfileMaxResources struct {
	// CPU is the maximum CPU request (Kubernetes quantity).
	CPU *resource.Quantity `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	// Memory is the maximum memory request (Kubernetes quantity).
	Memory *resource.Quantity `json:"memory,omitempty" yaml:"memory,omitempty"`
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
	// Default marks this domain as the one used when a component's expose
	// block names no domain. At most one per environment.
	Default bool `json:"default,omitempty" yaml:"default,omitempty"`
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
