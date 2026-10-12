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

import (
	"flag"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunGroups(t *testing.T) {
	// Overlap is only possible when go test may run two tests at once.
	// With -parallel=1 the scenarios still finish in group order.
	overlap := false
	if f := flag.Lookup("test.parallel"); f != nil {
		if n, err := strconv.Atoi(f.Value.String()); err == nil && n >= 2 {
			overlap = true
		}
	}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	if overlap {
		go func() {
			<-entered
			<-entered
			close(release)
		}()
	}

	var mu sync.Mutex
	var order []string
	activeSeq := 0
	inParallel := 0
	maxParallel := 0

	RunGroups(t, []string{"a", "b"}, []string{"c", "d"},
		func(name string) string { return name },
		func(t *testing.T, name string) {
			t.Helper()
			if name == "a" || name == "b" {
				mu.Lock()
				inParallel++
				if inParallel > maxParallel {
					maxParallel = inParallel
				}
				mu.Unlock()
				if overlap {
					entered <- struct{}{}
					<-release
				}
				mu.Lock()
				inParallel--
				order = append(order, name)
				mu.Unlock()
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if activeSeq != 0 {
				t.Errorf("sequential %s overlapped another sequential scenario", name)
			}
			activeSeq++
			order = append(order, name)
			activeSeq--
		})

	mu.Lock()
	got := append([]string(nil), order...)
	peak := maxParallel
	mu.Unlock()
	if overlap {
		require.GreaterOrEqual(t, peak, 2)
	}
	require.Len(t, got, 4)
	assert.ElementsMatch(t, []string{"a", "b"}, got[:2])
	assert.Equal(t, []string{"c", "d"}, got[2:])
}
