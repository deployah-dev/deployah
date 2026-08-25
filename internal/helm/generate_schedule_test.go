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
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	batchv1 "k8s.io/api/batch/v1"
)

func scheduledRenderSpec(task spec.Task) *spec.Spec {
	if task.On == "" {
		task.On = spec.TaskOnSchedule
	}
	if task.Schedule == "" {
		task.Schedule = "0 3 * * *"
	}
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
			"cleanup": task,
		},
		Environments: map[string]spec.Environment{
			"dev": {},
		},
	}
}

func TestMapScheduledTaskToChartValues(t *testing.T) {
	t.Parallel()

	ttl := 0
	m := &spec.Spec{Project: "shop"}
	rt := spec.ResolvedTask{
		Task: spec.Task{
			Image:                   "busybox:1.36",
			On:                      spec.TaskOnSchedule,
			Schedule:                "0 3 * * *",
			Command:                 []string{"cleanup"},
			TTLSecondsAfterFinished: &ttl,
		},
	}

	vals, err := mapScheduledTaskToChartValues(m, "cleanup", rt, "dev")
	require.NoError(t, err)

	cronjob := mustNestedMap(t, vals, "cronjob")
	assert.Equal(t, true, cronjob["enabled"])
	assert.Equal(t, "0 3 * * *", cronjob["schedule"])
	assert.Equal(t, spec.DefaultScheduleTimeZone, cronjob["timeZone"])
	assert.Equal(t, spec.DefaultConcurrencyPolicy, cronjob["concurrencyPolicy"])
	assert.Equal(t, false, cronjob["suspend"])
	assert.Equal(t, spec.DefaultSuccessfulJobsHistory, cronjob["successfulJobsHistoryLimit"])
	assert.Equal(t, spec.DefaultFailedJobsHistory, cronjob["failedJobsHistoryLimit"])
	assert.Equal(t, 1, cronjob["completions"])
	assert.Equal(t, 1, cronjob["parallelism"])
	assert.Equal(t, spec.DefaultBackoffLimit, cronjob["backoffLimit"])
	assert.Equal(t, 3600, cronjob["activeDeadlineSeconds"])
	assert.Equal(t, 0, cronjob["ttlSecondsAfterFinished"])
	_, hasStart := cronjob["startingDeadlineSeconds"]
	assert.False(t, hasStart)
	_, hasOverride := vals["fullnameOverride"]
	assert.False(t, hasOverride)
	_, hasJob := vals["job"]
	assert.False(t, hasJob)
}

func TestMapScheduledTaskToChartValues_ExplicitTimeout(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{Project: "shop"}
	rt := spec.ResolvedTask{
		Task: spec.Task{
			Image:    "busybox:1.36",
			On:       spec.TaskOnSchedule,
			Schedule: "@hourly",
			Timeout:  "30m",
			Command:  []string{"true"},
			Fanout:   spec.Fanout{Count: 2, Parallelism: 1},
			Suspend:  new(true),
		},
	}

	vals, err := mapScheduledTaskToChartValues(m, "cleanup", rt, "dev")
	require.NoError(t, err)
	cronjob := mustNestedMap(t, vals, "cronjob")
	assert.Equal(t, 1800, cronjob["activeDeadlineSeconds"])
	assert.Equal(t, 2, cronjob["completions"])
	assert.Equal(t, true, cronjob["suspend"])
	_, hasTTL := cronjob["ttlSecondsAfterFinished"]
	assert.False(t, hasTTL)
}

func TestHelmCronJob_InMainManifest(t *testing.T) {
	t.Parallel()

	m := scheduledRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnSchedule,
		Command: []string{"cleanup"},
	})
	cj := renderScheduledCronJob(t, m, "dev", "cleanup")
	assert.Equal(t, "batch/v1", cj.APIVersion)
	assert.Equal(t, "shop-dev-cleanup", cj.Name)
	assert.Equal(t, "0 3 * * *", cj.Spec.Schedule)
	require.NotNil(t, cj.Spec.TimeZone)
	assert.Equal(t, spec.DefaultScheduleTimeZone, *cj.Spec.TimeZone)
	assert.Equal(t, batchv1.ForbidConcurrent, cj.Spec.ConcurrencyPolicy)
	require.NotNil(t, cj.Spec.Suspend)
	assert.False(t, *cj.Spec.Suspend)
	require.NotNil(t, cj.Spec.SuccessfulJobsHistoryLimit)
	assert.Equal(t, int32(spec.DefaultSuccessfulJobsHistory), *cj.Spec.SuccessfulJobsHistoryLimit)
	require.NotNil(t, cj.Spec.FailedJobsHistoryLimit)
	assert.Equal(t, int32(spec.DefaultFailedJobsHistory), *cj.Spec.FailedJobsHistoryLimit)
	assert.Nil(t, cj.Spec.StartingDeadlineSeconds)
	require.NotNil(t, cj.Spec.JobTemplate.Spec.CompletionMode)
	assert.Equal(t, batchv1.IndexedCompletion, *cj.Spec.JobTemplate.Spec.CompletionMode)
	assert.Equal(t, "OnFailure", string(cj.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy))
	require.NotNil(t, cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, int64(3600), *cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
	assert.Empty(t, cj.Annotations["helm.sh/hook"])
	assert.Equal(t, "cleanup", cj.Labels[spec.LabelComponent])
	assert.Equal(t, "shop", cj.Labels[spec.LabelProject])
}

