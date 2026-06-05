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

// Package imageref resolves an image reference to an [io.ReadCloser] of the
// image's Docker/OCI tar archive.
//
// Resolution order:
//  1. File path: the ref is tested against the filesystem first (os.Stat).
//     Refs with prefix "/", "./", or "../", or suffix ".tar", ".tar.gz",
//     ".tar.zst", or ".tgz" that don't exist on disk fall through to step 2.
//  2. Local container daemon (Docker or Podman socket).
//  3. Remote OCI registry.
//
// The returned reader is suitable for piping directly into
// localkube.Manager.LoadImageArchive.
package imageref

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
)

// openers bundles the two pluggable resolution functions so tests can
// substitute fakes without global mutable state.
type openers struct {
	daemon   func(ctx context.Context, ref name.Reference) (io.ReadCloser, error)
	registry func(ctx context.Context, ref name.Reference) (io.ReadCloser, error)
}

var defaultOpeners = openers{
	daemon:   openFromDaemon,
	registry: openFromRegistry,
}

// Open resolves ref and returns an [io.ReadCloser] streaming the image as a
// Docker/OCI tar archive. The caller is responsible for closing the reader.
//
// Resolution order: file path → local daemon → remote registry.
//
// When both daemon and registry fail, both errors are joined so callers and
// log output see the full picture. go-containerregistry returns an error for
// images not found in the daemon (fixed in
// https://github.com/google/go-containerregistry/pull/1272), but the errors
// are still untyped, so we cannot cheaply distinguish a reachable-daemon-
// missing-image from an unreachable daemon without an explicit image-ID probe.
//
// Example:
//
//	rc, err := imageref.Open(ctx, "myapp:latest")
//	if err != nil { ... }
//	defer rc.Close()
//	err = m.LoadImageArchive(ctx, "dev", rc)
func Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	return openWith(ctx, ref, defaultOpeners)
}

// openWith is the testable core of Open. Tests pass a custom openers value.
func openWith(ctx context.Context, ref string, op openers) (io.ReadCloser, error) {
	// Step 1: stat the path; if it's a regular file, open it directly.
	// This replaces the old suffix-heuristic and handles files without a
	// recognizable extension as well as refs that look like registry names
	// but happen to exist on disk.
	if info, statErr := os.Stat(ref); statErr == nil && info.Mode().IsRegular() {
		f, err := os.Open(ref) // #nosec G304 -- ref is an explicit caller-provided file path
		if err != nil {
			return nil, fmt.Errorf("imageref: open file %q: %w", ref, err)
		}
		return f, nil
	}

	// Step 2 & 3: parse as an image reference, try daemon then registry.
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("imageref: invalid reference %q: %w", ref, err)
	}

	rc, daemonErr := op.daemon(ctx, parsed)
	if daemonErr == nil {
		return rc, nil
	}

	rc, regErr := op.registry(ctx, parsed)
	if regErr != nil {
		return nil, fmt.Errorf("imageref: %q: %w", ref, errors.Join(daemonErr, regErr))
	}
	return rc, nil
}
