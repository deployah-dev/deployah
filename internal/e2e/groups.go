// Copyright 2026 The Deployah Authors
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

package e2e

import "testing"

// RunGroups runs parallel items together, then sequential items.
func RunGroups[T any](t *testing.T, parallel, sequential []T, name func(T) string, run func(*testing.T, T)) {
	t.Helper()
	// Sequential items start after the parallel group finishes.
	// Only the parallel items call t.Parallel.
	t.Run("parallel", func(t *testing.T) {
		for _, item := range parallel {
			t.Run(name(item), func(t *testing.T) {
				t.Parallel()
				run(t, item)
			})
		}
	})
	t.Run("sequential", func(t *testing.T) {
		for _, item := range sequential {
			t.Run(name(item), func(t *testing.T) {
				run(t, item)
			})
		}
	})
}
