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
	"fmt"
	"log"
	"os"

	"deployah.dev/deployah/internal/localkube/imageref"
)

// ExampleOpen resolves a local file, daemon image, or registry image
// to a tar stream.
func ExampleOpen() {
	f, err := os.CreateTemp("", "example-*.tar")
	if err != nil {
		log.Fatal(err)
	}
	path := f.Name()
	if err = f.Close(); err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
		log.Fatal(err)
	}

	rc, err := imageref.Open(context.Background(), path)
	if err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
		log.Fatal(err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			log.Print(closeErr)
		}
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
	}()

	fmt.Println("resolved file")
	// Output: resolved file
}
