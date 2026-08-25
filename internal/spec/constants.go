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

import "time"

// File and Path Constants
const (
	// CurrentManifestVersion is the manifest apiVersion written by the init
	// command. During alpha, only this version has an embedded schema. Bump
	// it and replace the schema directory when the spec changes.
	CurrentManifestVersion = "v1-alpha.5"

	// DefaultSpecPath is the default path for the Deployah spec file
	DefaultSpecPath = "deployah.yaml"

	// schemaDocsBase is the URL prefix for published JSON Schema $id values.
	schemaDocsBase = "https://deployah.dev/schemas/"

	// DefaultEnvFile is the default environment file name
	DefaultEnvFile = ".env"

	// DefaultConfigFile is the default configuration file name
	DefaultConfigFile = "config.yaml"

	// DeployahConfigDir is the default directory for Deployah-specific files
	DeployahConfigDir = ".deployah"

	// EnvFilePrefix is the prefix for environment-specific files
	EnvFilePrefix = ".env."

	// ConfigFilePrefix is the prefix for environment-specific config files
	ConfigFilePrefix = "config."

	// ConfigFileSuffix is the suffix for configuration files
	ConfigFileSuffix = ".yaml"
)

// SupportedManifestVersions is the apiVersion values this release will load.
// During alpha this is only [CurrentManifestVersion].
var SupportedManifestVersions = []string{CurrentManifestVersion}

// ManifestSchemaURL is the JSON Schema $id for the current manifest, used
// as a `# $schema` modeline so editors can autocomplete omitted fields.
func ManifestSchemaURL() string {
	return schemaDocsBase + CurrentManifestVersion + "/manifest.json"
}

// PlatformSchemaURL is the JSON Schema $id for the current platform file.
func PlatformSchemaURL() string {
	return schemaDocsBase + CurrentPlatformVersion + "/platform.json"
}

// SchemaModeline returns an IntelliJ/Red Hat YAML modeline for schemaURL.
func SchemaModeline(schemaURL string) string {
	return "# $schema: " + schemaURL + "\n"
}

// Environment Variables
const (
	// EnvVarPrefix is the prefix for Deployah-specific environment variables
	EnvVarPrefix = "DPY_VAR_"

	// LogLevelEnvVar is the environment variable for log level override
	LogLevelEnvVar = "DPY_LOG_LEVEL"
)

// Validation Constants
const (
	// MaxComponentNameLength is the maximum allowed length for component names
	MaxComponentNameLength = 63

	// MaxTaskNameLength is the maximum allowed length for task names.
	// The CronJob helper keeps the task suffix intact inside the 52-character
	// Kubernetes CronJob name limit, so the schema and Go validation cap
	// names at 30.
	MaxTaskNameLength = 30

	// MaxProjectNameLength is the maximum allowed length for project names
	MaxProjectNameLength = 63

	// MaxEnvironmentNameLength is the maximum allowed length for environment names
	MaxEnvironmentNameLength = 63

	// ComponentNamePattern is the regex pattern for valid component names
	ComponentNamePattern = "^[a-zA-Z0-9_-]+$"

	// ProjectNamePattern is the regex pattern for valid project names
	ProjectNamePattern = "^[a-zA-Z0-9_-]+$"

	// EnvironmentNamePattern is the regex pattern for valid environment names
	EnvironmentNamePattern = "^[a-zA-Z0-9_-]+$"
)

// Spec Processing
const (
	// PlaceholderName is the placeholder used in templates for name substitution
	PlaceholderName = "{name}"

	// ComponentsPrefix is the prefix for component paths in schemas
	ComponentsPrefix = "components."

	// TasksPrefix is the prefix for task paths in schemas
	TasksPrefix = "tasks."

	// EnvironmentsPrefix is the prefix for environment paths in schemas
	EnvironmentsPrefix = "environments."

	// ArrayItemIndexTemplate is the template for array item indices in schema paths
	ArrayItemIndexTemplate = "[0]"

	// EnvFileSuffix is the suffix to remove from environment names during cleanup
	EnvFileSuffix = "/*"
)

