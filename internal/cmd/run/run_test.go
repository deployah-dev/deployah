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

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/spec"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stesting "k8s.io/client-go/testing"
)

func testManifest() *spec.Spec {
	return &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {Image: "ghcr.io/acme/shop:1.2.3", Env: spec.StringMap{"DATABASE_URL": "postgres://db"}},
		},
		Tasks: map[string]spec.Task{
			"migrate": {
				From:    "api",
				On:      spec.TaskOnPreDeploy,
				Command: []string{"migrate", "up"},
			},
			"backfill": {
				From:         "api",
				On:           spec.TaskOnManual,
				Command:      []string{"backfill"},
				Environments: []string{"prod"},
			},
			"cleanup": {
				From:     "api",
				On:       spec.TaskOnSchedule,
				Schedule: "0 3 * * *",
				Command:  []string{"cleanup"},
			},
		},
	}
}

func TestResolveTask(t *testing.T) {
	t.Parallel()

	m := testManifest()

	t.Run("merges from parent", func(t *testing.T) {
		t.Parallel()
		rt, err := spec.ResolveTask(m, nil, spec.NormalizeEnv("dev"), "migrate")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)
		assert.Equal(t, "postgres://db", rt.Runtime.ExplicitValues["DATABASE_URL"])
		assert.Equal(t, []string{"migrate", "up"}, rt.Task.Command)
	})

	t.Run("scheduled task is runnable", func(t *testing.T) {
		t.Parallel()
		rt, err := spec.ResolveTask(m, nil, spec.NormalizeEnv("dev"), "cleanup")
		require.NoError(t, err)
		assert.Equal(t, spec.TaskOnSchedule, rt.Task.On)
		assert.Equal(t, "0 3 * * *", rt.Task.Schedule)
		assert.Empty(t, rt.Task.Timeout)
	})

	t.Run("empty inherited profiles do not require a platform file", func(t *testing.T) {
		t.Parallel()
		local := testManifest()
		api := local.Components["api"]
		api.Profiles = []string{}
		local.Components["api"] = api
		rt, err := spec.ResolveTask(local, nil, spec.NormalizeEnv("dev"), "migrate")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)
	})

	t.Run("expose without platform still resolves runtime", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.dev"), []byte("REGION=eu\n"), 0o600))
		local := testManifest()
		local.SpecDir = dir
		api := local.Components["api"]
		api.Expose = &spec.Expose{Domain: "public"}
		local.Components["api"] = api
		rt, err := spec.ResolveTask(local, nil, spec.NormalizeEnv("dev"), "migrate")
		require.NoError(t, err)
		assert.Equal(t, "postgres://db", rt.Runtime.ExplicitValues["DATABASE_URL"])
		assert.Equal(t, "eu", rt.Runtime.FileValues["REGION"])
	})

	t.Run("platform resolve merges and skips by environment", func(t *testing.T) {
		t.Parallel()
		platform := &spec.PlatformConfig{
			APIVersion: "platform/v1-alpha.3",
			Environments: map[string]spec.PlatformEnvironment{
				"dev":  {Context: "kind"},
				"prod": {Context: "kind"},
			},
		}
		rt, err := spec.ResolveTask(m, platform, spec.NormalizeEnv("dev"), "migrate")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)

		_, err = spec.ResolveTask(m, platform, spec.NormalizeEnv("dev"), "backfill")
		require.Error(t, err)
		assert.ErrorContains(t, err, "skipped")

		_, err = spec.ResolveTask(m, platform, spec.NormalizeEnv("dev"), "missing")
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown task")
	})

	t.Run("runtime-only manual task without platform", func(t *testing.T) {
		t.Parallel()
		local := &spec.Spec{
			APIVersion: spec.CurrentManifestVersion,
			Project:    "shop",
			Components: map[string]spec.Component{
				"api": {Image: "busybox"},
			},
			Tasks: map[string]spec.Task{
				"migrate": {
					From:    "api",
					On:      spec.TaskOnManual,
					Command: []string{"echo", "ok"},
				},
			},
		}
		rt, err := spec.ResolveTask(local, nil, spec.NormalizeEnv("dev"), "migrate")
		require.NoError(t, err)
		assert.Equal(t, "busybox", rt.Task.Image)
		assert.Equal(t, []string{"echo", "ok"}, rt.Task.Command)
		job, err := k8s.BuildTaskJob(k8s.TaskJobOptions{
			Project:     "shop",
			Environment: "dev",
			Namespace:   "default",
			TaskName:    "migrate",
			Task:        rt.Task,
			Runtime:     rt.Runtime,
		})
		require.NoError(t, err)
		assert.Equal(t, "busybox", job.Spec.Template.Spec.Containers[0].Image)
	})
}

