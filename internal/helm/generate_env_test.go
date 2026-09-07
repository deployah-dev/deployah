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

package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestRenderDeployment_EnvConfigMapAndOverlappingEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("LOG_LEVEL=info\nREGION=eu\n"), 0o600))

	m := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]spec.Environment{
			"dev": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Port:  8080,
				Env: spec.StringMap{
					"LOG_LEVEL": "debug",
					"NOTE":      "{{ .Release.Name }}",
				},
			},
		},
	}
	resolved := resolveChart(t, m, "dev")
	assert.Equal(t, map[string]string{"LOG_LEVEL": "info", "REGION": "eu"}, resolved.Components["api"].Runtime.FileValues)
	assert.Equal(t, map[string]string{"LOG_LEVEL": "debug", "NOTE": "{{ .Release.Name }}"}, resolved.Components["api"].Runtime.ExplicitValues)

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), m, "dev", resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}

	cm := findRenderedConfigMap(t, result.Manifest, "-api-env")
	assert.Equal(t, map[string]string{"LOG_LEVEL": "info", "REGION": "eu"}, cm.Data)

	dep := findRenderedDeployment(t, result.Manifest, "-api")
	require.NotEmpty(t, dep.Spec.Template.Annotations["checksum/env"])
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []corev1.EnvVar{
		{Name: "LOG_LEVEL", Value: "debug"},
		{Name: "NOTE", Value: "{{ .Release.Name }}"},
	}, dep.Spec.Template.Spec.Containers[0].Env)
	require.Len(t, dep.Spec.Template.Spec.Containers[0].EnvFrom, 1)
	require.NotNil(t, dep.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef)
	assert.Equal(t, cm.Name, dep.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef.Name)
}

func TestRenderWorkloads_ExplicitEnvStaysLiteral(t *testing.T) {
	t.Parallel()

	const message = "{{ .Release.Name }}"
	want := []corev1.EnvVar{{Name: "MESSAGE", Value: message}}

	tests := []struct {
		name       string
		kind       string
		nameSuffix string
	}{
		{name: "deployment", kind: "Deployment", nameSuffix: "-api"},
		{name: "statefulset", kind: "StatefulSet", nameSuffix: "-api"},
		{name: "cronjob", kind: "CronJob", nameSuffix: "-cleanup"},
		{name: "job", kind: "Job", nameSuffix: "-migrate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := envWorkloadSpec(t.TempDir(), spec.StringMap{"MESSAGE": message}, tt.kind)
			result := renderEnvManifest(t, m, "dev")
			assert.Equal(t, want, renderedContainerEnv(t, result, tt.kind, tt.nameSuffix))
		})
	}
}

func TestMapSpecToChartValues_RuntimeEnvOmitsConfigMapName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("REGION=eu\n"), 0o600))

	m := envServiceSpec(dir, spec.StringMap{"LOG_LEVEL": "debug"})
	resolved := resolveChart(t, m, "dev")
	vals, err := MapSpecToChartValues(m, "dev", resolved)
	require.NoError(t, err)

	api := mustNestedMap(t, vals, "api")
	assert.Equal(t, map[string]string{"REGION": "eu"}, api["envFileValues"])
	assert.Equal(t, map[string]string{"LOG_LEVEL": "debug"}, api["envVars"])
	_, hasName := api["envVarsConfigMap"]
	assert.False(t, hasName)
}

func TestRenderDeployment_NoFileValuesOmitsEnvConfigMap(t *testing.T) {
	t.Parallel()

	m := envServiceSpec(t.TempDir(), spec.StringMap{"LOG_LEVEL": "debug"})
	result := renderEnvManifest(t, m, "dev")

	assertNoConfigMapSuffix(t, result.Manifest, "-api-env")
	dep := findRenderedDeployment(t, result.Manifest, "-api")
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []corev1.EnvVar{{Name: "LOG_LEVEL", Value: "debug"}}, dep.Spec.Template.Spec.Containers[0].Env)
	assert.Empty(t, dep.Spec.Template.Spec.Containers[0].EnvFrom)
	assert.Empty(t, dep.Spec.Template.Annotations["checksum/env"])
}

