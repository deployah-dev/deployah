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

// File and Path Constants
const (
	// CurrentManifestVersion is the manifest apiVersion written by the init
	// command and expected by the current resolver. Bump this when a new
	// schema version is added alongside a new schema directory.
	CurrentManifestVersion = "v1-alpha.2"

	// DefaultSpecPath is the default path for the Deployah spec file
	DefaultSpecPath = "deployah.yaml"

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
)

// ResourcePresetMappings defines the resource specifications for each preset
var ResourcePresetMappings = map[ResourcePreset]map[string]Resources{
	ResourcePresetNano: {
		"requests": {
			CPU:              new("100m"),
			Memory:           new("128Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("150m"),
			Memory:           new("192Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
	ResourcePresetMicro: {
		"requests": {
			CPU:              new("250m"),
			Memory:           new("256Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("375m"),
			Memory:           new("384Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
	ResourcePresetSmall: {
		"requests": {
			CPU:              new("500m"),
			Memory:           new("512Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("750m"),
			Memory:           new("768Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
	ResourcePresetMedium: {
		"requests": {
			CPU:              new("500m"),
			Memory:           new("1024Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("750m"),
			Memory:           new("1536Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
	ResourcePresetLarge: {
		"requests": {
			CPU:              new("1000m"),
			Memory:           new("2048Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("1500m"),
			Memory:           new("3072Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
	ResourcePresetXLarge: {
		"requests": {
			CPU:              new("1000m"),
			Memory:           new("3072Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("3000m"),
			Memory:           new("6144Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
	ResourcePreset2XLarge: {
		"requests": {
			CPU:              new("1000m"),
			Memory:           new("3072Mi"),
			EphemeralStorage: new("50Mi"),
		},
		"limits": {
			CPU:              new("6000m"),
			Memory:           new("12288Mi"),
			EphemeralStorage: new("2Gi"),
		},
	},
}
