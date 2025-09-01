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

// Package manifest provides functions for parsing and manipulating manifest files.
package manifest

// File and Path Constants
const (
	// DefaultManifestPath is the default path for the Deployah manifest file
	DefaultManifestPath = "deployah.yaml"

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

// Manifest Processing
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
			CPU:              stringPtr("100m"),
			Memory:           stringPtr("128Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("150m"),
			Memory:           stringPtr("192Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
	ResourcePresetMicro: {
		"requests": {
			CPU:              stringPtr("250m"),
			Memory:           stringPtr("256Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("375m"),
			Memory:           stringPtr("384Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
	ResourcePresetSmall: {
		"requests": {
			CPU:              stringPtr("500m"),
			Memory:           stringPtr("512Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("750m"),
			Memory:           stringPtr("768Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
	ResourcePresetMedium: {
		"requests": {
			CPU:              stringPtr("500m"),
			Memory:           stringPtr("1024Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("750m"),
			Memory:           stringPtr("1536Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
	ResourcePresetLarge: {
		"requests": {
			CPU:              stringPtr("1000m"),
			Memory:           stringPtr("2048Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("1500m"),
			Memory:           stringPtr("3072Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
	ResourcePresetXLarge: {
		"requests": {
			CPU:              stringPtr("1000m"),
			Memory:           stringPtr("3072Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("3000m"),
			Memory:           stringPtr("6144Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
	ResourcePreset2XLarge: {
		"requests": {
			CPU:              stringPtr("1000m"),
			Memory:           stringPtr("3072Mi"),
			EphemeralStorage: stringPtr("50Mi"),
		},
		"limits": {
			CPU:              stringPtr("6000m"),
			Memory:           stringPtr("12288Mi"),
			EphemeralStorage: stringPtr("2Gi"),
		},
	},
}

// stringPtr is a helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
