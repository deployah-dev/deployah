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

package localkube

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestSafeClusterName covers the valid and invalid inputs for safeClusterName.
func TestSafeClusterName(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"", false},
		{"..", false},
		{"../x", false},
		{"a/b", false},
		{`a\b`, false},
		{"/absolute", false},
		{"valid", true},
		{"valid-1", true},
		{"my_cluster", true},
	}
	for _, tc := range cases {
		err := safeClusterName(tc.name)
		if tc.valid {
			assert.NoError(t, err, "expected %q to be valid", tc.name)
		} else {
			assert.ErrorIs(t, err, ErrInvalidName, "expected %q to be invalid", tc.name)
		}
	}
}

func newTestKubeconfigStore(t *testing.T) *kubeconfigStore {
	t.Helper()
	store, err := newKubeconfigStore(t.TempDir())
	require.NoError(t, err)
	return store
}

// TestKubeconfigStore_writeAndRead atomically writes a kubeconfig and
// reads it back.
func TestKubeconfigStore_writeAndRead(t *testing.T) {
	s := newTestKubeconfigStore(t)
	raw := []byte("apiVersion: v1\nkind: Config\n")

	path, err := s.writeKubeconfig("dev", raw)
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	got, err := os.ReadFile(path) // #nosec G304 -- path is our own temp file
	require.NoError(t, err)
	assert.Equal(t, raw, got)
}

// TestKubeconfigStore_path returns the expected path for a cluster name.
func TestKubeconfigStore_path(t *testing.T) {
	dir := t.TempDir()
	s, err := newKubeconfigStore(dir)
	require.NoError(t, err)

	p := s.kubeconfigPath("dev")
	assert.Equal(t, dir+"/dev.yaml", p)
}

// TestKubeconfigStore_concurrentWrites verifies that concurrent writes to the
// same kubeconfig file don't corrupt it.
func TestKubeconfigStore_concurrentWrites(t *testing.T) {
	s := newTestKubeconfigStore(t)
	raw := []byte("apiVersion: v1\nkind: Config\n")

	var g errgroup.Group
	for range 8 {
		g.Go(func() error {
			_, err := s.writeKubeconfig("dev", raw)
			return err
		})
	}
	require.NoError(t, g.Wait())

	path := s.kubeconfigPath("dev")
	got, err := os.ReadFile(path) // #nosec G304 -- path is our own temp file
	require.NoError(t, err)
	assert.Equal(t, raw, got)
}

// TestKubeconfigStore_multipleNames writes kubeconfigs for different clusters.
func TestKubeconfigStore_multipleNames(t *testing.T) {
	s := newTestKubeconfigStore(t)

	pairs := [][2]string{
		{"dev", "apiVersion: v1\n"},
		{"staging", "apiVersion: v1\nkind: Config\n"},
	}
	for _, p := range pairs {
		_, err := s.writeKubeconfig(p[0], []byte(p[1]))
		require.NoError(t, err)
	}
	for _, p := range pairs {
		got, err := os.ReadFile(s.kubeconfigPath(p[0]))
		require.NoError(t, err)
		assert.Equal(t, []byte(p[1]), got)
	}
}
