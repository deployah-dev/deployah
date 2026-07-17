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

package readiness

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ComponentStatus summarizes pod readiness for one Deployah component.
type ComponentStatus struct {
	// Name is the component name from the Deployah spec.
	Name string
	// ReadyPods is the number of pods that passed readiness checks.
	ReadyPods int
	// TotalPods is the total number of pods for this component.
	TotalPods int
}

// Poll lists the pods belonging to releaseName in namespace and groups them
// by Deployah component (falling back to the standard
// "app.kubernetes.io/component" label, then the release name itself, when a
// pod carries neither), counting how many of each component's pods are
// ready per [IsPodReady].
func Poll(ctx context.Context, k8sClient kubernetes.Interface, namespace, releaseName string) ([]ComponentStatus, error) {
	pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + releaseName,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	type counts struct{ ready, total int }
	byComponent := make(map[string]*counts)

	for _, pod := range pods.Items {
		comp := pod.Labels[k8s.ComponentLabel]
		if comp == "" {
			comp = pod.Labels["app.kubernetes.io/component"]
		}
		if comp == "" {
			comp = releaseName
		}

		c := byComponent[comp]
		if c == nil {
			c = &counts{}
			byComponent[comp] = c
		}
		c.total++
		if IsPodReady(&pod) {
			c.ready++
		}
	}

	statuses := make([]ComponentStatus, 0, len(byComponent))
	for name, c := range byComponent {
		statuses = append(statuses, ComponentStatus{Name: name, ReadyPods: c.ready, TotalPods: c.total})
	}
	// byComponent is a map, so iteration order is randomized; sort by name
	// for a stable display across polls and across runs.
	slices.SortFunc(statuses, func(a, b ComponentStatus) int {
		return strings.Compare(a.Name, b.Name)
	})
	return statuses, nil
}

// IsPodReady reports whether pod is running and every container inside it
// reports ready.
func IsPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
}

// AllReady reports whether every component in statuses has all of its pods
// ready. It returns false for an empty slice: no data is not the same as
// "ready".
func AllReady(statuses []ComponentStatus) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, s := range statuses {
		if s.ReadyPods < s.TotalPods {
			return false
		}
	}
	return true
}

// Summary formats statuses as a comma-separated "name: ready/total" list,
// e.g. "api: 2/2, worker: 1/2". It returns "" for an empty slice.
func Summary(statuses []ComponentStatus) string {
	if len(statuses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(statuses))
	for _, s := range statuses {
		parts = append(parts, fmt.Sprintf("%s: %d/%d", s.Name, s.ReadyPods, s.TotalPods))
	}
	return strings.Join(parts, ", ")
}