func TestRenderWorkloads_EnvFromMatchesConfigMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		kind       string
		nameSuffix string
		cmSuffix   string
	}{
		{name: "deployment", kind: "Deployment", nameSuffix: "-api", cmSuffix: "-api-env"},
		{name: "statefulset", kind: "StatefulSet", nameSuffix: "-api", cmSuffix: "-api-env"},
		{name: "cronjob", kind: "CronJob", nameSuffix: "-cleanup", cmSuffix: "-cleanup-env"},
		{name: "job", kind: "Job", nameSuffix: "-migrate", cmSuffix: "-migrate-env"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("REGION=eu\n"), 0o600))
			m := envWorkloadSpec(dir, nil, tt.kind)
			result := renderEnvManifest(t, m, "dev")
			cm := findEnvConfigMap(t, result, tt.kind, tt.cmSuffix)
			assert.Equal(t, cm.Name, renderedEnvFromConfigMap(t, result, tt.kind, tt.nameSuffix))
		})
	}
}

func TestRenderDeployment_LongFullnameEnvFromMatchesConfigMap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("REGION=eu\n"), 0o600))

	const component = "component-with-a-long-name-xx"
	m := envServiceSpec(dir, nil)
	m.Project = strings.Repeat("p", 40)
	m.Components[component] = m.Components["api"]
	delete(m.Components, "api")

	result := renderEnvManifest(t, m, "dev")
	cm := findRenderedConfigMap(t, result.Manifest, "-env")
	dep := findOnlyDeployment(t, result.Manifest)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	require.Len(t, dep.Spec.Template.Spec.Containers[0].EnvFrom, 1)
	require.NotNil(t, dep.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef)
	assert.Equal(t, cm.Name, dep.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef.Name)
	assert.LessOrEqual(t, len(cm.Name), 63)
	assert.True(t, strings.HasSuffix(cm.Name, "-env"))
	require.Greater(t, len(dep.Name)+len("-env"), 63)
	assert.Regexp(t, `-[0-9a-f]{4}-env$`, cm.Name)
}

func TestHelmHook_EnvConfigMapWeightAndDeletePolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("REGION=eu\n"), 0o600))

	m := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]spec.Environment{
			"dev": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Port:  8080,
			},
		},
		Tasks: map[string]spec.Task{
			"migrate": {
				From:    "api",
				On:      spec.TaskOnPreDeploy,
				Command: []string{"migrate"},
			},
			"seed": {
				From:    "api",
				On:      spec.TaskOnPreDeploy,
				After:   []string{"migrate"},
				Command: []string{"seed"},
			},
		},
	}
	resolved := resolveChart(t, m, "dev")
	assert.Equal(t, 0, resolved.Tasks["migrate"].HookWeight)
	assert.Equal(t, 1, resolved.Tasks["seed"].HookWeight)

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), m, "dev", resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}

	migrateCM := hookBySuffix(t, result, "ConfigMap", "-migrate-env")
	seedCM := hookBySuffix(t, result, "ConfigMap", "-seed-env")
	migrateJob := hookBySuffix(t, result, "Job", "-migrate")
	seedJob := hookBySuffix(t, result, "Job", "-seed")

	assert.Equal(t, migrateJob.Weight-1, migrateCM.Weight)
	assert.Equal(t, seedJob.Weight-1, seedCM.Weight)
	assert.Less(t, seedCM.Weight, seedJob.Weight)
	assert.Contains(t, seedCM.Manifest, `helm.sh/hook: "pre-install,pre-upgrade"`)
	assert.Contains(t, seedCM.Manifest, "before-hook-creation,hook-succeeded")
	assert.NotContains(t, seedCM.Manifest, "hook-failed")
	assert.Contains(t, seedJob.Manifest, "before-hook-creation,hook-succeeded")

	var migrateJobObj batchv1.Job
	require.NoError(t, yaml.Unmarshal([]byte(migrateJob.Manifest), &migrateJobObj))
	require.Len(t, migrateJobObj.Spec.Template.Spec.Containers, 1)
	require.Len(t, migrateJobObj.Spec.Template.Spec.Containers[0].EnvFrom, 1)
	require.NotNil(t, migrateJobObj.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef)
	assert.Equal(t, migrateCM.Name, migrateJobObj.Spec.Template.Spec.Containers[0].EnvFrom[0].ConfigMapRef.Name)
}

func TestHelmHook_EnvConfigMapWeightFollowsJob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("REGION=eu\n"), 0o600))

	m := envWorkloadSpec(dir, nil, "Job")
	resolved := resolveChart(t, m, "dev")
	rt := resolved.Tasks["migrate"]
	rt.HookWeight = 4
	resolved.Tasks["migrate"] = rt

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), m, "dev", resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}

	cm := hookBySuffix(t, result, "ConfigMap", "-migrate-env")
	job := hookBySuffix(t, result, "Job", "-migrate")
	assert.Equal(t, 3, cm.Weight)
	assert.Equal(t, 4, job.Weight)
	assert.Contains(t, cm.Manifest, `helm.sh/hook-weight: "3"`)
}

