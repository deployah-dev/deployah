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

// Package k8s provides functions to interact with Kubernetes pods.
package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetRunningPods gets running pods for the specified project, component, and environment
func (c *Client) GetRunningPods(ctx context.Context, project, component, environment string) ([]PodInfo, error) {
	selector, err := BuildSelector(project, component, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to build selector: %w", err)
	}

	// Get pods
	pods, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	// Convert to PodInfo
	podInfos := make([]PodInfo, 0, len(pods.Items))
	for _, pod := range pods.Items {
		containers := make([]string, 0, len(pod.Spec.Containers))
		for _, container := range pod.Spec.Containers {
			containers = append(containers, container.Name)
		}

		podInfos = append(podInfos, PodInfo{
			Name:       pod.Name,
			Namespace:  pod.Namespace,
			Containers: containers,
			Status:     string(pod.Status.Phase),
		})
	}

	return podInfos, nil
}

// GetPodInfo retrieves detailed information about a specific pod
func (c *Client) GetPodInfo(ctx context.Context, podName string) (*PodInfo, error) {
	pod, err := c.k8sClient.CoreV1().Pods(c.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	containers := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		containers = append(containers, container.Name)
	}

	return &PodInfo{
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		Containers: containers,
		Status:     string(pod.Status.Phase),
	}, nil
}

// GetPodStatus retrieves pod status information for a release
func (c *Client) GetPodStatus(ctx context.Context, releaseName string) (int, int, string, error) {
	// Create label selector for pods belonging to this release
	selector := fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName)

	pods, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return 0, 0, "0/0", fmt.Errorf("failed to list pods for release %s: %w", releaseName, err)
	}

	totalPods := len(pods.Items)
	readyPods := 0

	for _, pod := range pods.Items {
		if pod.Status.Phase == "Running" {
			ready := true
			for _, container := range pod.Status.ContainerStatuses {
				if !container.Ready {
					ready = false
					break
				}
			}
			if ready {
				readyPods++
			}
		}
	}

	return totalPods, readyPods, fmt.Sprintf("%d/%d", readyPods, totalPods), nil
}

// ValidatePodExists checks if a pod with the given name exists
func (c *Client) ValidatePodExists(ctx context.Context, podName string) error {
	_, err := c.k8sClient.CoreV1().Pods(c.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("pod %s not found: %w", podName, err)
	}
	return nil
}