func TestHelmCronJob_EveryAccepted(t *testing.T) {
	t.Parallel()

	m := scheduledRenderSpec(spec.Task{
		From:     "api",
		On:       spec.TaskOnSchedule,
		Schedule: "@every 1h",
		Command:  []string{"true"},
	})
	cj := renderScheduledCronJob(t, m, "dev", "cleanup")
	assert.Equal(t, "@every 1h", cj.Spec.Schedule)
}

func TestCronJobName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		releaseName string
		task        string
		wantExact   string
		wantSuffix  string
		wantLen     int
	}{
		{
			name:        "under 52 passes through",
			releaseName: "shop-dev",
			task:        "cleanup",
			wantExact:   "shop-dev-cleanup",
		},
		{
			name:        "task named after the project",
			releaseName: "shop-dev",
			task:        "shop",
			wantExact:   "shop-dev-shop",
		},
		{
			name:        "over 52 truncates with hash and keeps task suffix",
			releaseName: "shop-review-feature-add-new-checkout-flow-with-stripe",
			task:        "cleanup",
			wantSuffix:  "-cleanup",
			wantLen:     52,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := scheduledTaskNamed(tt.task)
			result := mustRenderScheduled(t, m, "dev", tt.releaseName, "")
			cj := cronJobFromManifest(t, result.Manifest, tt.task)
			if tt.wantExact != "" {
				assert.Equal(t, tt.wantExact, cj.Name)
				return
			}
			assert.LessOrEqual(t, len(cj.Name), 52)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantLen, len(cj.Name))
			}
			assert.True(t, strings.HasSuffix(cj.Name, tt.wantSuffix), "name %q suffix", cj.Name)
			assert.Regexp(t, regexp.MustCompile(`-[0-9a-f]{4}`+regexp.QuoteMeta(tt.wantSuffix)+`$`), cj.Name)
		})
	}
}

func TestCronJobName_TwoTasksStayDistinguishable(t *testing.T) {
	t.Parallel()

	m := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {Role: spec.ComponentRoleService, Image: "busybox:1.36", Port: 8080},
		},
		Tasks: map[string]spec.Task{
			"cleanup": {From: "api", On: spec.TaskOnSchedule, Schedule: "0 3 * * *", Command: []string{"true"}},
			"compact": {From: "api", On: spec.TaskOnSchedule, Schedule: "0 4 * * *", Command: []string{"true"}},
		},
		Environments: map[string]spec.Environment{"dev": {}},
	}
	const release = "shop-review-feature-add-new-checkout-flow-with-stripe"
	result := mustRenderScheduled(t, m, "dev", release, "")
	cleanup := cronJobFromManifest(t, result.Manifest, "cleanup")
	compact := cronJobFromManifest(t, result.Manifest, "compact")
	assert.NotEqual(t, cleanup.Name, compact.Name)
	assert.True(t, strings.HasSuffix(cleanup.Name, "-cleanup"))
	assert.True(t, strings.HasSuffix(compact.Name, "-compact"))
}

func TestReleaseNamePortability(t *testing.T) {
	t.Parallel()

	m := scheduledRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnSchedule,
		Command: []string{"true"},
	})
	first := mustRenderScheduled(t, m, "dev", "alpha-dev", "")
	second := mustRenderScheduled(t, m, "dev", "bravo-dev", "")
	a := cronJobFromManifest(t, first.Manifest, "cleanup")
	b := cronJobFromManifest(t, second.Manifest, "cleanup")
	assert.Equal(t, "alpha-dev-cleanup", a.Name)
	assert.Equal(t, "bravo-dev-cleanup", b.Name)
}

func TestTimeZoneGuard(t *testing.T) {
	t.Parallel()

	t.Run("non-UTC fails on v1.26", func(t *testing.T) {
		t.Parallel()
		m := scheduledRenderSpec(spec.Task{
			From:     "api",
			On:       spec.TaskOnSchedule,
			TimeZone: "Europe/Berlin",
			Command:  []string{"true"},
		})
		_, err := renderScheduled(t, m, "dev", "shop-dev", "v1.26.0")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires Kubernetes 1.27")
		assert.Contains(t, err.Error(), "Europe/Berlin")
		assert.NotContains(t, err.Error(), "now")
	})

	t.Run("Etc/UTC still renders on v1.26", func(t *testing.T) {
		t.Parallel()
		m := scheduledRenderSpec(spec.Task{
			From:     "api",
			On:       spec.TaskOnSchedule,
			TimeZone: spec.DefaultScheduleTimeZone,
			Command:  []string{"true"},
		})
		result := mustRenderScheduled(t, m, "dev", "shop-dev", "v1.26.0")
		cj := cronJobFromManifest(t, result.Manifest, "cleanup")
		require.NotNil(t, cj.Spec.TimeZone)
		assert.Equal(t, spec.DefaultScheduleTimeZone, *cj.Spec.TimeZone)
	})
}