func TestHelmHook_NoFileValuesOmitsEnvConfigMap(t *testing.T) {
	t.Parallel()

	m := envWorkloadSpec(t.TempDir(), spec.StringMap{"LOG_LEVEL": "debug"}, "Job")
	result := renderEnvManifest(t, m, "dev")

	assertNoHookKindSuffix(t, result, "ConfigMap", "-migrate-env")
	hook := hookBySuffix(t, result, "Job", "-migrate")
	var job batchv1.Job
	require.NoError(t, yaml.Unmarshal([]byte(hook.Manifest), &job))
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []corev1.EnvVar{{Name: "LOG_LEVEL", Value: "debug"}}, job.Spec.Template.Spec.Containers[0].Env)
	assert.Empty(t, job.Spec.Template.Spec.Containers[0].EnvFrom)
}

func envWorkloadSpec(dir string, env spec.StringMap, kind string) *spec.Spec {
	m := envServiceSpec(dir, env)
	switch kind {
	case "StatefulSet":
		comp := m.Components["api"]
		comp.Kind = spec.ComponentKindStateful
		m.Components["api"] = comp
	case "CronJob":
		m.Tasks = map[string]spec.Task{
			"cleanup": {
				From:     "api",
				On:       spec.TaskOnSchedule,
				Schedule: "0 3 * * *",
				Command:  []string{"cleanup"},
			},
		}
	case "Job":
		m.Tasks = map[string]spec.Task{
			"migrate": {
				From:    "api",
				On:      spec.TaskOnPreDeploy,
				Command: []string{"migrate"},
			},
		}
	}
	return m
}

func envServiceSpec(dir string, env spec.StringMap) *spec.Spec {
	return &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]spec.Environment{
			"dev": {},
		},
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Port:  8080,
				Env:   env,
			},
		},
	}
}

func renderEnvManifest(t *testing.T, m *spec.Spec, env string) *render.RenderResult {
	t.Helper()
	resolved := resolveChart(t, m, env)
	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), m, env, resolved, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	return result
}

func assertNoConfigMapSuffix(t *testing.T, manifest, nameSuffix string) {
	t.Helper()
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var cm corev1.ConfigMap
		if err := yaml.Unmarshal([]byte(doc), &cm); err != nil {
			continue
		}
		if cm.Kind == "ConfigMap" && strings.HasSuffix(cm.Name, nameSuffix) {
			t.Fatalf("unexpected ConfigMap ending with %q: %s", nameSuffix, cm.Name)
		}
	}
}

func findRenderedConfigMap(t *testing.T, manifest, nameSuffix string) *corev1.ConfigMap {
	t.Helper()
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var cm corev1.ConfigMap
		if err := yaml.Unmarshal([]byte(doc), &cm); err != nil {
			continue
		}
		if cm.Kind == "ConfigMap" && strings.HasSuffix(cm.Name, nameSuffix) {
			return &cm
		}
	}
	t.Fatalf("no ConfigMap ending with %q", nameSuffix)
	return nil
}

func findOnlyDeployment(t *testing.T, manifest string) *appsv1.Deployment {
	t.Helper()
	var found *appsv1.Deployment
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var dep appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &dep); err != nil {
			continue
		}
		if dep.Kind != "Deployment" || dep.Name == "" {
			continue
		}
		if found != nil {
			t.Fatalf("expected one Deployment, also found %q", dep.Name)
		}
		foundDep := dep
		found = &foundDep
	}
	require.NotNil(t, found, "no Deployment")
	return found
}

func renderedContainerEnv(t *testing.T, result *render.RenderResult, kind, nameSuffix string) []corev1.EnvVar {
	t.Helper()
	switch kind {
	case "Deployment":
		dep := findRenderedDeployment(t, result.Manifest, nameSuffix)
		require.Len(t, dep.Spec.Template.Spec.Containers, 1)
		return dep.Spec.Template.Spec.Containers[0].Env
	case "StatefulSet":
		sts := findRenderedStatefulSet(t, result.Manifest, nameSuffix)
		require.Len(t, sts.Spec.Template.Spec.Containers, 1)
		return sts.Spec.Template.Spec.Containers[0].Env
	case "CronJob":
		cj := findRenderedCronJob(t, result.Manifest, nameSuffix)
		require.Len(t, cj.Spec.JobTemplate.Spec.Template.Spec.Containers, 1)
		return cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Env
	case "Job":
		hook := hookBySuffix(t, result, "Job", nameSuffix)
		var job batchv1.Job
		require.NoError(t, yaml.Unmarshal([]byte(hook.Manifest), &job))
		require.Len(t, job.Spec.Template.Spec.Containers, 1)
		return job.Spec.Template.Spec.Containers[0].Env
	default:
		t.Fatalf("unknown kind %q", kind)
		return nil
	}
}

