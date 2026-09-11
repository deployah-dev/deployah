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

package helm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"helm.sh/helm/v4/pkg/kube"
)

func TestManagedFieldsManager_PinnedAtInit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "deployah", kube.ManagedFieldsManager)
}

// TestManagedFieldsManager_ParallelNewClient races many constructors against
// the process-global field manager. NewClient must not write the global;
// after concurrent Init the manager stays "deployah".
func TestManagedFieldsManager_ParallelNewClient(t *testing.T) {
	const n = 32
	var g errgroup.Group
	for range n {
		g.Go(func() error {
			_, err := NewClient(
				WithStorageDriver("memory"),
				WithNamespace("default"),
			)
			return err
		})
	}
	require.NoError(t, g.Wait())
	assert.Equal(t, "deployah", kube.ManagedFieldsManager)
}
