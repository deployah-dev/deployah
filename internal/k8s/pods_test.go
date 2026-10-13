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

package k8s

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/helm"

	corev1 "k8s.io/api/core/v1"
	k8stesting "k8s.io/client-go/testing"
)

func TestGetRunningPods_ReturnsMatchingPodInfo(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		podInfoPod("api-a", "apps", "shop", "api", "dev", "shop-dev", []string{"app", "sidecar"}),
		podInfoPod("api-b", "apps", "shop", "api", "dev", "shop-dev", []string{"app"}),
		podInfoPod("web", "apps", "shop", "web", "dev", "shop-dev", []string{"web"}),
		podInfoPod("other-project", "apps", "billing", "api", "dev", "billing-dev", []string{"app"}),
		podInfoPod("other-ns", "other", "shop", "api", "dev", "shop-dev", []string{"app"}),
	)
	// The fake client does not apply field selectors. The running-phase
	// filter is the list request sent to the API server.
	var field string
	cs.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		list, ok := action.(k8stesting.ListAction)
		if ok {
			field = list.GetListRestrictions().Fields.String()
		}
		return false, nil, nil
	})

	got, err := NewClient(cs, "apps").GetRunningPods(t.Context(), "shop", "api", "dev")
	require.NoError(t, err)
	assert.ElementsMatch(t, []PodInfo{
		{Name: "api-a", Namespace: "apps", Containers: []string{"app", "sidecar"}, Status: string(corev1.PodRunning)},
		{Name: "api-b", Namespace: "apps", Containers: []string{"app"}, Status: string(corev1.PodRunning)},
	}, got)
	assert.Equal(t, "status.phase=Running", field)
}

func TestGetRunningPods_NoneMatch(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		podInfoPod("web", "apps", "shop", "web", "dev", "shop-dev", []string{"web"}),
	)
	got, err := NewClient(cs, "apps").GetRunningPods(t.Context(), "shop", "api", "dev")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestGetRunningPods_LogicalEnvironmentIncludesInstances(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		podInfoPod("logical", "apps", "shop", "api", "review", helm.GenerateReleaseName("shop", "review"), []string{"app"}),
		podInfoPod("pr-123", "apps", "shop", "api", "review", helm.GenerateReleaseName("shop", "review/pr-123"), []string{"app"}),
		podInfoPod("web", "apps", "shop", "web", "review", helm.GenerateReleaseName("shop", "review"), []string{"web"}),
	)
	got, err := NewClient(cs, "apps").GetRunningPods(t.Context(), "shop", "api", "review")
	require.NoError(t, err)
	assert.ElementsMatch(t, []PodInfo{
		{Name: "logical", Namespace: "apps", Containers: []string{"app"}, Status: string(corev1.PodRunning)},
		{Name: "pr-123", Namespace: "apps", Containers: []string{"app"}, Status: string(corev1.PodRunning)},
	}, got)
}

func TestGetRunningPods_WildcardInstanceExcludesSiblings(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		podInfoPod("logical", "apps", "shop", "api", "review", helm.GenerateReleaseName("shop", "review"), []string{"app"}),
		podInfoPod("pr-123", "apps", "shop", "api", "review", helm.GenerateReleaseName("shop", "review/pr-123"), []string{"app"}),
		podInfoPod("pr-456", "apps", "shop", "api", "review", helm.GenerateReleaseName("shop", "review/pr-456"), []string{"app"}),
	)
	got, err := NewClient(cs, "apps").GetRunningPods(t.Context(), "shop", "api", "review/pr-123")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pr-123", got[0].Name)
}

func TestGetRunningPods_InvalidEnvironment(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	listed := false
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		listed = true
		return true, nil, errors.New("should not list")
	})
	_, err := NewClient(cs, "apps").GetRunningPods(t.Context(), "shop", "api", "review/PR-123")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to build selector")
	assert.ErrorContains(t, err, "lowercase DNS label")
	assert.False(t, listed)
}

func TestGetRunningPods_ListError(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver timeout")
	})
	_, err := NewClient(cs, "apps").GetRunningPods(t.Context(), "shop", "api", "dev")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to list pods")
	assert.ErrorContains(t, err, "apiserver timeout")
}