func TestResolveTask_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		task     string
		profiles []string
		want     string
	}{
		{name: "unknown task", task: "missing", want: "unknown task"},
		{name: "skipped in this environment", task: "backfill", want: "skipped"},
		{name: "inherited profiles require a platform file", task: "migrate", profiles: []string{"batch"}, want: "no platform file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := testManifest()
			api := m.Components["api"]
			api.Profiles = tt.profiles
			m.Components["api"] = api
			_, err := spec.ResolveTask(m, nil, spec.NormalizeEnv("dev"), tt.task)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRunTask_FlagValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "negative count",
			args: []string{"run", "migrate", "dev", "--count", "-1"},
			want: "zero or positive",
		},
		{
			name: "parallelism above indexed job limit",
			args: []string{"run", "migrate", "dev", "--parallelism", "100001"},
			want: "at most",
		},
		{
			name: "parallelism greater than count",
			args: []string{"run", "migrate", "dev", "--count", "2", "--parallelism", "3"},
			want: "less than or equal to --count",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			io, _, _, _ := nabattest.NewIO()
			app := nabat.MustNew("deployah", nabat.WithIO(io))
			Register(app)
			err := nabattest.Run(t, app, tt.args)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestExecuteRun_DetachAndWait(t *testing.T) {
	t.Parallel()

	opts := k8s.TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "backfill",
		Task:        spec.Task{Image: "busybox:1.36", Command: []string{"true"}},
	}

	t.Run("detach returns after create", func(t *testing.T) {
		t.Parallel()
		cs := fake.NewSimpleClientset()
		job := mustBuildJob(t, opts, "shop-dev-backfill-detach")
		c := nabatContext(t)
		require.NoError(t, executeRun(c, cs, time.Minute, job, nil, true))
		got, err := cs.BatchV1().Jobs("default").Get(t.Context(), "shop-dev-backfill-detach", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "shop-dev-backfill-detach", got.Name)
	})

	t.Run("wait succeeds when the job completes", func(t *testing.T) {
		t.Parallel()
		cs := fake.NewSimpleClientset()
		jobGetWithStatus(cs, func(job *batchv1.Job) {
			job.Status.Succeeded = 1
		})
		job := mustBuildJob(t, opts, "shop-dev-backfill-wait")
		c := nabatContext(t)
		require.NoError(t, executeRun(c, cs, time.Minute, job, nil, false))
	})

	t.Run("wait fails when the job fails", func(t *testing.T) {
		t.Parallel()
		cs := fake.NewSimpleClientset()
		jobGetWithStatus(cs, func(job *batchv1.Job) {
			job.Status.Conditions = []batchv1.JobCondition{{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Message: "backoff limit exceeded",
			}}
		})
		job := mustBuildJob(t, opts, "shop-dev-backfill-fail")
		c := nabatContext(t)
		err := executeRun(c, cs, time.Minute, job, nil, false)
		require.Error(t, err)
		assert.ErrorContains(t, err, "backoff limit exceeded")
	})

	t.Run("create error", func(t *testing.T) {
		t.Parallel()
		cs := fake.NewSimpleClientset()
		cs.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("quota exceeded")
		})
		job := mustBuildJob(t, opts, "shop-dev-backfill-create")
		c := nabatContext(t)
		err := executeRun(c, cs, time.Minute, job, nil, true)
		require.Error(t, err)
		assert.ErrorContains(t, err, "quota exceeded")
	})
}

func mustBuildJob(t *testing.T, opts k8s.TaskJobOptions, name string) *batchv1.Job {
	t.Helper()
	job, err := k8s.BuildTaskJob(opts)
	require.NoError(t, err)
	job.Name = name
	job.GenerateName = ""
	return job
}

func jobGetWithStatus(cs *fake.Clientset, mutate func(*batchv1.Job)) {
	cs.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		get, ok := action.(k8stesting.GetAction)
		if !ok {
			return false, nil, nil
		}
		obj, err := cs.Tracker().Get(batchv1.SchemeGroupVersion.WithResource("jobs"), get.GetNamespace(), get.GetName())
		if err != nil {
			return true, nil, err
		}
		job, ok := obj.(*batchv1.Job)
		if !ok {
			return true, nil, fmt.Errorf("unexpected job object %T", obj)
		}
		job = job.DeepCopy()
		mutate(job)
		return true, job, nil
	})
}

func nabatContext(t *testing.T) *nabat.Context {
	t.Helper()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	return nabattest.Context(t, app)
}
