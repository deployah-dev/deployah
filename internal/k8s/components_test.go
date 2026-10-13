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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
	k8stesting "k8s.io/client-go/testing"
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
	var field string
	cs.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		list, ok := action.(k8stesting.ListAction)
		if ok {
			field = list.GetListRestrictions().Fields.String()
		}
		return false, nil, nil
	})
	client := NewClient(cs, "default")
	got, err := client.GetAvailableEnvironments(t.Context(), "shop", "api")
	require.NoError(t, err)
	assert.Equal(t, []string{"review", "review/pr-123", "review/pr-456"}, got)
	// The fake client does not apply field selectors. Running phase is requested.
	assert.Equal(t, "status.phase=Running", field)
}

func TestGetAvailableComponents(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(
		componentPod("api-1", "apps", "shop", "api"),
		componentPod("api-2", "apps", "shop", "api"),
		componentPod("web", "apps", "shop", "web"),
		componentPod("unlabeled", "apps", "shop", ""),
		componentPod("other-project", "apps", "billing", "api"),
		componentPod("other-ns", "other", "shop", "worker"),
	)
	var field string
	cs.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		list, ok := action.(k8stesting.ListAction)
		if ok {
			field = list.GetListRestrictions().Fields.String()
		}
		return false, nil, nil
	})

	got, err := NewClient(cs, "apps").GetAvailableComponents(t.Context(), "shop")
	require.NoError(t, err)
	// Names come from a map, so the order is not part of the contract.
	assert.ElementsMatch(t, []string{"api", "web"}, got)
	assert.Equal(t, "status.phase=Running", field)
}

func TestGetAvailableComponents_InvalidProject(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	listed := false
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		listed = true
		return true, nil, errors.New("should not list")
	})
	_, err := NewClient(cs, "apps").GetAvailableComponents(t.Context(), "bad value!")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to build project selector")
	assert.False(t, listed)
}

func TestGetAvailableComponents_ListError(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver timeout")
	})
	_, err := NewClient(cs, "apps").GetAvailableComponents(t.Context(), "shop")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to list pods")
	assert.ErrorContains(t, err, "apiserver timeout")
}

func TestValidateComponentExists(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset(componentPod("api-1", "apps", "shop", "api"))
	client := NewClient(cs, "apps")
	require.NoError(t, client.ValidateComponentExists(t.Context(), "shop", "api"))

	err := client.ValidateComponentExists(t.Context(), "shop", "web")
	require.Error(t, err)
	assert.ErrorContains(t, err, "component 'web' not found or has no running pods in project 'shop'")
}

func TestValidateComponentExists_ListError(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver timeout")
	})
	err := NewClient(cs, "apps").ValidateComponentExists(t.Context(), "shop", "api")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to validate component 'api'")
	assert.ErrorContains(t, err, "apiserver timeout")
}

func TestGetAvailableEnvironments_SkipsBlankAnnotations(t *testing.T) {
	t.Parallel()

	pod := func(name, component string, annotations map[string]string) *corev1.Pod {
		return &corev1.Pod{
			Name:      name,
			Namespace: "apps",
			Labels: map[string]string{
				ProjectLabel:   "shop",
				ComponentLabel: component,
			},
			Annotations: annotations,
			Status:      corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	env := func(original string) map[string]string {
		return map[string]string{spec.AnnotationEnvironmentInstance: original}
	}
	cs := fake.NewSimpleClientset(
		pod("missing", "api", nil),
		pod("blank", "api", env("   ")),
		pod("dev-a", "api", env("dev")),
		pod("dev-b", "api", env("dev")),
		pod("staging", "api", env("staging")),
		pod("other-component", "web", env("prod")),
	)
	got, err := NewClient(cs, "apps").GetAvailableEnvironments(t.Context(), "shop", "api")
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "staging"}, got)
}

func TestGetAvailableEnvironments_ListError(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver timeout")
	})
	_, err := NewClient(cs, "apps").GetAvailableEnvironments(t.Context(), "shop", "api")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to list pods")
	assert.ErrorContains(t, err, "apiserver timeout")
}

func TestGetAvailableEnvironments_InvalidComponent(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	listed := false
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		listed = true
		return true, nil, errors.New("should not list")
	})
	_, err := NewClient(cs, "apps").GetAvailableEnvironments(t.Context(), "shop", "bad value!")
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to build component selector")
	assert.False(t, listed)
}

// componentPod builds a running pod. An empty component omits the label.
func componentPod(name, namespace, project, component string) *corev1.Pod {
	labels := map[string]string{ProjectLabel: project}
	if component != "" {
		labels[ComponentLabel] = component
	}
	return &corev1.Pod{
		Name:      name,
		Namespace: namespace,
		Labels:    labels,
		Status:    corev1.PodStatus{Phase: corev1.PodRunning},
	}
}
