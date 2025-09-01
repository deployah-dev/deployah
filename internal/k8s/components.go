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

// Package k8s provides functions to interact with Kubernetes components.
package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetAvailableComponents gets all available components from running pods for a specific project
func (c *Client) GetAvailableComponents(ctx context.Context, projectName string) ([]string, error) {
	selector, err := BuildProjectSelector(projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to build project selector: %w", err)
	}

	// Get all pods with deployah.dev/project label in the namespace
	pods, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Extract unique component names from pod labels
	componentSet := make(map[string]bool)
	for _, pod := range pods.Items {
		if componentName, exists := pod.Labels[ComponentLabel]; exists {
			componentSet[componentName] = true
		}
	}

	// Convert to slice
	components := make([]string, 0, len(componentSet))
	for component := range componentSet {
		components = append(components, component)
	}

	return components, nil
}

// ValidateComponentExists checks if a component exists and has running pods for the given project
func (c *Client) ValidateComponentExists(ctx context.Context, projectName, componentName string) error {
	pods, err := c.GetRunningPods(ctx, projectName, componentName, "")
	if err != nil {
		return fmt.Errorf("failed to validate component '%s': %w", componentName, err)
	}
	if len(pods) == 0 {
		return fmt.Errorf("component '%s' not found or has no running pods in project '%s'", componentName, projectName)
	}
	return nil
}

// GetComponentInfo gets detailed information about a specific component
func (c *Client) GetComponentInfo(ctx context.Context, projectName, componentName string) (*ComponentInfo, error) {
	pods, err := c.GetRunningPods(ctx, projectName, componentName, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get pods for component: %w", err)
	}

	readyPods := 0
	for _, pod := range pods {
		if pod.Status == "Running" {
			readyPods++
		}
	}

	return &ComponentInfo{
		Name:      componentName,
		Project:   projectName,
		PodCount:  len(pods),
		ReadyPods: readyPods,
	}, nil
}

// GetProjectComponents gets all components for a project with their information
func (c *Client) GetProjectComponents(ctx context.Context, projectName string) ([]ComponentInfo, error) {
	components, err := c.GetAvailableComponents(ctx, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to get available components: %w", err)
	}

	componentInfos := make([]ComponentInfo, 0, len(components))
	for _, componentName := range components {
		info, err := c.GetComponentInfo(ctx, projectName, componentName)
		if err != nil {
			// Log error but continue with other components
			continue
		}
		componentInfos = append(componentInfos, *info)
	}

	return componentInfos, nil
}

// GetAvailableEnvironments gets all available environments from running pods for a specific project and component
func (c *Client) GetAvailableEnvironments(ctx context.Context, projectName, componentName string) ([]string, error) {
	selector, err := BuildComponentSelector(projectName, componentName)
	if err != nil {
		return nil, fmt.Errorf("failed to build component selector: %w", err)
	}

	// Get all pods with deployah.dev/project and deployah.dev/component labels in the namespace
	pods, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Extract unique environment names from pod labels
	environmentSet := make(map[string]bool)
	for _, pod := range pods.Items {
		if environmentName, exists := pod.Labels[EnvironmentLabel]; exists {
			environmentSet[environmentName] = true
		}
	}

	// Convert to slice
	environments := make([]string, 0, len(environmentSet))
	for environment := range environmentSet {
		environments = append(environments, environment)
	}

	return environments, nil
}