func findEnvConfigMap(t *testing.T, result *render.RenderResult, kind, nameSuffix string) *corev1.ConfigMap {
	t.Helper()
	if kind == "Job" {
		hook := hookBySuffix(t, result, "ConfigMap", nameSuffix)
		var cm corev1.ConfigMap
		require.NoError(t, yaml.Unmarshal([]byte(hook.Manifest), &cm))
		return &cm
	}
	return findRenderedConfigMap(t, result.Manifest, nameSuffix)
}

func renderedEnvFromConfigMap(t *testing.T, result *render.RenderResult, kind, nameSuffix string) string {
	t.Helper()
	envFrom := renderedContainerEnvFrom(t, result, kind, nameSuffix)
	require.Len(t, envFrom, 1)
	require.NotNil(t, envFrom[0].ConfigMapRef)
	return envFrom[0].ConfigMapRef.Name
}

func renderedContainerEnvFrom(t *testing.T, result *render.RenderResult, kind, nameSuffix string) []corev1.EnvFromSource {
	t.Helper()
	switch kind {
	case "Deployment":
		dep := findRenderedDeployment(t, result.Manifest, nameSuffix)
		require.Len(t, dep.Spec.Template.Spec.Containers, 1)
		return dep.Spec.Template.Spec.Containers[0].EnvFrom
	case "StatefulSet":
		sts := findRenderedStatefulSet(t, result.Manifest, nameSuffix)
		require.Len(t, sts.Spec.Template.Spec.Containers, 1)
		return sts.Spec.Template.Spec.Containers[0].EnvFrom
	case "CronJob":
		cj := findRenderedCronJob(t, result.Manifest, nameSuffix)
		require.Len(t, cj.Spec.JobTemplate.Spec.Template.Spec.Containers, 1)
		return cj.Spec.JobTemplate.Spec.Template.Spec.Containers[0].EnvFrom
	case "Job":
		hook := hookBySuffix(t, result, "Job", nameSuffix)
		var job batchv1.Job
		require.NoError(t, yaml.Unmarshal([]byte(hook.Manifest), &job))
		require.Len(t, job.Spec.Template.Spec.Containers, 1)
		return job.Spec.Template.Spec.Containers[0].EnvFrom
	default:
		t.Fatalf("unknown kind %q", kind)
		return nil
	}
}

func findRenderedDeployment(t *testing.T, manifest, nameSuffix string) *appsv1.Deployment {
	t.Helper()
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var dep appsv1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &dep); err != nil {
			continue
		}
		if dep.Kind == "Deployment" && strings.HasSuffix(dep.Name, nameSuffix) {
			return &dep
		}
	}
	t.Fatalf("no Deployment ending with %q", nameSuffix)
	return nil
}

func findRenderedStatefulSet(t *testing.T, manifest, nameSuffix string) *appsv1.StatefulSet {
	t.Helper()
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var sts appsv1.StatefulSet
		if err := yaml.Unmarshal([]byte(doc), &sts); err != nil {
			continue
		}
		if sts.Kind == "StatefulSet" && strings.HasSuffix(sts.Name, nameSuffix) {
			return &sts
		}
	}
	t.Fatalf("no StatefulSet ending with %q", nameSuffix)
	return nil
}

func findRenderedCronJob(t *testing.T, manifest, nameSuffix string) *batchv1.CronJob {
	t.Helper()
	for doc := range strings.SplitSeq(manifest, "\n---\n") {
		var cj batchv1.CronJob
		if err := yaml.Unmarshal([]byte(doc), &cj); err != nil {
			continue
		}
		if cj.Kind == "CronJob" && strings.HasSuffix(cj.Name, nameSuffix) {
			return &cj
		}
	}
	t.Fatalf("no CronJob ending with %q", nameSuffix)
	return nil
}

func hookBySuffix(t *testing.T, result *render.RenderResult, kind, suffix string) *releasev1.Hook {
	t.Helper()
	for _, h := range result.Hooks {
		if h != nil && h.Kind == kind && strings.HasSuffix(h.Name, suffix) {
			return h
		}
	}
	t.Fatalf("no hook %s ending with %q", kind, suffix)
	return nil
}

func assertNoHookKindSuffix(t *testing.T, result *render.RenderResult, kind, suffix string) {
	t.Helper()
	for _, h := range result.Hooks {
		if h != nil && h.Kind == kind && strings.HasSuffix(h.Name, suffix) {
			t.Fatalf("unexpected hook %s ending with %q: %s", kind, suffix, h.Name)
		}
	}
}
