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
// See the License for the specific language governing the License.

package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
)

func TestGetAvailableEnvironments_DistinguishesWildcardInstances(t *testing.T) {
	t.Parallel()

	pod := func(name, component, env, instance, original string) *corev1.Pod {
		return &corev1.Pod{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				ProjectLabel:     "shop",
				ComponentLabel:   component,
				EnvironmentLabel: env,
				InstanceLabel:    instance,
			},
			Annotations: map[string]string{
				spec.AnnotationEnvironmentInstance: original,
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}

	cs := fake.NewSimpleClientset(
		pod("logical", "api", "review", helm.GenerateReleaseName("shop", "review"), "review"),
		pod("pr-123", "api", "review", helm.GenerateReleaseName("shop", "review/pr-123"), "review/pr-123"),
		pod("pr-456", "api", "review", helm.GenerateReleaseName("shop", "review/pr-456"), "review/pr-456"),
		pod("web-pr", "web", "review", helm.GenerateReleaseName("shop", "review/pr-789"), "review/pr-789"),
	)
	client := NewClient(cs, "default")
	got, err := client.GetAvailableEnvironments(t.Context(), "shop", "api")
	require.NoError(t, err)
	assert.Equal(t, []string{"review", "review/pr-123", "review/pr-456"}, got)
}
