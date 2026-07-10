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

package spec_test

import (
	"context"
	"fmt"
	"log"
	"os"

	"deployah.dev/deployah/internal/spec"
)

// ExampleFillSpecWithDefaults applies schema defaults to a minimal manifest.
func ExampleFillSpecWithDefaults() {
	m := &spec.Spec{
		APIVersion: "v1-alpha.2",
		Project:    "demo",
		Components: map[string]spec.Component{
			"web": {Image: "nginx:latest"},
		},
	}
	if err := spec.FillSpecWithDefaults(m, "v1-alpha.2"); err != nil {
		log.Fatal(err)
	}
	fmt.Println(m.Components["web"].Port)
	// Output: 8080
}

// ExampleLoad reads a manifest file from disk.
func ExampleLoad() {
	const yamlDoc = `apiVersion: v1-alpha.2
project: demo
environments:
  default: {}
components:
  web:
    image: nginx:latest
    resources:
      cpu: 500m
      memory: 512Mi
`
	f, err := os.CreateTemp("", "example-*.yaml")
	if err != nil {
		log.Fatal(err)
	}
	path := f.Name()
	if _, err = f.WriteString(yamlDoc); err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
		log.Fatal(err)
	}
	if err = f.Close(); err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
		log.Fatal(err)
	}

	m, err := spec.Load(context.Background(), path, "", nil)
	if err != nil {
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
		log.Fatal(err)
	}
	defer func() {
		if rmErr := os.Remove(path); rmErr != nil {
			log.Print(rmErr)
		}
	}()
	fmt.Println(m.Project)
	// Output: demo
}