func TestHelmCronJob_RunJobHasNoDeadlineWhenTimeoutOmitted(t *testing.T) {
	t.Parallel()

	m := scheduledRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnSchedule,
		Command: []string{"cleanup"},
	})
	require.NoError(t, spec.FillSpecWithDefaults(m, spec.CurrentManifestVersion))
	for name, task := range m.Tasks {
		if p, ok := m.Components[task.From]; ok {
			cp := p
			m.Tasks[name] = task.MergeFrom(&cp)
		}
	}
	merged, ok := m.MergedTask("cleanup")
	require.True(t, ok)
	assert.Empty(t, merged.Timeout)

	cj := renderScheduledCronJob(t, m, "dev", "cleanup")
	require.NotNil(t, cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, int64(3600), *cj.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)

	job, err := k8s.BuildTaskJob(k8s.TaskJobOptions{
		Project:     m.Project,
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "cleanup",
		Task:        merged,
	})
	require.NoError(t, err)
	assert.Nil(t, job.Spec.ActiveDeadlineSeconds)
}

func scheduledTaskNamed(name string) *spec.Spec {
	m := scheduledRenderSpec(spec.Task{
		From:    "api",
		On:      spec.TaskOnSchedule,
		Command: []string{"true"},
	})
	m.Tasks = map[string]spec.Task{name: m.Tasks["cleanup"]}
	return m
}

func renderScheduledCronJob(t *testing.T, manifest *spec.Spec, env, taskName string) *batchv1.CronJob {
	t.Helper()
	result, cleanup, err := renderOfflineScheduled(t, manifest, env)
	require.NoError(t, err)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	for _, h := range result.Hooks {
		if h != nil && strings.Contains(h.Manifest, "kind: CronJob") {
			t.Fatalf("scheduled CronJob must not be a Helm hook")
		}
	}
	return cronJobFromManifest(t, result.Manifest, taskName)
}

func renderOfflineScheduled(t *testing.T, manifest *spec.Spec, env string) (*render.RenderResult, func(), error) {
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
	return client.RenderOffline(t.Context(), manifest, env, nil, nil)
}

func mustRenderScheduled(t *testing.T, manifest *spec.Spec, env, releaseName, kubeVersion string) *render.RenderResult {
	t.Helper()
	result, err := renderScheduled(t, manifest, env, releaseName, kubeVersion)
	require.NoError(t, err)
	return result
}

func renderScheduled(t *testing.T, manifest *spec.Spec, env, releaseName, kubeVersion string) (*render.RenderResult, error) {
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

	ch, _, cleanup, err := client.prepareAndLoadChart(t.Context(), manifest, env, nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(cleanup)

	values, labels := renderInputs(manifest, env)
	defer restoreCapabilitiesForDryRun(client.config)()

	install := action.NewInstall(client.config)
	install.ReleaseName = releaseName
	install.Namespace = client.settings.Namespace()
	install.CreateNamespace = true
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true
	install.Labels = labels
	install.APIVersions = chartcommon.VersionSet{offlineMonitorAPIVersion}
	if kubeVersion != "" {
		kv, parseErr := chartcommon.ParseKubeVersion(kubeVersion)
		require.NoError(t, parseErr)
		install.KubeVersion = kv
	}

	rel, runErr := install.RunWithContext(t.Context(), ch, values)
	if runErr != nil {
		return nil, client.wrapHelmError("render", releaseName, runErr)
	}
	v1rel, convErr := releaserToV1(rel)
	if convErr != nil {
		return nil, convErr
	}
	return &render.RenderResult{
		ReleaseName: releaseName,
		Namespace:   install.Namespace,
		Manifest:    v1rel.Manifest,
		Hooks:       v1rel.Hooks,
		IsUpgrade:   false,
		Revision:    1,
	}, nil
}

func cronJobFromManifest(t *testing.T, manifest, taskName string) *batchv1.CronJob {
	t.Helper()
	suffix := "-" + taskName
	for doc := range strings.SplitSeq(manifest, "---") {
		var cj batchv1.CronJob
		if unmarshalErr := yaml.Unmarshal([]byte(doc), &cj); unmarshalErr != nil {
			continue
		}
		if cj.Kind == "CronJob" && strings.HasSuffix(cj.Name, suffix) {
			return &cj
		}
	}
	t.Fatalf("no CronJob ending with %q in manifest", suffix)
	return nil
}
