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

//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/testing/e2e"

	inttest "deployah.dev/deployah/internal/testing"
)

const (
	wildcardProject = "revdemo"
	wildcardEnvA    = "review/pr-123"
	wildcardEnvB    = "review/pr-456"
)

// TestWildcardInstances deploys two review/* siblings and checks identity,
// list/status isolation, independent runtime env, a manual run, and delete.
func runWildcardInstances(t *testing.T, env *e2eEnv) {
	t.Helper()
	src := filepath.Join(env.scenariosDir, "wildcard-review")
	require.DirExists(t, src)

	dir, err := inttest.NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	ns := fixtureNamespace("wildcard-review")
	claim, err := env.claimNamespace(t.Context(), ns)
	require.NoError(t, err)
	t.Cleanup(func() {
		if cleanupErr := env.cleanupClaim(t, dir, claim, []releaseTarget{
			{Project: wildcardProject, Env: wildcardEnvA},
			{Project: wildcardProject, Env: wildcardEnvB},
		}); cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
		}
	})

	releaseA := helm.GenerateReleaseName(wildcardProject, wildcardEnvA)
	releaseB := helm.GenerateReleaseName(wildcardProject, wildcardEnvB)
	require.NotEqual(t, releaseA, releaseB)

	env.deployWildcard(t, dir, ns, wildcardEnvA)
	env.waitWildcardReady(t, ns, wildcardEnvA, releaseA, "pr-123")

	env.deployWildcard(t, dir, ns, wildcardEnvB)
	env.waitWildcardReady(t, ns, wildcardEnvB, releaseB, "pr-456")

	env.assertListJSON(t, dir, ns, "review", []string{wildcardEnvA, wildcardEnvB})
	env.assertListJSON(t, dir, ns, wildcardEnvA, []string{wildcardEnvA})
	env.assertStatusJSON(t, dir, ns, wildcardEnvA, releaseA)

	env.assertRunIsolated(t, dir, ns, releaseA)

	runIn(t, dir, "delete", wildcardProject, wildcardEnvA,
		"--yes", "--wait", "--allow-missing-platform",
		"--context", kindContext, "--namespace", ns)

	env.waitWild(t, ns, []wildCheck{
		{sel: e2e.ResourceSelect{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Labels:     map[string]string{spec.LabelInstance: releaseA},
		}, empty: true},
		wildcardDeployment(wildcardEnvB, releaseB),
		wildcardEnvConfigMap(releaseB, "pr-456"),
	}, 3*time.Minute)

	env.assertStatusJSON(t, dir, ns, wildcardEnvB, releaseB)
}

func (env *e2eEnv) deployWildcard(t *testing.T, dir, ns, environment string) {
	t.Helper()
	runIn(t, dir, "deploy", environment, "--context", kindContext, "--namespace", ns)
}

func (env *e2eEnv) waitWildcardReady(t *testing.T, ns, original, release, slot string) {
	t.Helper()
	env.waitWild(t, ns, []wildCheck{
		wildcardDeployment(original, release),
		wildcardEnvConfigMap(release, slot),
	}, 3*time.Minute)
}

type wildCheck struct {
	sel    e2e.ResourceSelect
	expect map[string]any
	empty  bool
}

func (env *e2eEnv) waitWild(t *testing.T, ns string, checks []wildCheck, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last []string
	for {
		last = nil
		for i, check := range checks {
			objects, err := env.observe(t.Context(), check.sel, ns)
			if err != nil {
				last = append(last, fmt.Sprintf("check[%d]: %v", i, err))
				continue
			}
			if check.empty {
				if len(objects) != 0 {
					last = append(last, fmt.Sprintf("check[%d]: want 0, got %d", i, len(objects)))
				}
				continue
			}
			if len(objects) == 0 {
				last = append(last, fmt.Sprintf("check[%d]: no resources selected", i))
				continue
			}
			for _, obj := range objects {
				for _, diff := range inttest.DiffSubset("$", check.expect, obj) {
					last = append(last, fmt.Sprintf("check[%d]: %s", i, diff))
				}
			}
		}
		if len(last) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("wildcard resources not ready:\n%s", strings.Join(last, "\n"))
		}
		time.Sleep(resourcePollInterval)
	}
}

func wildcardDeployment(original, release string) wildCheck {
	return wildCheck{
		sel: e2e.ResourceSelect{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       release + "-web",
			Labels: map[string]string{
				spec.LabelProject:     wildcardProject,
				spec.LabelEnvironment: "review",
				spec.LabelInstance:    release,
			},
		},
		expect: map[string]any{
			"metadata": map[string]any{
				"annotations": map[string]any{
					spec.AnnotationEnvironmentInstance: original,
				},
			},
			"status": map[string]any{"readyReplicas": 1},
		},
	}
}