func TestGetPodInfo(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		podInfoPod("api-a", "apps", "shop", "api", "dev", "shop-dev", []string{"app", "sidecar"}),
	)
	client := NewClient(cs, "apps")

	got, err := client.GetPodInfo(t.Context(), "api-a")
	require.NoError(t, err)
	assert.Equal(t, &PodInfo{
		Name:       "api-a",
		Namespace:  "apps",
		Containers: []string{"app", "sidecar"},
		Status:     string(corev1.PodRunning),
	}, got)

	_, err = client.GetPodInfo(t.Context(), "missing")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to get pod missing")
}

func TestGetPodStatus_CountsReadyRunningPods(t *testing.T) {
	t.Parallel()

	release := "shop-dev"
	cs := fake.NewSimpleClientset(
		statusPod("ready", "apps", release, corev1.PodRunning, []corev1.ContainerStatus{{Name: "app", Ready: true}}),
		statusPod("not-ready", "apps", release, corev1.PodRunning, []corev1.ContainerStatus{{Name: "app", Ready: false}}),
		statusPod("pending", "apps", release, corev1.PodPending, []corev1.ContainerStatus{{Name: "app", Ready: true}}),
		statusPod("succeeded", "apps", release, corev1.PodSucceeded, []corev1.ContainerStatus{{Name: "app", Ready: true}}),
		// A running pod with no container statuses counts as ready: the check
		// returns true when every reported status is ready, including none.
		statusPod("no-status", "apps", release, corev1.PodRunning, nil),
		statusPod("other-release", "apps", "shop-prod", corev1.PodRunning, []corev1.ContainerStatus{{Name: "app", Ready: true}}),
		statusPod("other-ns", "other", release, corev1.PodRunning, []corev1.ContainerStatus{{Name: "app", Ready: true}}),
	)

	total, ready, summary, err := NewClient(cs, "apps").GetPodStatus(t.Context(), release)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Equal(t, 2, ready)
	assert.Equal(t, "2/5", summary)
}

func TestGetPodStatus_ListError(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver timeout")
	})
	total, ready, summary, err := NewClient(cs, "apps").GetPodStatus(t.Context(), "shop-dev")
	require.Error(t, err)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, ready)
	assert.Equal(t, "0/0", summary)
	assert.ErrorContains(t, err, "list pods for release shop-dev")
	assert.ErrorContains(t, err, "apiserver timeout")
}

func TestGetPodStatus_InvalidReleaseName(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	listed := false
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		listed = true
		return true, nil, errors.New("should not list")
	})
	total, ready, summary, err := NewClient(cs, "apps").GetPodStatus(t.Context(), "bad value!")
	require.Error(t, err)
	assert.Equal(t, 0, total)
	assert.Equal(t, 0, ready)
	assert.Equal(t, "0/0", summary)
	assert.ErrorContains(t, err, "build selector for release bad value!")
	assert.False(t, listed)
}

func TestValidatePodExists(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		podInfoPod("api-a", "apps", "shop", "api", "dev", "shop-dev", []string{"app"}),
	)
	client := NewClient(cs, "apps")
	require.NoError(t, client.ValidatePodExists(t.Context(), "api-a"))

	err := client.ValidatePodExists(t.Context(), "missing")
	require.Error(t, err)
	assert.ErrorContains(t, err, "pod missing not found")
}

func podInfoPod(name, namespace, project, component, environment, instance string, containers []string) *corev1.Pod {
	specContainers := make([]corev1.Container, 0, len(containers))
	for _, container := range containers {
		specContainers = append(specContainers, corev1.Container{Name: container})
	}
	return &corev1.Pod{
		Name:      name,
		Namespace: namespace,
		Labels: map[string]string{
			ProjectLabel:     project,
			ComponentLabel:   component,
			EnvironmentLabel: environment,
			InstanceLabel:    instance,
		},
		Spec:   corev1.PodSpec{Containers: specContainers},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func statusPod(name, namespace, release string, phase corev1.PodPhase, statuses []corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		Name:      name,
		Namespace: namespace,
		Labels:    map[string]string{InstanceLabel: release},
		Status:    corev1.PodStatus{Phase: phase, ContainerStatuses: statuses},
	}
}
