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

package cli

import (
	"context"
	"time"

	"github.com/dustin/go-humanize"

	"deployah.dev/deployah/internal/k8s"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ReleaseViewModel is the JSON and YAML output shape for Helm releases.
// Field values match the table output for the same release.
type ReleaseViewModel struct {
	Project      string         `json:"project" yaml:"project"`
	Environment  string         `json:"environment" yaml:"environment"`
	Release      string         `json:"release" yaml:"release"`
	Namespace    string         `json:"namespace" yaml:"namespace"`
	Status       string         `json:"status" yaml:"status"`
	Revision     int            `json:"revision" yaml:"revision"`
	LastDeployed string         `json:"lastDeployed" yaml:"lastDeployed"`
	Age          string         `json:"age" yaml:"age"`
	Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
	Values       map[string]any `json:"values,omitempty" yaml:"values,omitempty"`
	Notes        string         `json:"notes,omitempty" yaml:"notes,omitempty"`
	PodCount     int            `json:"podCount" yaml:"podCount"`
	ReadyPods    int            `json:"readyPods" yaml:"readyPods"`
	PodStatus    string         `json:"podStatus" yaml:"podStatus"` // e.g., "3/3", "2/3", "0/3"
}

// extractDeployahLabels extracts project and environment from deployah labels
func extractDeployahLabels(release *v1.Release) (project, environment string) {
	if release.Labels == nil {
		return "unknown", "unknown"
	}

	project = release.Labels["deployah.dev/project"]
	if project == "" {
		project = "unknown"
	}

	environment = release.Labels["deployah.dev/environment"]
	if environment == "" {
		environment = "unknown"
	}

	return project, environment
}

// getPodInfo retrieves pod information for a release
func getPodInfo(ctx context.Context, k8sClient *k8s.Client, release *v1.Release) (int, int, string) {
	if k8sClient == nil {
		return 0, 0, "0/0"
	}

	totalPods, readyPods, status, err := k8sClient.GetPodStatus(ctx, release.Name)
	if err != nil {
		return 0, 0, "0/0"
	}

	return totalPods, readyPods, status
}

// ReleaseToViewModel converts a Helm release to a view model for output.
func ReleaseToViewModel(rel *v1.Release) ReleaseViewModel {
	project, environment := extractDeployahLabels(rel)

	vm := ReleaseViewModel{
		Project:     project,
		Environment: environment,
		Release:     rel.Name,
		Namespace:   rel.Namespace,
		Revision:    int(rel.Version),
		Status:      "unknown",
	}

	if rel.Info != nil {
		vm.Status = rel.Info.Status.String()
		if !rel.Info.LastDeployed.IsZero() {
			vm.LastDeployed = rel.Info.LastDeployed.Format(time.RFC3339)
			vm.Age = humanize.Time(rel.Info.LastDeployed)
		}
		vm.Description = rel.Info.Description
		vm.Notes = rel.Info.Notes
	}

	if len(rel.Config) > 0 {
		vm.Values = rel.Config
	}

	return vm
}

// ReleaseToViewModelWithPods converts a Helm release to a view model with
// pod information.
func ReleaseToViewModelWithPods(ctx context.Context, k8sClient *k8s.Client, rel *v1.Release) ReleaseViewModel {
	vm := ReleaseToViewModel(rel)

	// Get pod information
	totalPods, readyPods, podStatus := getPodInfo(ctx, k8sClient, rel)
	vm.PodCount = totalPods
	vm.ReadyPods = readyPods
	vm.PodStatus = podStatus

	return vm
}
