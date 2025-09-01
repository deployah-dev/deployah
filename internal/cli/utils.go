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
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/deployah-dev/deployah/internal/k8s"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/deployah-dev/deployah/internal/ui"
	"github.com/dustin/go-humanize"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ReleaseViewModel represents the curated output structure for json/yaml
// matching what is displayed in table mode.
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

// ReleaseToViewModel converts a Helm release to a view model for output
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
			vm.Age = humanize.Time(rel.Info.LastDeployed.Time)
		}
		vm.Description = rel.Info.Description
		vm.Notes = rel.Info.Notes
	}

	if len(rel.Config) > 0 {
		vm.Values = rel.Config
	}

	return vm
}

// ReleaseToViewModelWithPods converts a Helm release to a view model with pod information
func ReleaseToViewModelWithPods(ctx context.Context, k8sClient *k8s.Client, rel *v1.Release) ReleaseViewModel {
	vm := ReleaseToViewModel(rel)

	// Get pod information
	totalPods, readyPods, podStatus := getPodInfo(ctx, k8sClient, rel)
	vm.PodCount = totalPods
	vm.ReadyPods = readyPods
	vm.PodStatus = podStatus

	return vm
}

// GetTableColumns returns the detailed column configuration (with pod information)
func GetTableColumns(detailed bool) []ui.Column {
	return []ui.Column{
		{
			Title:    "PROJECT",
			Key:      "project",
			MinWidth: 10,
			MaxWidth: 20,
			StyleFunc: func(value string) lipgloss.Style {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorBrightWhite))
			},
			Condition: true,
		},
		{
			Title:    "ENV",
			Key:      "environment",
			MinWidth: 6,
			MaxWidth: 12,
			StyleFunc: func(value string) lipgloss.Style {
				return ui.GetEnvironmentStyle(value)
			},
			Condition: true,
		},
		{
			Title:    "STATUS",
			Key:      "status",
			MinWidth: 10,
			MaxWidth: 16,
			StyleFunc: func(value string) lipgloss.Style {
				return ui.GetStatusStyle(value)
			},
			Condition: true,
		},
		{
			Title:    "PODS",
			Key:      "pods",
			MinWidth: 8,
			MaxWidth: 12,
			StyleFunc: func(value string) lipgloss.Style {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorBrightCyan))
			},
			Condition: detailed,
		},
		{
			Title:    "READY",
			Key:      "ready",
			MinWidth: 8,
			MaxWidth: 15,
			StyleFunc: func(value string) lipgloss.Style {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorGreen))
			},
			Condition: detailed,
		},
		{
			Title: "REV",
			Key:   "revision",
			Width: 4,
			StyleFunc: func(value string) lipgloss.Style {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorGray))
			},
			Condition: true,
		},
		{
			Title:    "AGE",
			Key:      "age",
			MinWidth: 8,
			MaxWidth: 20,
			StyleFunc: func(value string) lipgloss.Style {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorGray))
			},
			Condition: true,
		},
		{
			Title:    "NAMESPACE",
			Key:      "namespace",
			MinWidth: 10,
			MaxWidth: 15,
			StyleFunc: func(value string) lipgloss.Style {
				return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorGray))
			},
			Condition: true,
		},
	}
}

// ColorizeJSONWithChroma applies syntax highlighting to JSON using chroma
func ColorizeJSONWithChroma(data []byte) (string, error) {
	// Check if we're outputting to a terminal that supports colors
	if !ui.IsTerminal() {
		return string(data), nil
	}

	// Use JSON lexer for syntax highlighting
	lexer := lexers.Get("json")
	if lexer == nil {
		lexer = lexers.Fallback
	}

	// Use a terminal-friendly style
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}

	// Create a terminal formatter
	formatter := formatters.Get("terminal")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	// Tokenize the JSON
	iterator, err := lexer.Tokenise(nil, string(data))
	if err != nil {
		return "", fmt.Errorf("failed to tokenize JSON: %w", err)
	}

	// Format with colors
	var result strings.Builder
	err = formatter.Format(&result, style, iterator)
	if err != nil {
		return "", fmt.Errorf("failed to format JSON: %w", err)
	}

	return result.String(), nil
}

// GetHelmClient initializes and returns a Helm client from the runtime context.
// This is a common utility function used across multiple commands.
func GetHelmClient(ctx context.Context) (runtime.HelmClient, error) {
	rt := runtime.FromRuntime(ctx)
	if rt == nil {
		return nil, fmt.Errorf("runtime not initialized")
	}

	helmClient, err := rt.Helm()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize helm client: %w", err)
	}

	return helmClient, nil
}
