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

// Package k8s provides functions to interact with Kubernetes types.
package k8s

// PodInfo contains information about a pod
type PodInfo struct {
	Name       string
	Namespace  string
	Containers []string
	Status     string
}

// ComponentInfo contains information about a component
type ComponentInfo struct {
	Name      string
	Project   string
	PodCount  int
	ReadyPods int
}

// ProjectInfo contains information about a project
type ProjectInfo struct {
	Name        string
	Environment string
	Components  []string
	PodCount    int
	ReadyPods   int
}

// Label constants for Deployah resources
const (
	ProjectLabel     = "deployah.dev/project"
	ComponentLabel   = "deployah.dev/component"
	EnvironmentLabel = "deployah.dev/environment"
)
