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

package workspace

import (
	"os"
	"path/filepath"

	"deployah.dev/deployah/internal/spec"
)

// platformSource is the platform file selected at Workspace construction.
type platformSource struct {
	path     string
	required bool
}

func selectPlatformSource(explicitPath, specPath string) platformSource {
	if explicitPath != "" {
		return platformSource{path: explicitPath, required: true}
	}
	if envPath := os.Getenv(spec.PlatformEnvVar); envPath != "" {
		return platformSource{path: envPath, required: true}
	}
	return platformSource{
		path:     filepath.Join(filepath.Dir(specPath), spec.DefaultPlatformPath),
		required: false,
	}
}

// PlatformPath returns the snapshotted platform file path.
func (w *Workspace) PlatformPath() string {
	return w.platformSource.path
}

// Platform loads and memoizes the platform configuration from the
// snapshotted source.
//
// A successful load is memoized. A missing optional adjacent file returns
// (nil, nil). A missing or unreadable required source (explicit path or
// DEPLOYAH_PLATFORM_FILE) returns an error. Failed loads and the optional
// miss are not cached.
func (w *Workspace) Platform() (*spec.PlatformConfig, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.platform != nil {
		return w.platform, nil
	}
	path := w.platformSource.path
	if !w.platformSource.required {
		if _, err := os.Stat(path); err != nil {
			return nil, nil //nolint:nilnil // absent optional platform file is not an error; callers check for nil config
		}
	}
	p, err := spec.LoadPlatform(path)
	if err != nil {
		return nil, err
	}
	w.platform = p
	return p, nil
}
