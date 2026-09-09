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

// Package testenv isolates kubeconfig-related process and client-go
// global state for unit tests.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

// RunWithIsolatedKubeconfig runs m with HOME and client-go recommended
// kubeconfig paths pointed at a temporary directory. KUBECONFIG is
// unset so default resolution is used. Process and client-go state
// are restored after m returns.
func RunWithIsolatedKubeconfig(m *testing.M) (code int) {
	home, err := os.MkdirTemp("", "deployah-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test home: %v\n", err)
		return 1
	}
	defer func() {
		if rmErr := os.RemoveAll(home); rmErr != nil {
			fmt.Fprintf(os.Stderr, "remove test home: %v\n", rmErr)
			if code == 0 {
				code = 1
			}
		}
	}()

	oldHome, homeSet := os.LookupEnv("HOME")
	oldKubeconfig, kubeconfigSet := os.LookupEnv("KUBECONFIG")
	oldConfigDir := clientcmd.RecommendedConfigDir
	oldHomeFile := clientcmd.RecommendedHomeFile
	oldSchemaFile := clientcmd.RecommendedSchemaFile
	defer func() {
		if restoreErr := restoreEnv("HOME", oldHome, homeSet); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "restore HOME: %v\n", restoreErr)
			if code == 0 {
				code = 1
			}
		}
		if restoreErr := restoreEnv("KUBECONFIG", oldKubeconfig, kubeconfigSet); restoreErr != nil {
			fmt.Fprintf(os.Stderr, "restore KUBECONFIG: %v\n", restoreErr)
			if code == 0 {
				code = 1
			}
		}
		setRecommendedPaths(oldConfigDir, oldHomeFile, oldSchemaFile)
	}()

	if err = os.Setenv("HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "set HOME: %v\n", err)
		return 1
	}
	if err = os.Unsetenv("KUBECONFIG"); err != nil {
		fmt.Fprintf(os.Stderr, "unset KUBECONFIG: %v\n", err)
		return 1
	}

	configDir := filepath.Join(home, clientcmd.RecommendedHomeDir)
	setRecommendedPaths(
		configDir,
		filepath.Join(configDir, clientcmd.RecommendedFileName),
		filepath.Join(configDir, clientcmd.RecommendedSchemaName),
	)

	return m.Run()
}

func setRecommendedPaths(configDir, homeFile, schemaFile string) {
	clientcmd.RecommendedConfigDir = configDir   //nolint:reassign // client-go sets these at package init
	clientcmd.RecommendedHomeFile = homeFile     //nolint:reassign // client-go sets these at package init
	clientcmd.RecommendedSchemaFile = schemaFile //nolint:reassign // client-go sets these at package init
}

func restoreEnv(key, value string, wasSet bool) error {
	if !wasSet {
		return os.Unsetenv(key)
	}
	return os.Setenv(key, value)
}
