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
	"context"
	"encoding/json"
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

	inttest "deployah.dev/deployah/internal/testing"
)

const (
	wildcardProject = "revdemo"
	wildcardEnvA    = "review/pr-123"
	wildcardEnvB    = "review/pr-456"
)

// TestWildcardInstances deploys two review/* siblings and checks identity,
// list/status isolation, independent runtime env, a manual run, and delete.
func (s *E2ESuite) TestWildcardInstances() {
	t := s.T()
	src := filepath.Join(s.scenariosDir, "wildcard-review")
	require.DirExists(t, src)

	dir := t.TempDir()
	copyTree(t, src, dir)

	ns := fixtureNamespace("wildcard-review")
	s.createNamespace(t, ns)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
		defer cancel()
		for _, env := range []string{wildcardEnvA, wildcardEnvB} {
			if _, _, delErr := runInErrContext(t, cleanupCtx, dir, "delete", wildcardProject, env,
				"--yes", "--wait", "--allow-missing-platform",
				"--context", kindContext, "--namespace", ns); delErr != nil {
				t.Logf("cleanup delete %s failed (non-fatal): %v", env, delErr)
			}
		}
		s.deleteNamespace(t, ns)
	})

	releaseA := helm.GenerateReleaseName(wildcardProject, wildcardEnvA)
	releaseB := helm.GenerateReleaseName(wildcardProject, wildcardEnvB)
	require.NotEqual(t, releaseA, releaseB)

	s.deployWildcard(t, dir, ns, wildcardEnvA)
	s.waitWildcardReady(t, ns, wildcardEnvA, releaseA, "pr-123")

	s.deployWildcard(t, dir, ns, wildcardEnvB)
	s.waitWildcardReady(t, ns, wildcardEnvB, releaseB, "pr-456")

	s.assertListJSON(t, dir, ns, "review", []string{wildcardEnvA, wildcardEnvB})
	s.assertListJSON(t, dir, ns, wildcardEnvA, []string{wildcardEnvA})
	s.assertStatusJSON(t, dir, ns, wildcardEnvA, releaseA)

	s.assertRunIsolated(t, dir, ns, releaseA)

	runIn(t, dir, "delete", wildcardProject, wildcardEnvA,
		"--yes", "--wait", "--allow-missing-platform",
		"--context", kindContext, "--namespace", ns)

	zero := 0
	s.waitForResources(t, ns, []inttest.ResourceAssertion{
		{
			MinCount: &zero,
			Match: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]any{
					"labels": map[string]any{
						spec.LabelInstance: releaseA,
					},
				},
			},
		},
		wildcardDeployment(wildcardEnvB, releaseB),
		wildcardEnvConfigMap(releaseB, "pr-456"),
	}, 3*time.Minute)

	s.assertStatusJSON(t, dir, ns, wildcardEnvB, releaseB)
}

func (s *E2ESuite) deployWildcard(t *testing.T, dir, ns, env string) {
	t.Helper()
	runIn(t, dir, "deploy", env, "--context", kindContext, "--yes", "--namespace", ns)
}

func (s *E2ESuite) waitWildcardReady(t *testing.T, ns, original, release, slot string) {
	t.Helper()
	s.waitForResources(t, ns, []inttest.ResourceAssertion{
		wildcardDeployment(original, release),
		wildcardEnvConfigMap(release, slot),
	}, 3*time.Minute)
}

func wildcardDeployment(original, release string) inttest.ResourceAssertion {
	return inttest.ResourceAssertion{
		Match: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name": release + "-web",
				"labels": map[string]any{
					spec.LabelProject:     wildcardProject,
					spec.LabelEnvironment: "review",
					spec.LabelInstance:    release,
				},
				"annotations": map[string]any{
					spec.AnnotationEnvironmentInstance: original,
				},
			},
			"status": map[string]any{"readyReplicas": 1},
		},
	}
}

func wildcardEnvConfigMap(release, slot string) inttest.ResourceAssertion {
	return inttest.ResourceAssertion{
		Match: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name": release + "-web-env",
				"labels": map[string]any{
					spec.LabelEnvironment: "review",
					spec.LabelInstance:    release,
				},
			},
			"data": map[string]any{"PR_SLOT": slot},
		},
	}
}

func (s *E2ESuite) assertListJSON(t *testing.T, dir, ns, env string, wantInstances []string) {
	t.Helper()
	stdout, _ := runIn(t, dir, "list",
		"--project", wildcardProject,
		"--environment", env,
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

func (s *E2ESuite) assertStatusJSON(t *testing.T, dir, ns, env, release string) {
	t.Helper()
	stdout, _ := runIn(t, dir, "status", wildcardProject,
		"--environment", env,
		"--output", "json",
		"--context", kindContext,
		"--namespace", ns)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &rows), stdout)
	require.Len(t, rows, 1, stdout)
	assert.Equal(t, env, rows[0]["instance"])
	assert.Equal(t, release, rows[0]["release"])
	sibling := wildcardEnvB
	if env == wildcardEnvB {
		sibling = wildcardEnvA
	}
	assert.NotContains(t, stdout, sibling)
}

func (s *E2ESuite) assertRunIsolated(t *testing.T, dir, ns, releaseA string) {
	t.Helper()
	runIn(t, dir, "run", "ping", wildcardEnvA,
		"--context", kindContext, "--yes", "--namespace", ns)

	res, err := s.newResources(ns)
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
