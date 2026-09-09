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

// Package testenv isolates process environment for unit tests.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// IsolatedKubeconfig pins HOME and KUBECONFIG to a throwaway directory
// so client-go kubeconfig migration cannot touch the developer's
// ~/.kube/config. Call it from TestMain and return its exit code.
func IsolatedKubeconfig(m *testing.M) (code int) {
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

	if err = os.Setenv("HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "set HOME: %v\n", err)
		return 1
	}
	if err = os.Setenv("KUBECONFIG", filepath.Join(home, "kubeconfig")); err != nil {
		fmt.Fprintf(os.Stderr, "set KUBECONFIG: %v\n", err)
		return 1
	}
	return m.Run()
}
