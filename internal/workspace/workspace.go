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
	"cmp"
	"context"
	"fmt"
	"sync"

	"deployah.dev/deployah/internal/spec"
)

// Config holds source-location inputs for a [Workspace].
type Config struct {
	// SpecPath is the Deployah spec file. Empty means [spec.DefaultSpecPath].
	SpecPath string
	// PlatformPath is an explicit platform file. Empty means
	// DEPLOYAH_PLATFORM_FILE, then the default file next to the spec.
	PlatformPath string
}

// Workspace owns the Deployah project source locations for one invocation
// and loads the spec and optional platform configuration from those sources.
//
// [New] snapshots the effective spec path and platform source selection,
// including whether the platform path is required. Later changes to
// DEPLOYAH_PLATFORM_FILE do not retarget an existing Workspace.
//
// Concurrent calls to [Workspace.SpecPath], [Workspace.PlatformPath],
// [Workspace.Platform], [Workspace.ParseManifest], and [Workspace.LoadSpec]
// are safe.
type Workspace struct {
	specPath       string
	platformSource platformSource
	platform       *spec.PlatformConfig
	mu             sync.Mutex
}

// New returns a Workspace that snapshots config and the current
// DEPLOYAH_PLATFORM_FILE value. It does not read the spec or platform files.
func New(config Config) *Workspace {
	specPath := cmp.Or(config.SpecPath, spec.DefaultSpecPath)
	return &Workspace{
		specPath:       specPath,
		platformSource: selectPlatformSource(config.PlatformPath, specPath),
	}
}

// SpecPath returns the effective spec file path. It is never empty.
func (w *Workspace) SpecPath() string {
	return w.specPath
}

// ParseManifest reads the spec at [Workspace.SpecPath] without envsubst,
// defaults, or platform resolution.
func (w *Workspace) ParseManifest() (*spec.Spec, error) {
	manifest, _, err := spec.ParseManifest(w.specPath)
	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// LoadSpec loads the spec for environment from [Workspace.SpecPath] using
// the Workspace platform and opts. Each call reads the spec from disk
// because envsubst depends on environment. The [spec.SubstitutionReport]
// is the report produced by that [spec.Load] call.
func (w *Workspace) LoadSpec(ctx context.Context, environment string, opts ...spec.LoadOption) (*spec.Spec, spec.SubstitutionReport, error) {
	platform, err := w.Platform()
	if err != nil {
		return nil, spec.SubstitutionReport{}, fmt.Errorf("failed to load platform file: %w", err)
	}
	manifest, report, err := spec.Load(ctx, w.specPath, environment, platform, opts...)
	if err != nil {
		return nil, spec.SubstitutionReport{}, fmt.Errorf("failed to load spec: %w", err)
	}
	return manifest, report, nil
}
