// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package helm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/spec"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func taskSpec() *spec.Spec {
	backoff := spec.DefaultBackoffLimit
	return &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Port:  8080,
				Env:   map[string]string{"DATABASE_URL": "postgres://db", "LOG": "info"},
			},
		},
		Tasks: map[string]spec.Task{
			"migrate": {
				From:         "api",
				On:           spec.TaskOnPreDeploy,
				Command:      []string{"migrate", "up"},
				Timeout:      spec.DefaultHookTaskTimeout,
				BackoffLimit: &backoff,
				Env:          map[string]string{"LOG": "debug"},
			},
			"smoke": {
				From:         "api",
				On:           spec.TaskOnPostDeploy,
				Command:      []string{"curl", "-f", "http://api/health"},
				Timeout:      spec.DefaultHookTaskTimeout,
				BackoffLimit: &backoff,
			},
			"backfill": {
				From:    "api",
				On:      spec.TaskOnManual,
				Command: []string{"backfill"},
				Fanout:  spec.Fanout{Count: 4, Parallelism: 2},
			},
			"cleanup": {
				From:     "api",
				On:       spec.TaskOnSchedule,
				Command:  []string{"cleanup"},
				Schedule: "0 3 * * *",
			},
		},
	}
}

func TestMapSpecToChartValues_HookTasks(t *testing.T) {
	t.Parallel()

	m := taskSpec()
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	for name, task := range m.Tasks {
		if p, ok := m.Components[task.From]; ok {
			cp := p
			m.Tasks[name] = task.MergeFrom(&cp)
		}
	}

	vals, err := MapSpecToChartValues(m, "dev", nil)
	require.NoError(t, err)

	migrate := mustNestedMap(t, vals, "migrate")
	job := mustNestedMap(t, migrate, "job")
	assert.Equal(t, true, job["enabled"])
	assert.Equal(t, "pre-install,pre-upgrade", job["hook"])
	assert.Equal(t, 0, job["hookWeight"])
	assert.Equal(t, hookDeletePolicy, job["hookDeletePolicy"])
	assert.Equal(t, 1, job["completions"])
	assert.Equal(t, 1, job["parallelism"])
	assert.Equal(t, spec.DefaultBackoffLimit, job["backoffLimit"])
	assert.Equal(t, 300, job["activeDeadlineSeconds"])
	assert.Equal(t, []string{"migrate", "up"}, migrate["command"])
	assert.Equal(t, map[string]string{"DATABASE_URL": "postgres://db", "LOG": "debug"}, migrate["envVars"])

	labels, ok := migrate["commonLabels"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "migrate", labels[spec.LabelComponent])

	smoke := mustNestedMap(t, vals, "smoke")
	smokeJob := mustNestedMap(t, smoke, "job")
	assert.Equal(t, "post-install,post-upgrade", smokeJob["hook"])

	_, hasManual := vals["backfill"]
	assert.False(t, hasManual, "manual tasks must be absent from chart values")

	cleanup := mustNestedMap(t, vals, "cleanup")
	cronjob := mustNestedMap(t, cleanup, "cronjob")
	assert.Equal(t, true, cronjob["enabled"])
	assert.Equal(t, "0 3 * * *", cronjob["schedule"])
	assert.Equal(t, spec.DefaultScheduleTimeZone, cronjob["timeZone"])
	assert.Equal(t, spec.DefaultConcurrencyPolicy, cronjob["concurrencyPolicy"])
	assert.Equal(t, 3600, cronjob["activeDeadlineSeconds"])
	_, hasJob := cleanup["job"]
	assert.False(t, hasJob, "scheduled tasks must not render a hook Job")

	deployah := mustNestedMap(t, vals, "deployah")
	resolved := mustNestedMap(t, deployah, "resolved")
	require.Contains(t, resolved, "tasks")
	_, hasSA := deployah["tasks"]
	assert.False(t, hasSA, "hook tasks must not request a dedicated ServiceAccount")
}

