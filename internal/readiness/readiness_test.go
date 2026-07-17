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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func pod(name, component string, ready bool) *corev1.Pod {
	phase := corev1.PodRunning
	if !ready {
		phase = corev1.PodPending
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/instance": "web-production",
				"deployah.dev/component":     component,
			},
		},
		Status: corev1.PodStatus{
			Phase:             phase,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: ready}},
		},
	}
}

// TestPoll verifies component grouping and instance-label filtering.
func TestPoll(t *testing.T) {
	t.Parallel()

	otherRelease := pod("other-1", "api", true)
	otherRelease.Labels["app.kubernetes.io/instance"] = "other-release"

	tests := []struct {
		name    string
		pods    []runtime.Object
		want    map[string]ComponentStatus
		wantLen int
	}{
		{
			name: "groups by component label",
			pods: []runtime.Object{
				pod("api-1", "api", true),
				pod("api-2", "api", false),
				pod("worker-1", "worker", true),
			},
			want: map[string]ComponentStatus{
				"api":    {Name: "api", ReadyPods: 1, TotalPods: 2},
				"worker": {Name: "worker", ReadyPods: 1, TotalPods: 1},
			},
			wantLen: 2,
		},
		{
			name:    "ignores pods from other releases",
			pods:    []runtime.Object{otherRelease},
			want:    map[string]ComponentStatus{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := fake.NewSimpleClientset(tt.pods...)

			statuses, err := Poll(t.Context(), client, "default", "web-production")
			require.NoError(t, err)
			require.Len(t, statuses, tt.wantLen)

			byName := make(map[string]ComponentStatus, len(statuses))
			for _, s := range statuses {
				byName[s.Name] = s
			}
			for name, want := range tt.want {
				got, ok := byName[name]
				require.True(t, ok, "missing component %s", name)
				assert.Equal(t, want, got)
			}
		})
	}
}

// TestIsPodReady covers running+ready versus not-running / not-ready pods.
func TestIsPodReady(t *testing.T) {
	t.Parallel()

	notRunning := pod("p", "api", true)
	notRunning.Status.Phase = corev1.PodSucceeded

	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{name: "running and ready", pod: pod("p", "api", true), want: true},
		{name: "running but not ready", pod: pod("p", "api", false), want: false},
		{name: "not running", pod: notRunning, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsPodReady(tt.pod))
		})
	}
}

// TestAllReady verifies the all-or-nothing readiness check.
func TestAllReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []ComponentStatus
		want     bool
	}{
		{name: "nil is not ready", statuses: nil, want: false},
		{name: "partial ready", statuses: []ComponentStatus{{Name: "api", ReadyPods: 1, TotalPods: 2}}, want: false},
		{
			name: "all ready",
			statuses: []ComponentStatus{
				{Name: "api", ReadyPods: 2, TotalPods: 2},
				{Name: "worker", ReadyPods: 1, TotalPods: 1},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, AllReady(tt.statuses))
		})
	}
}

// TestSummary verifies the comma-separated "name: ready/total" formatting.
func TestSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []ComponentStatus
		want     string
	}{
		{name: "nil", statuses: nil, want: ""},
		{
			name: "two components",
			statuses: []ComponentStatus{
				{Name: "api", ReadyPods: 1, TotalPods: 2},
				{Name: "worker", ReadyPods: 1, TotalPods: 1},
			},
			want: "api: 1/2, worker: 1/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Summary(tt.statuses))
		})
	}
}
