// Package manifest provides functions for parsing and manipulating manifest files.
package manifest

// DEPLOYAH_VARIABLE_PREFIX is used for variables that are used for template rendering
const DEPLOYAH_VARIABLE_PREFIX = "DPY_VAR_"

// DefaultEnvFile is the default environment file name
const DefaultEnvFile = ".env"

// DefaultConfigFile is the default configuration file name
const DefaultConfigFile = "config.yaml"

// ResourcePresetMappings defines the resource values for each preset
var ResourcePresetMappings = map[ResourcePreset]map[string]Resources{
	ResourcePresetNano: {
		"requests": {
			CPU:              "100m",
			Memory:           "128Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "150m",
			Memory:           "192Mi",
			EphemeralStorage: "2Gi",
		},
	},
	ResourcePresetMicro: {
		"requests": {
			CPU:              "250m",
			Memory:           "256Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "375m",
			Memory:           "384Mi",
			EphemeralStorage: "2Gi",
		},
	},
	ResourcePresetSmall: {
		"requests": {
			CPU:              "500m",
			Memory:           "512Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "750m",
			Memory:           "768Mi",
			EphemeralStorage: "2Gi",
		},
	},
	ResourcePresetMedium: {
		"requests": {
			CPU:              "500m",
			Memory:           "1024Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "750m",
			Memory:           "1536Mi",
			EphemeralStorage: "2Gi",
		},
	},
	ResourcePresetLarge: {
		"requests": {
			CPU:              "1.0",
			Memory:           "2048Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "1.5",
			Memory:           "3072Mi",
			EphemeralStorage: "2Gi",
		},
	},
	ResourcePresetXLarge: {
		"requests": {
			CPU:              "1.0",
			Memory:           "3072Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "3.0",
			Memory:           "6144Mi",
			EphemeralStorage: "2Gi",
		},
	},
	ResourcePreset2XLarge: {
		"requests": {
			CPU:              "1.0",
			Memory:           "3072Mi",
			EphemeralStorage: "50Mi",
		},
		"limits": {
			CPU:              "6.0",
			Memory:           "12288Mi",
			EphemeralStorage: "2Gi",
		},
	},
}
