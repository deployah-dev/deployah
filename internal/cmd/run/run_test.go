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
			"api": {Image: "ghcr.io/acme/shop:1.2.3", Env: map[string]string{"DATABASE_URL": "postgres://db"}},
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

func TestResolveRunTask(t *testing.T) {
	t.Parallel()

	m := testManifest()

	t.Run("merges from parent", func(t *testing.T) {
		t.Parallel()
		rt, err := resolveRunTask(m, nil, "dev", "migrate")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)
		assert.Equal(t, "postgres://db", rt.Task.Env["DATABASE_URL"])
		assert.Equal(t, []string{"migrate", "up"}, rt.Task.Command)
	})

	t.Run("scheduled task is runnable", func(t *testing.T) {
		t.Parallel()
		rt, err := resolveRunTask(m, nil, "dev", "cleanup")
		require.NoError(t, err)
		assert.Equal(t, spec.TaskOnSchedule, rt.Task.On)
		assert.Equal(t, "0 3 * * *", rt.Task.Schedule)
		assert.Empty(t, rt.Task.Timeout)
	})

	t.Run("unknown task", func(t *testing.T) {
		t.Parallel()
		_, err := resolveRunTask(m, nil, "dev", "missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown task")
	})

	t.Run("skipped in this environment", func(t *testing.T) {
		t.Parallel()
		_, err := resolveRunTask(m, nil, "dev", "backfill")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "skipped")
	})

	t.Run("empty inherited profiles do not require a platform file", func(t *testing.T) {
		t.Parallel()
		local := testManifest()
		api := local.Components["api"]
		api.Profiles = []string{}
		local.Components["api"] = api
		rt, err := resolveRunTask(local, nil, "dev", "migrate")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)
	})

	t.Run("inherited profiles require a platform file", func(t *testing.T) {
		t.Parallel()
		local := testManifest()
		api := local.Components["api"]
		api.Profiles = []string{"batch"}
		local.Components["api"] = api
		_, err := resolveRunTask(local, nil, "dev", "migrate")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no platform file")
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
		rt, err := resolveRunTask(m, platform, "dev", "migrate")
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)

		_, err = resolveRunTask(m, platform, "dev", "backfill")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "skipped")

		_, err = resolveRunTask(m, platform, "dev", "missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown task")
	})
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
			assert.Contains(t, err.Error(), tt.want)
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
		require.NoError(t, executeRun(c, cs, time.Minute, job, true))
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
		require.NoError(t, executeRun(c, cs, time.Minute, job, false))
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
		err := executeRun(c, cs, time.Minute, job, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backoff limit exceeded")
	})

	t.Run("create error", func(t *testing.T) {
		t.Parallel()
		cs := fake.NewSimpleClientset()
		cs.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("quota exceeded")
		})
		job := mustBuildJob(t, opts, "shop-dev-backfill-create")
		c := nabatContext(t)
		err := executeRun(c, cs, time.Minute, job, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "quota exceeded")
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
