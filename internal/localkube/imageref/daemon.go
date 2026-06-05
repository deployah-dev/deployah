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

package imageref

import (
	"context"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// openFromDaemon tries to export an image from the local container daemon
// (Docker or Podman). Podman exposes a Docker-compatible socket when
// "podman system service" is running; set DOCKER_HOST accordingly.
func openFromDaemon(ctx context.Context, ref name.Reference) (io.ReadCloser, error) {
	img, err := daemon.Image(ref, daemon.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	return tarballPipe(ref, img), nil
}

// tarballPipe streams img as a Docker tar archive via an [io.Pipe] so the
// caller can start reading while the archive is being written.
//
// The goroutine is panic-safe: any panic in tarball.Write is recovered and
// propagated as an error to the reader via PipeWriter.CloseWithError, so the
// reader is never left hanging. See https://pkg.go.dev/io#Pipe for the
// CloseWithError contract (idempotent, never overwrites a previous error).
func tarballPipe(ref name.Reference, img v1.Image) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("imageref: tarball panic: %v", r)
			}
			pw.CloseWithError(err) // idempotent; nil becomes EOF for the reader
		}()
		err = tarball.Write(ref, img, pw)
	}()
	return pr
}
