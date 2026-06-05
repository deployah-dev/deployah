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

package imageref_test

import (
	"context"
	"log"

	"deployah.dev/deployah/internal/localkube/imageref"
)

// ExampleOpen resolves a local file, daemon image, or registry image
// to a tar stream.
func ExampleOpen() {
	// Resolve any of: local file path, local daemon image, remote registry image.
	rc, err := imageref.Open(context.Background(), "myapp:latest")
	if err != nil {
		log.Fatal(err)
	}
	if closeErr := rc.Close(); closeErr != nil {
		log.Fatal(closeErr)
	}

	// rc is an io.ReadCloser of a Docker/OCI tar archive.
	// Pass it directly to localkube.Manager.LoadImageArchive.
}