func TestMapSpecToChartValues_ManualOnlyOmitsTaskValues(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleService, Image: "nginx:latest", Port: 80},
		},
		Tasks: map[string]spec.Task{
			"backfill": {From: "api", On: spec.TaskOnManual, Command: []string{"true"}},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	vals, err := MapSpecToChartValues(m, "dev", nil)
	require.NoError(t, err)
	_, hasBackfill := vals["backfill"]
	assert.False(t, hasBackfill)
	if deployah, ok := vals["deployah"].(map[string]any); ok {
		_, hasSA := deployah["tasks"]
		assert.False(t, hasSA)
	}
}

// TestPrepareChart_ChartYAMLImportsOnlySubCharts pins Chart.yaml to the same
// names as [createComponentSubCharts] and [createTaskSubCharts]. A manual task
// and a component scoped to another environment get no sub-chart, so an
// import-values entry under either name would point at a chart that is not
// there.
func TestPrepareChart_ChartYAMLImportsOnlySubCharts(t *testing.T) {
	t.Parallel()

	m := taskSpec()
	m.Components["worker"] = spec.Component{
		Role:         spec.ComponentRoleWorker,
		Image:        "ghcr.io/acme/shop:1.2.3",
		Environments: []string{"prod"},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))

	cache := NewChartCache(time.Hour)
	const environment = "dev"
	chartDir, err := PrepareChart(t.Context(), m, environment, nil, cache)
	require.NoError(t, err)
	t.Cleanup(func() { removeChartDirs(t, cache, m, environment, chartDir) })

	raw, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml")) // #nosec G304 -- chartDir is the temp dir PrepareChart just created
	require.NoError(t, err)

	var chart struct {
		Dependencies []struct {
			ImportValues []struct {
				Parent string `json:"parent"`
			} `json:"import-values"`
		} `json:"dependencies"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &chart))
	require.Len(t, chart.Dependencies, 1)

	parents := make([]string, 0, len(chart.Dependencies[0].ImportValues))
	for _, iv := range chart.Dependencies[0].ImportValues {
		parents = append(parents, iv.Parent)
	}
	// Components first, then tasks, each sorted: the order is part of what
	// keeps a regenerated chart byte-identical.
	assert.Equal(t, []string{"api", "cleanup", "migrate", "smoke"}, parents,
		"manual task backfill and prod-only component worker must not be imported")
	assert.DirExists(t, filepath.Join(chartDir, "charts", "migrate"))
	assert.DirExists(t, filepath.Join(chartDir, "charts", "cleanup"))
	assert.FileExists(t, filepath.Join(chartDir, "charts", "cleanup", "templates", "cronjob.yaml"))
	assert.NoFileExists(t, filepath.Join(chartDir, "charts", "cleanup", "templates", "job.yaml"))
	assert.NoDirExists(t, filepath.Join(chartDir, "charts", "backfill"))
	assert.NoDirExists(t, filepath.Join(chartDir, "charts", "worker"))
}

// removeChartDirs deletes both the copy PrepareChart returned and the
// directory backing its cache entry.
func removeChartDirs(tb testing.TB, cache *ChartCache, m *spec.Spec, environment, returned string) {
	tb.Helper()
	removeChartDir(tb, returned)
	key, err := cache.GenerateKey(m, environment, nil)
	if err != nil {
		tb.Logf("cleanup: cache key: %v", err)
		return
	}
	if cached, found := cache.get(key); found {
		removeChartDir(tb, cached)
	}
}

func TestHookTasksForChart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		spec        *spec.Spec
		environment string
		wantWeight  map[string]int
	}{
		{
			name: "after sets hook weight",
			spec: &spec.Spec{
				Project: "shop",
				Components: map[string]spec.Component{
					"api": {Image: "nginx:latest"},
				},
				Tasks: map[string]spec.Task{
					"seed":    {From: "api", On: spec.TaskOnPreDeploy, After: []string{"migrate"}, Command: []string{"true"}},
					"migrate": {From: "api", On: spec.TaskOnPreDeploy, Command: []string{"true"}},
				},
			},
			environment: "dev",
			wantWeight:  map[string]int{"migrate": 0, "seed": 1},
		},
		{
			name: "skips inherited other environment",
			spec: &spec.Spec{
				Project: "shop",
				Components: map[string]spec.Component{
					"api": {Image: "nginx:latest", Environments: []string{"prod"}},
				},
				Tasks: map[string]spec.Task{
					"migrate": {From: "api", On: spec.TaskOnPreDeploy, Command: []string{"true"}},
				},
			},
			environment: "dev",
			wantWeight:  map[string]int{},
		},
		{
			name: "includes matching environment",
			spec: &spec.Spec{
				Project: "shop",
				Components: map[string]spec.Component{
					"api": {Image: "nginx:latest", Environments: []string{"prod"}},
				},
				Tasks: map[string]spec.Task{
					"migrate": {From: "api", On: spec.TaskOnPreDeploy, Command: []string{"true"}},
				},
			},
			environment: "prod",
			wantWeight:  map[string]int{"migrate": 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := hookTasksForChart(tt.spec, tt.environment, nil)
			require.NoError(t, err)
			weights := make(map[string]int, len(got))
			for name, rt := range got {
				weights[name] = rt.HookWeight
			}
			assert.Equal(t, tt.wantWeight, weights)
		})
	}
}

func TestMapTaskToChartValues_DigestArgsEphemeralTTL(t *testing.T) {
	t.Parallel()

	ttl := 60
	m := &spec.Spec{Project: "shop"}
	rt := spec.ResolvedTask{
		Task: spec.Task{
			Image:   "nginx@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			On:      spec.TaskOnPreDeploy,
			Command: []string{"migrate"},
			Args:    []string{"up"},
			Resources: spec.Resources{
				CPU:              spec.MustQuantity("100m"),
				Memory:           spec.MustQuantity("128Mi"),
				EphemeralStorage: spec.MustQuantity("1Gi"),
			},
			TTLSecondsAfterFinished: &ttl,
			Timeout:                 spec.DefaultHookTaskTimeout,
		},
		HookWeight: 2,
	}

	vals, err := mapTaskToChartValues(m, "migrate", rt, "dev")
	require.NoError(t, err)

	image := mustNestedMap(t, vals, "image")
	assert.Equal(t, "docker.io/library/nginx", image["repository"])
	assert.Equal(t, "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", image["digest"])
	_, hasTag := image["tag"]
	assert.False(t, hasTag)
	assert.Equal(t, []string{"migrate"}, vals["command"])
	assert.Equal(t, []string{"up"}, vals["args"])

	resources := mustNestedMap(t, vals, "resources")
	requests := mustNestedMap(t, resources, "requests")
	assert.Equal(t, "100m", requests["cpu"])
	assert.Equal(t, "128Mi", requests["memory"])
	assert.Equal(t, "1Gi", requests["ephemeral-storage"])

	job := mustNestedMap(t, vals, "job")
	assert.Equal(t, 2, job["hookWeight"])
	assert.Equal(t, 60, job["ttlSecondsAfterFinished"])
	assert.Equal(t, 300, job["activeDeadlineSeconds"])
}

func TestHookTasksForChart_Cycle(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Image: "nginx:latest"},
		},
		Tasks: map[string]spec.Task{
			"a": {From: "api", On: spec.TaskOnPreDeploy, After: []string{"b"}, Command: []string{"true"}},
			"b": {From: "api", On: spec.TaskOnPreDeploy, After: []string{"a"}, Command: []string{"true"}},
		},
	}
	_, err := hookTasksForChart(m, "dev", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestMapSpecToChartValues_AppliesTaskProfile(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		Project: "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleService, Image: "nginx:latest", Port: 80},
		},
		Tasks: map[string]spec.Task{
			"migrate": {From: "api", On: spec.TaskOnPreDeploy, Command: []string{"true"}},
		},
	}
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	task, ok := m.MergedTask("migrate")
	require.True(t, ok)
	resolved := &spec.ResolvedSpec{
		Tasks: map[string]spec.ResolvedTask{
			"migrate": {
				Task: task,
				MergedProfile: &spec.PlatformProfile{
					NodeSelector: map[string]string{"workload": "batch"},
				},
			},
		},
	}
	vals, err := MapSpecToChartValues(m, "dev", resolved)
	require.NoError(t, err)
	migrate := mustNestedMap(t, vals, "migrate")
	assert.Equal(t, map[string]string{"workload": "batch"}, migrate["nodeSelector"])
}

func TestHelmJob_CommandBracesAreData(t *testing.T) {
	t.Parallel()

	m := hookRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnPreDeploy,
		Command: []string{"echo", "{{ .Release.Name }}"},
	})
	job := renderHookJob(t, m, "dev", "migrate")
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []string{"echo", "{{ .Release.Name }}"}, job.Spec.Template.Spec.Containers[0].Command)
}

func TestHelmJob_EnvBracesAreData(t *testing.T) {
	t.Parallel()

	m := hookRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnPreDeploy,
		Command: []string{"true"},
		Env:     map[string]string{"NOTE": "{{ .Release.Name }}"},
	})
	job := renderHookJob(t, m, "dev", "migrate")
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []corev1.EnvVar{
		{Name: "DATABASE_URL", Value: "postgres://db"},
		{Name: "LOG", Value: "info"},
		{Name: "NOTE", Value: "{{ .Release.Name }}"},
	}, job.Spec.Template.Spec.Containers[0].Env)
}

func TestHelmJob_TTLSecondsAfterFinished(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ttl  *int
		want *int32
	}{
		{name: "zero deletes immediately", ttl: new(0), want: new(int32(0))},
		{name: "positive value", ttl: new(60), want: new(int32(60))},
		{name: "omitted", ttl: nil, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := hookRenderSpec(spec.Task{
				From:                    "api",
				On:                      spec.TaskOnPreDeploy,
				Command:                 []string{"true"},
				TTLSecondsAfterFinished: tt.ttl,
			})
			job := renderHookJob(t, m, "dev", "migrate")
			assert.Equal(t, tt.want, job.Spec.TTLSecondsAfterFinished)
		})
	}
}

func TestHelmJob_ShortNameImageGetsRegistryPrefix(t *testing.T) {
	t.Parallel()

	m := hookRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnPreDeploy,
		Image:   "nginx:latest",
		Command: []string{"true"},
	})
	job := renderHookJob(t, m, "dev", "migrate")
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "docker.io/library/nginx:latest", job.Spec.Template.Spec.Containers[0].Image)
}

func TestHelmJobPodMatchesBuildTaskJob(t *testing.T) {
	t.Parallel()

	task := spec.Task{
		From:    "api",
		On:      spec.TaskOnPreDeploy,
		Command: []string{"migrate", "up"},
		Args:    []string{"--strict"},
		Env:     map[string]string{"LOG": "debug"},
		Fanout:  spec.Fanout{Count: 2, Parallelism: 1},
		Timeout: spec.DefaultHookTaskTimeout,
	}
	m := hookRenderSpec(task)
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	merged, ok := m.MergedTask("migrate")
	require.True(t, ok)

	helmJob := renderHookJob(t, m, "dev", "migrate")
	cliJob, err := k8s.BuildTaskJob(k8s.TaskJobOptions{
		Project:     m.Project,
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "migrate",
		Task:        merged,
	})
	require.NoError(t, err)

	helmPod := helmJob.Spec.Template.Spec
	cliPod := cliJob.Spec.Template.Spec
	require.Len(t, helmPod.Containers, 1)
	require.Len(t, cliPod.Containers, 1)
	assert.Equal(t, cliPod.Containers[0].Image, helmPod.Containers[0].Image)
	assert.Equal(t, cliPod.Containers[0].Command, helmPod.Containers[0].Command)
	assert.Equal(t, cliPod.Containers[0].Args, helmPod.Containers[0].Args)
	assert.ElementsMatch(t, cliPod.Containers[0].Env, helmPod.Containers[0].Env)
	assertResourceListsEqual(t, cliPod.Containers[0].Resources.Requests, helmPod.Containers[0].Resources.Requests)
	require.NotNil(t, helmPod.AutomountServiceAccountToken)
	require.NotNil(t, cliPod.AutomountServiceAccountToken)
	assert.False(t, *helmPod.AutomountServiceAccountToken)
	assert.False(t, *cliPod.AutomountServiceAccountToken)
	assert.Empty(t, helmPod.ServiceAccountName)
	assert.Empty(t, cliPod.ServiceAccountName)
	assert.Equal(t, cliPod.RestartPolicy, helmPod.RestartPolicy)
	require.NotNil(t, helmJob.Spec.Completions)
	require.NotNil(t, cliJob.Spec.Completions)
	assert.Equal(t, *cliJob.Spec.Completions, *helmJob.Spec.Completions)
	require.NotNil(t, helmJob.Spec.Parallelism)
	require.NotNil(t, cliJob.Spec.Parallelism)
	assert.Equal(t, *cliJob.Spec.Parallelism, *helmJob.Spec.Parallelism)
	require.NotNil(t, helmJob.Spec.BackoffLimit)
	require.NotNil(t, cliJob.Spec.BackoffLimit)
	assert.Equal(t, *cliJob.Spec.BackoffLimit, *helmJob.Spec.BackoffLimit)
	require.NotNil(t, helmJob.Spec.ActiveDeadlineSeconds)
	require.NotNil(t, cliJob.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, *cliJob.Spec.ActiveDeadlineSeconds, *helmJob.Spec.ActiveDeadlineSeconds)
}

func hookRenderSpec(task spec.Task) *spec.Spec {
	return &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {
				Role:  spec.ComponentRoleService,
				Image: "ghcr.io/acme/shop:1.2.3",
				Port:  8080,
				Env:   map[string]string{"DATABASE_URL": "postgres://db", "LOG": "info"},
			},
		},
		Tasks: map[string]spec.Task{
			"migrate": task,
		},
		Environments: map[string]spec.Environment{
			"dev": {},
		},
	}
}

func renderHookJob(t *testing.T, manifest *spec.Spec, env, taskName string) *batchv1.Job {
	t.Helper()
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	for name, task := range manifest.Tasks {
		if p, ok := manifest.Components[task.From]; ok {
			cp := p
			manifest.Tasks[name] = task.MergeFrom(&cp)
		}
	}

	client, err := NewClient(WithNamespace("default"))
	require.NoError(t, err)
	result, cleanup, err := client.RenderOffline(t.Context(), manifest, env, nil, nil)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}

	suffix := "-" + taskName
	for _, h := range result.Hooks {
		if h == nil || h.Manifest == "" {
			continue
		}
		var job batchv1.Job
		if unmarshalErr := yaml.Unmarshal([]byte(h.Manifest), &job); unmarshalErr != nil {
			continue
		}
		if job.Kind == "Job" && strings.HasSuffix(job.Name, suffix) {
			return &job
		}
	}
	t.Fatalf("no hook Job ending with %q", suffix)
	return nil
}

func assertResourceListsEqual(t *testing.T, a, b corev1.ResourceList) {
	t.Helper()
	require.Equal(t, len(a), len(b), "resource list length")
	for name, aq := range a {
		bq, ok := b[name]
		require.True(t, ok, "missing resource %s", name)
		assert.True(t, aq.Equal(bq), "resource %s: %s vs %s", name, aq.String(), bq.String())
	}
}