// Health Check Probe Timing
//
// These constants define the Kubernetes probe parameters used when building
// startup, readiness, and liveness probes from the spec health fields. They
// are named constants so that the product behavior (e.g. how quickly a pod
// is removed from rotation) can be reviewed and changed in one place.
const (
	// DefaultStartupProbePeriod is how often (in seconds) the startup probe
	// checks the container port during the startup window.
	DefaultStartupProbePeriod = 5

	// DefaultStartupProbeFailureThreshold is how many consecutive failures
	// before the container is killed during startup.
	// Budget: 36 * 5s = 180s (3 minutes).
	DefaultStartupProbeFailureThreshold = 36

	// DefaultStartupProbeTimeout is the per-request timeout in seconds for
	// the startup probe.
	DefaultStartupProbeTimeout = 3

	// DefaultReadinessProbePeriod is how often (in seconds) the readiness
	// probe checks whether the container can receive traffic.
	DefaultReadinessProbePeriod = 5

	// DefaultReadinessProbeFailureThreshold is how many consecutive failures
	// before the container is removed from service endpoints.
	// Detection window: 3 * 5s = 15s.
	DefaultReadinessProbeFailureThreshold = 3

	// DefaultReadinessProbeTimeout is the per-request timeout in seconds for
	// the readiness probe.
	DefaultReadinessProbeTimeout = 3

	// DefaultLivenessProbePeriod is how often (in seconds) the alive probe
	// checks whether the container is responsive.
	DefaultLivenessProbePeriod = 10

	// DefaultLivenessProbeTimeout is the per-request timeout in seconds for
	// the alive probe.
	DefaultLivenessProbeTimeout = 3

	// DefaultLivenessRestartAfterSec is the default restart-after window
	// in seconds (used as a numeric fallback in probe generation).
	DefaultLivenessRestartAfterSec = 60

	// DefaultLivenessInterval is the default value for health.alive.interval
	// when the field is omitted.
	DefaultLivenessInterval = "10s"

	// DefaultLivenessRestartAfter is the default value for
	// health.alive.restartAfter when the field is omitted.
	DefaultLivenessRestartAfter = "60s"

	// DefaultServiceShutdownTimeout is the default shutdownTimeout for
	// service components (terminationGracePeriodSeconds).
	DefaultServiceShutdownTimeout = "30s"

	// DefaultWorkerShutdownTimeout is the default shutdownTimeout for
	// worker components (terminationGracePeriodSeconds).
	DefaultWorkerShutdownTimeout = "60s"

	// DefaultServicePort is the default container port for service
	// components when Port is omitted. Workers do not receive this default.
	DefaultServicePort = 8080

	// DefaultMinReplicas is the default Autoscaling.MinReplicas when the
	// field is omitted.
	DefaultMinReplicas = 2

	// DefaultMaxReplicas is the default Autoscaling.MaxReplicas when the
	// field is omitted.
	DefaultMaxReplicas = 5

	// DefaultCPUTarget is the default CPU utilization percentage for the
	// default autoscaling metric when metrics are omitted.
	DefaultCPUTarget = 75

	// DefaultHookTaskTimeout is the default timeout for preDeploy and
	// postDeploy tasks when timeout is omitted.
	DefaultHookTaskTimeout = "5m"

	// DefaultScheduledTaskTimeout is the CronJob activeDeadlineSeconds
	// used when a scheduled task omits timeout. It is not applied to
	// [Task.Timeout] or to deployah run Jobs.
	DefaultScheduledTaskTimeout = "1h"

	// DefaultConcurrencyPolicy is the CronJob concurrencyPolicy when
	// omitted on a scheduled task.
	DefaultConcurrencyPolicy = "Forbid"

	// DefaultScheduleTimeZone is the CronJob timeZone when omitted on a
	// scheduled task.
	DefaultScheduleTimeZone = "Etc/UTC"

	// DefaultSuccessfulJobsHistory is successfulJobsHistoryLimit on a
	// scheduled-task CronJob.
	DefaultSuccessfulJobsHistory = 3

	// DefaultFailedJobsHistory is failedJobsHistoryLimit on a
	// scheduled-task CronJob.
	DefaultFailedJobsHistory = 3

	// DefaultDeployTimeout is the default CLI --timeout. Hook task
	// timeouts must be strictly less than the session --timeout at
	// deploy or run time.
	DefaultDeployTimeout = 10 * time.Minute

	// DefaultBackoffLimit is the default Job retry count for tasks.
	DefaultBackoffLimit = 3

	// DefaultFanoutCount is the default fanout count when omitted.
	DefaultFanoutCount = 1

	// DefaultFanoutParallelism is the default fanout parallelism when omitted.
	DefaultFanoutParallelism = 1

	// MaxFanoutParallelism is the largest allowed fanout.parallelism.
	// Kubernetes rejects Indexed Jobs when parallelism is above 10^5.
	MaxFanoutParallelism = 100_000

	// DefaultCLIJobTTLSeconds is how long CLI-triggered Jobs are kept
	// after they finish (7 days).
	DefaultCLIJobTTLSeconds = 7 * 24 * 60 * 60

	// DefaultMetricsPath is the default HTTP path for Prometheus metrics.
	DefaultMetricsPath = "/metrics"

	// IdentityPortName is the synthetic container/service port name used for
	// headless DNS on stateful workers that have no app port.
	IdentityPortName = "identity"

	// IdentityPortNumber is the discard protocol port used as a tracking
	// port for headless Services when a worker has no application port.
	IdentityPortNumber = 9

	// MetricsPortName is the named container/service port used when metrics
	// scraping is enabled on a dedicated port or on a worker.
	MetricsPortName = "metrics"
)

