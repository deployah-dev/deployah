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

package runtime

import "time"

// Runtime Defaults
const (
	// DefaultTimeout is the default timeout for Helm operations
	DefaultTimeout = 10 * time.Minute

	// DefaultStorageDriver is the default Helm storage driver
	DefaultStorageDriver = "secret"

	// DefaultNamespace is used when no namespace is specified
	DefaultNamespace = "default"
)

// Helm Configuration
const (
	// HelmStorageDriverSecret uses Kubernetes secrets for Helm storage
	HelmStorageDriverSecret = "secret"

	// HelmStorageDriverConfigMap uses Kubernetes ConfigMaps for Helm storage
	HelmStorageDriverConfigMap = "configmap"

	// HelmStorageDriverMemory uses in-memory storage for Helm (testing only)
	HelmStorageDriverMemory = "memory"

	// HelmTimeoutMin is the minimum allowed timeout for Helm operations
	HelmTimeoutMin = 30 * time.Second

	// HelmTimeoutMax is the maximum allowed timeout for Helm operations
	HelmTimeoutMax = 60 * time.Minute
)

// Environment Variables
const (
	// KubeConfigEnvVar is the environment variable for kubeconfig path
	KubeConfigEnvVar = "KUBECONFIG"

	// NamespaceEnvVar is the environment variable for default namespace
	NamespaceEnvVar = "DPY_NAMESPACE"

	// DebugEnvVar is the environment variable for enabling debug mode
	DebugEnvVar = "DPY_DEBUG"
)

// Validation Functions

// ValidateTimeout ensures timeout is within acceptable bounds.
func ValidateTimeout(timeout time.Duration) bool {
	return timeout >= HelmTimeoutMin && timeout <= HelmTimeoutMax
}

// ValidateStorageDriver ensures the storage driver is valid.
func ValidateStorageDriver(driver string) bool {
	switch driver {
	case HelmStorageDriverSecret, HelmStorageDriverConfigMap, HelmStorageDriverMemory:
		return true
	default:
		return false
	}
}

// GetValidStorageDrivers returns a list of valid storage drivers.
func GetValidStorageDrivers() []string {
	return []string{
		HelmStorageDriverSecret,
		HelmStorageDriverConfigMap,
		HelmStorageDriverMemory,
	}
}
