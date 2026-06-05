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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/google/renameio/v2"
)

// DefaultKubeconfigPath returns the path where deployah stores the kubeconfig
// for the named cluster, using the default XDG state directory.
// The file may not exist yet; no I/O is performed.
func DefaultKubeconfigPath(name string) (string, error) {
	s, err := newKubeconfigStore("")
	if err != nil {
		return "", err
	}
	return s.kubeconfigPath(name), nil
}

// safeClusterName rejects names that aren't safe single filesystem components.
//
// [filepath.IsLocal] (Go 1.20+) rejects empty strings, absolute paths, "..",
// and Windows-reserved names. We additionally ban any path separator so a
// name like "a/b" cannot escape the state root.
func safeClusterName(name string) error {
	if strings.ContainsAny(name, `/\`) || !filepath.IsLocal(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}

// kubeconfigStore atomically writes kubeconfig copies to a configurable dir.
type kubeconfigStore struct {
	root string
}

// newKubeconfigStore resolves the storage root and ensures it exists.
// When dir is empty, the XDG state home is used.
func newKubeconfigStore(dir string) (*kubeconfigStore, error) {
	var root string
	if dir != "" {
		root = dir
	} else {
		probe, err := xdg.StateFile(stateSubdirKubeconfigs + "/.keep")
		if err != nil {
			return nil, fmt.Errorf("resolve xdg kubeconfig dir: %w", err)
		}
		root = filepath.Dir(probe)
	}
	return &kubeconfigStore{root: root}, nil
}

// kubeconfigPath returns the path for a cluster's kubeconfig copy.
func (s *kubeconfigStore) kubeconfigPath(name string) string {
	return filepath.Join(s.root, name+".yaml")
}

// remove deletes a cluster's kubeconfig file and its lock file.
// Missing files are not treated as errors.
func (s *kubeconfigStore) remove(name string) error {
	path := s.kubeconfigPath(name)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove kubeconfig %s: %w", name, err)
	}
	if err := os.Remove(path + ".lock"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove kubeconfig lock %s: %w", name, err)
	}
	return nil
}

// writeKubeconfig atomically writes kubeconfig bytes to the store root.
// renameio.WriteFile writes to a temp file and renames it into place, so
// concurrent writers never observe a partial file. No lock file is created,
// which keeps the store from colliding with Kind's own <name>.yaml.lock.
func (s *kubeconfigStore) writeKubeconfig(name string, data []byte) (string, error) {
	path := s.kubeconfigPath(name)
	if err := renameio.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write kubeconfig %s: %w", name, err)
	}
	return path, nil
}