func wildcardEnvConfigMap(release, slot string) wildCheck {
	return wildCheck{
		sel: e2e.ResourceSelect{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       release + "-web-env",
			Labels: map[string]string{
				spec.LabelEnvironment: "review",
				spec.LabelInstance:    release,
			},
		},
		expect: map[string]any{
			"data": map[string]any{"PR_SLOT": slot},
		},
	}
}

func (env *e2eEnv) assertListJSON(t *testing.T, dir, ns, environment string, wantInstances []string) {
	t.Helper()
	stdout, _ := runIn(t, dir, "list",
		"--project", wildcardProject,
		"--environment", environment,
		"--output", "json",
		"--context", kindContext,
		"--namespace", ns)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &rows), stdout)
	require.Len(t, rows, len(wantInstances), stdout)
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		inst, ok := row["instance"].(string)
		require.True(t, ok, "instance: %v", row["instance"])
		got = append(got, inst)
		rel, ok := row["release"].(string)
		require.True(t, ok && rel != "", "release: %v", row["release"])
		assert.Equal(t, "review", row["environment"])
	}
	assert.ElementsMatch(t, wantInstances, got)
}

func (env *e2eEnv) assertStatusJSON(t *testing.T, dir, ns, environment, release string) {
	t.Helper()
	stdout, _ := runIn(t, dir, "status", wildcardProject,
		"--environment", environment,
		"--output", "json",
		"--context", kindContext,
		"--namespace", ns)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &rows), stdout)
	require.Len(t, rows, 1, stdout)
	assert.Equal(t, environment, rows[0]["instance"])
	assert.Equal(t, release, rows[0]["release"])
	sibling := wildcardEnvB
	if environment == wildcardEnvB {
		sibling = wildcardEnvA
	}
	assert.NotContains(t, stdout, sibling)
}

func (env *e2eEnv) assertRunIsolated(t *testing.T, dir, ns, releaseA string) {
	t.Helper()
	runIn(t, dir, "run", "ping", wildcardEnvA,
		"--context", kindContext, "--yes", "--namespace", ns)

	res, err := env.newResources(ns)
	require.NoError(t, err)
	selector := labels.Set{
		spec.LabelProject:     wildcardProject,
		spec.LabelComponent:   "ping",
		spec.LabelEnvironment: "review",
		spec.LabelInstance:    releaseA,
	}.String()

	jobs := &unstructured.UnstructuredList{}
	jobs.SetGroupVersionKind(schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "JobList"})
	require.NoError(t, res.List(t.Context(), jobs, resources.WithLabelSelector(selector)))
	require.Len(t, jobs.Items, 1)
	job := jobs.Items[0]
	assert.Equal(t, wildcardEnvA, job.GetAnnotations()[spec.AnnotationEnvironmentInstance])
	assert.Equal(t, "review", job.GetLabels()[spec.LabelEnvironment])
	assert.NotEqual(t, helm.GenerateReleaseName(wildcardProject, wildcardEnvB), job.GetLabels()[spec.LabelInstance])

	cms := &unstructured.UnstructuredList{}
	cms.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMapList"})
	require.NoError(t, res.List(t.Context(), cms, resources.WithLabelSelector(
		labels.Set{spec.LabelInstance: releaseA}.String())))

	releaseCM := releaseA + "-web-env"
	var runCM string
	var sawRelease bool
	for i := range cms.Items {
		cm := cms.Items[i]
		switch cm.GetName() {
		case releaseCM:
			sawRelease = true
			data, found, dataErr := unstructured.NestedStringMap(cm.Object, "data")
			require.NoError(t, dataErr)
			require.True(t, found)
			assert.Equal(t, "pr-123", data["PR_SLOT"])
			assert.Empty(t, cm.GetOwnerReferences())
		default:
			if len(cm.GetOwnerReferences()) == 1 && cm.GetOwnerReferences()[0].Kind == "Job" {
				runCM = cm.GetName()
				assert.Equal(t, wildcardEnvA, cm.GetAnnotations()[spec.AnnotationEnvironmentInstance])
			}
		}
	}
	require.True(t, sawRelease, "release ConfigMap %s missing", releaseCM)
	require.NotEmpty(t, runCM, "run ConfigMap owned by the Job is missing")
	assert.NotEqual(t, releaseCM, runCM)
	assert.False(t, strings.Contains(runCM, releaseCM))

	envFrom, found, err := unstructured.NestedSlice(job.Object, "spec", "template", "spec", "containers")
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, envFrom)
	container, ok := envFrom[0].(map[string]any)
	require.True(t, ok)
	from, found, err := unstructured.NestedSlice(container, "envFrom")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, from, 1)
	src, ok := from[0].(map[string]any)
	require.True(t, ok)
	name, found, err := unstructured.NestedString(src, "configMapRef", "name")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, runCM, name)
}
