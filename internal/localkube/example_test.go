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
	"context"
	"fmt"
	"log"
	"os"

	"deployah.dev/deployah/internal/localkube"
)

func mustManager() *localkube.Manager {
	m, err := localkube.New()
	if err != nil {
		log.Fatal(err)
	}
	return m
}

// ExampleNew constructs a Manager with runtime and Kubernetes version options.
func ExampleNew() {
	m, err := localkube.New(
		localkube.WithRuntime(localkube.RuntimePodman),
		localkube.WithKubernetesVersion("1.31"),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = m
}

// ExampleManager_Create creates a cluster with port mappings and event callbacks.
func ExampleManager_Create() {
	m := mustManager()

	err := m.Create(context.Background(), "dev",
		localkube.WithPortMappings(localkube.PortMapping{HostPort: 8080, ContainerPort: 80}),
		localkube.WithCreateEventHandler(func(e localkube.Event) {
			fmt.Printf("step=%s status=%v\n", e.Step, e.Status)
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
}

// ExampleManager_KubeConfig writes or reads the cluster kubeconfig.
func ExampleManager_KubeConfig() {
	m := mustManager()

	kc, err := m.KubeConfig(context.Background(), "dev")
	if err != nil {
		log.Fatal(err)
	}

	if _, writeErr := kc.WriteTo(os.Stdout); writeErr != nil {
		log.Fatal(writeErr)
	}

	// Or hand the stable path to a tool that needs a file.
	fmt.Println(kc.Path())
}

// ExampleManager_LoadImage loads a daemon image into every cluster node.
func ExampleManager_LoadImage() {
	m := mustManager()

	if err := m.LoadImage(context.Background(), "dev", "myapp:latest"); err != nil {
		log.Fatal(err)
	}
}

// ExampleManager_LoadImageArchive loads a tar archive into every cluster node.
func ExampleManager_LoadImageArchive() {
	m := mustManager()

	f, err := os.Open("myapp.tar")
	if err != nil {
		log.Fatal(err)
	}

	if err = m.LoadImageArchive(context.Background(), "dev", f); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			log.Fatal(closeErr)
		}
		log.Fatal(err)
	}
	if closeErr := f.Close(); closeErr != nil {
		log.Fatal(closeErr)
	}
}
