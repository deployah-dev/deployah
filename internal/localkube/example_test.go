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

package localkube_test

import (
	"fmt"
	"log"
	"os"

	"deployah.dev/deployah/internal/localkube"
)

// ExampleNew constructs a Manager with runtime and Kubernetes version options.
func ExampleNew() {
	m, err := localkube.New(
		localkube.WithRuntime(localkube.RuntimePodman),
		localkube.WithKubernetesVersion("1.31"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(m != nil)
	// Output: true
}

// ExampleManager_Create shows create options and the progress event shape.
// A live cluster is required to run Manager.Create itself.
func ExampleManager_Create() {
	handler := func(e localkube.Event) {
		fmt.Printf("step=%s status=%d\n", e.Step, e.Status)
	}
	handler(localkube.Event{Step: localkube.StepCreating, Status: localkube.StepStarted})

	_ = []localkube.CreateOption{
		localkube.WithPortMappings(localkube.PortMapping{HostPort: 8080, ContainerPort: 80}),
		localkube.WithCreateEventHandler(handler),
	}
	// Output: step=creating status=0
}

// ExampleManager_KubeConfig shows where Manager.KubeConfig writes the file.
// A live cluster is required to fetch kubeconfig bytes.
func ExampleManager_KubeConfig() {
	path, err := localkube.DefaultKubeconfigPath("dev")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("path ok: %v\n", len(path) > 0)
	// Output: path ok: true
}

// ExampleManager_LoadImage shows the resolving-image event emitted during load.
// A live cluster is required to run Manager.LoadImage itself.
func ExampleManager_LoadImage() {
	e := localkube.Event{
		Step:   localkube.StepResolvingImage,
		Status: localkube.StepStarted,
		Detail: "myapp:latest",
	}
	fmt.Printf("step=%s status=%d ref=%s\n", e.Step, e.Status, e.Detail)
	// Output: step=resolving-image status=0 ref=myapp:latest
}

// ExampleManager_LoadImageArchive opens a tar archive for loading.
// A live cluster is required to run Manager.LoadImageArchive itself.
func ExampleManager_LoadImageArchive() {
	f, err := os.CreateTemp("", "example-*.tar")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Print(closeErr)
		}
		if rmErr := os.Remove(f.Name()); rmErr != nil {
			log.Print(rmErr)
		}
	}()

	fmt.Printf("opened archive: %v\n", f != nil)
	// Output: opened archive: true
}