// Resource Management
const (
	// DefaultResourcePreset is the default resource preset when none is specified
	DefaultResourcePreset = "small"

	// MinCPUMillicores is the minimum CPU allocation in millicores
	MinCPUMillicores = 10

	// MaxCPUMillicores is the maximum CPU allocation in millicores
	MaxCPUMillicores = 16000

	// MinMemoryMB is the minimum memory allocation in megabytes
	MinMemoryMB = 16

	// MaxMemoryMB is the maximum memory allocation in megabytes
	MaxMemoryMB = 32768
)

// Kubernetes Labels
const (
	// LabelPrefix is the prefix for all Deployah-managed labels
	LabelPrefix = "deployah.dev"

	// LabelProject is the label key for project identification
	LabelProject = LabelPrefix + "/project"

	// LabelEnvironment is the label key for environment identification
	LabelEnvironment = LabelPrefix + "/environment"

	// LabelManagedBy is the label key indicating management by Deployah
	LabelManagedBy = LabelPrefix + "/managed-by"

	// LabelVersion is the label key for API version tracking
	LabelVersion = LabelPrefix + "/version"

	// LabelComponent is the label key for component identification
	LabelComponent = LabelPrefix + "/component"

	// ManagedByValue is the value used for the managed-by label
	ManagedByValue = "deployah"

	// AnnotationSource is the annotation key recording which Deployah layer
	// produced a managed object (spec, manifests, or crds).
	AnnotationSource = LabelPrefix + "/source"

	// AnnotationProject is the annotation key for project identification on
	// Deployah-managed objects. Same string as LabelProject; used as an
	// annotation so CRDs (which carry no environment label) still identify
	// the owning project.
	AnnotationProject = LabelProject

	// SourceSpec is the AnnotationSource value for chart-generated objects.
	SourceSpec = "spec"

	// SourceManifests is the AnnotationSource value for .deployah/manifests.
	SourceManifests = "manifests"

	// SourceCRDs is the AnnotationSource value for .deployah/crds.
	SourceCRDs = "crds"

	// ManifestsDir is the subdirectory under DeployahConfigDir for extra
	// Kubernetes manifests.
	ManifestsDir = "manifests"

	// CRDsDir is the subdirectory under DeployahConfigDir for CRDs.
	CRDsDir = "crds"
)

// ResourcePresetMappings defines the resource specifications for each preset
var ResourcePresetMappings = map[ResourcePreset]map[string]Resources{
	ResourcePresetNano: {
		"requests": {
			CPU:              MustQuantity("100m"),
			Memory:           MustQuantity("128Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("150m"),
			Memory:           MustQuantity("192Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
	ResourcePresetMicro: {
		"requests": {
			CPU:              MustQuantity("250m"),
			Memory:           MustQuantity("256Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("375m"),
			Memory:           MustQuantity("384Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
	ResourcePresetSmall: {
		"requests": {
			CPU:              MustQuantity("500m"),
			Memory:           MustQuantity("512Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("750m"),
			Memory:           MustQuantity("768Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
	ResourcePresetMedium: {
		"requests": {
			CPU:              MustQuantity("500m"),
			Memory:           MustQuantity("1024Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("750m"),
			Memory:           MustQuantity("1536Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
	ResourcePresetLarge: {
		"requests": {
			CPU:              MustQuantity("1000m"),
			Memory:           MustQuantity("2048Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("1500m"),
			Memory:           MustQuantity("3072Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
	ResourcePresetXLarge: {
		"requests": {
			CPU:              MustQuantity("1000m"),
			Memory:           MustQuantity("3072Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("3000m"),
			Memory:           MustQuantity("6144Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
	ResourcePreset2XLarge: {
		"requests": {
			CPU:              MustQuantity("1000m"),
			Memory:           MustQuantity("3072Mi"),
			EphemeralStorage: MustQuantity("50Mi"),
		},
		"limits": {
			CPU:              MustQuantity("6000m"),
			Memory:           MustQuantity("12288Mi"),
			EphemeralStorage: MustQuantity("2Gi"),
		},
	},
}
