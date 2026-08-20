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

package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/spec"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stesting "k8s.io/client-go/testing"
)

func TestBuildTaskJob_IndexedAndLabels(t *testing.T) {
	t.Parallel()

	job, err := BuildTaskJob(TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "backfill",
		Task: spec.Task{
			Image:   "busybox:1.36",
			Command: []string{"backfill"},
			Env:     map[string]string{"MODE": "full"},
			Fanout:  spec.Fanout{Count: 4, Parallelism: 2},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "shop-dev-backfill-", job.GenerateName)
	assert.Empty(t, job.Name)
	require.NotNil(t, job.Spec.CompletionMode)
	assert.Equal(t, batchv1.IndexedCompletion, *job.Spec.CompletionMode)
	require.NotNil(t, job.Spec.Completions)
	assert.Equal(t, int32(4), *job.Spec.Completions)
	require.NotNil(t, job.Spec.Parallelism)
	assert.Equal(t, int32(2), *job.Spec.Parallelism)
	assert.Equal(t, "shop", job.Labels[spec.LabelProject])
	assert.Equal(t, "backfill", job.Labels[spec.LabelComponent])
	assert.Equal(t, "dev", job.Labels[spec.LabelEnvironment])
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "busybox:1.36", job.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, []string{"backfill"}, job.Spec.Template.Spec.Containers[0].Command)
	assert.Equal(t, []corev1.EnvVar{{Name: "MODE", Value: "full"}}, job.Spec.Template.Spec.Containers[0].Env)
	require.NotNil(t, job.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *job.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.Empty(t, job.Spec.Template.Spec.ServiceAccountName)
	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, int32(spec.DefaultCLIJobTTLSeconds), *job.Spec.TTLSecondsAfterFinished)
}

func TestBuildTaskJob_EnvSorted(t *testing.T) {
	t.Parallel()

	job, err := BuildTaskJob(TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "migrate",
		Task: spec.Task{
			Image:   "busybox:1.36",
			Command: []string{"true"},
			Env:     map[string]string{"LOG": "debug", "DATABASE_URL": "postgres://db", "A": "1"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []corev1.EnvVar{
		{Name: "A", Value: "1"},
		{Name: "DATABASE_URL", Value: "postgres://db"},
		{Name: "LOG", Value: "debug"},
	}, job.Spec.Template.Spec.Containers[0].Env)
}

func TestBuildTaskJob_FlagOverrides(t *testing.T) {
	t.Parallel()

	job, err := BuildTaskJob(TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "migrate",
		Task: spec.Task{
			Image:   "busybox:1.36",
			Command: []string{"true"},
			Fanout:  spec.Fanout{Count: 1, Parallelism: 1},
		},
		Count:       3,
		Parallelism: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, job.Spec.Completions)
	assert.Equal(t, int32(3), *job.Spec.Completions)
	require.NotNil(t, job.Spec.Parallelism)
	assert.Equal(t, int32(2), *job.Spec.Parallelism)
}

func TestJobGenerateName_Truncates(t *testing.T) {
	t.Parallel()

	got := jobGenerateName("shop-dev", "backfill")
	assert.Equal(t, "shop-dev-backfill-", got)

	long := jobGenerateName(strings.Repeat("a", 40), strings.Repeat("b", 40))
	assert.Equal(t, jobGenerateNameMax, len(long))
	assert.Equal(t, (strings.Repeat("a", 40) + "-" + strings.Repeat("b", 40) + "-")[:jobGenerateNameMax], long)
}

func TestBuildTaskJob_EphemeralStorageArgsAndTTL(t *testing.T) {
	t.Parallel()

	ttl := 30
	job, err := BuildTaskJob(TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "migrate",
		Task: spec.Task{
			Image:   "busybox:1.36",
			Command: []string{"migrate"},
			Args:    []string{"up"},
			Resources: spec.Resources{
				CPU:              spec.MustQuantity("100m"),
				Memory:           spec.MustQuantity("128Mi"),
				EphemeralStorage: spec.MustQuantity("1Gi"),
			},
			TTLSecondsAfterFinished: &ttl,
		},
	})
	require.NoError(t, err)
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []string{"up"}, job.Spec.Template.Spec.Containers[0].Args)
	req := job.Spec.Template.Spec.Containers[0].Resources.Requests
	assert.True(t, spec.MustQuantity("100m").Equal(req[corev1.ResourceCPU]))
	assert.True(t, spec.MustQuantity("128Mi").Equal(req[corev1.ResourceMemory]))
	assert.True(t, spec.MustQuantity("1Gi").Equal(req[corev1.ResourceEphemeralStorage]))
	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, int32(30), *job.Spec.TTLSecondsAfterFinished)
}

func TestCreateTaskJob_Error(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("quota exceeded")
	})
	_, err := CreateTaskJob(t.Context(), cs, namedJob(t, TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "backfill",
		Task:        spec.Task{Image: "busybox:1.36", Command: []string{"true"}},
	}, "shop-dev-backfill-create"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quota exceeded")
}

func TestCreateTaskJob_UniqueNames(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	opts := TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "backfill",
		Task:        spec.Task{Image: "busybox:1.36", Command: []string{"true"}},
	}

	first, err := CreateTaskJob(t.Context(), cs, namedJob(t, opts, "shop-dev-backfill-a"))
	require.NoError(t, err)
	second, err := CreateTaskJob(t.Context(), cs, namedJob(t, opts, "shop-dev-backfill-b"))
	require.NoError(t, err)
	assert.NotEqual(t, first.Name, second.Name)
}

func namedJob(t *testing.T, opts TaskJobOptions, name string) *batchv1.Job {
	t.Helper()
	job, err := BuildTaskJob(opts)
	require.NoError(t, err)
	job.Name = name
	job.GenerateName = ""
	return job
}

func TestWaitForJob_SuccessAndFailure(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when completions are met", func(t *testing.T) {
		t.Parallel()
		job := &batchv1.Job{
			Name: "ok", Namespace: "default",
			Spec:   batchv1.JobSpec{Completions: new(int32(1))},
			Status: batchv1.JobStatus{Succeeded: 1},
		}
		cs := fake.NewSimpleClientset(job)
		require.NoError(t, WaitForJob(t.Context(), cs, "default", "ok"))
	})

	t.Run("fails when the job condition is Failed", func(t *testing.T) {
		t.Parallel()
		job := &batchv1.Job{
			Name: "bad", Namespace: "default",
			Spec: batchv1.JobSpec{Completions: new(int32(1))},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{
					Type:    batchv1.JobFailed,
					Status:  "True",
					Message: "backoff limit exceeded",
				}},
			},
		}
		cs := fake.NewSimpleClientset(job)
		err := WaitForJob(t.Context(), cs, "default", "bad")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backoff limit exceeded")
	})

	t.Run("failed condition without message uses reason", func(t *testing.T) {
		t.Parallel()
		job := &batchv1.Job{
			Name: "bad", Namespace: "default",
			Spec: batchv1.JobSpec{Completions: new(int32(1))},
			Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{
					Type:   batchv1.JobFailed,
					Status: "True",
					Reason: "BackoffLimitExceeded",
				}},
			},
		}
		cs := fake.NewSimpleClientset(job)
		err := WaitForJob(t.Context(), cs, "default", "bad")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "BackoffLimitExceeded")
	})

	t.Run("nil completions treats one success as done", func(t *testing.T) {
		t.Parallel()
		job := &batchv1.Job{
			Name: "ok", Namespace: "default",
			Status: batchv1.JobStatus{Succeeded: 1},
		}
		cs := fake.NewSimpleClientset(job)
		require.NoError(t, WaitForJob(t.Context(), cs, "default", "ok"))
	})
}

func TestWaitForJob_RetriesTransientGet(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		job := &batchv1.Job{
			Name: "ok", Namespace: "default",
			Spec:   batchv1.JobSpec{Completions: new(int32(1))},
			Status: batchv1.JobStatus{Succeeded: 1},
		}
		cs := fake.NewSimpleClientset(job)
		gets := 0
		cs.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
			gets++
			if gets == 1 {
				return true, nil, apierrors.NewTooManyRequests("slow down", 1)
			}
			return false, nil, nil
		})
		require.NoError(t, WaitForJob(t.Context(), cs, "default", "ok"))
		assert.GreaterOrEqual(t, gets, 2)
	})
}

func TestWaitForJob_RetriesBriefNotFound(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		job := &batchv1.Job{
			Name: "ok", Namespace: "default",
			Spec:   batchv1.JobSpec{Completions: new(int32(1))},
			Status: batchv1.JobStatus{Succeeded: 1},
		}
		cs := fake.NewSimpleClientset(job)
		gets := 0
		cs.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
			gets++
			if gets == 1 {
				return true, nil, apierrors.NewNotFound(
					schema.GroupResource{Group: "batch", Resource: "jobs"},
					"ok",
				)
			}
			return false, nil, nil
		})
		require.NoError(t, WaitForJob(t.Context(), cs, "default", "ok"))
		assert.GreaterOrEqual(t, gets, 2)
	})
}

func TestWaitForJob_GivesUpAfterConsecutiveNotFound(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		cs := fake.NewSimpleClientset()
		err := WaitForJob(t.Context(), cs, "default", "gone")
		require.Error(t, err)
		assert.ErrorContains(t, err, "wait for job")
		assert.True(t, apierrors.IsNotFound(err))
	})
}

func TestWaitForJob_PermanentGetError(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "batch", Resource: "jobs"},
			"denied",
			errors.New("denied"),
		)
	})
	err := WaitForJob(t.Context(), cs, "default", "denied")
	require.Error(t, err)
	assert.ErrorContains(t, err, "wait for job")
}

func TestWaitForJob_RetriesTransportError(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		job := &batchv1.Job{
			Name: "ok", Namespace: "default",
			Spec:   batchv1.JobSpec{Completions: new(int32(1))},
			Status: batchv1.JobStatus{Succeeded: 1},
		}
		cs := fake.NewSimpleClientset(job)
		gets := 0
		cs.PrependReactor("get", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
			gets++
			if gets == 1 {
				return true, nil, errors.New("connection reset")
			}
			return false, nil, nil
		})
		require.NoError(t, WaitForJob(t.Context(), cs, "default", "ok"))
		assert.GreaterOrEqual(t, gets, 2)
	})
}

func TestWaitForJob_TimeoutNamesJob(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		job := &batchv1.Job{
			Name: "slow", Namespace: "default",
			Spec: batchv1.JobSpec{Completions: new(int32(1))},
		}
		cs := fake.NewSimpleClientset(job)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		err := WaitForJob(ctx, cs, "default", "slow")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorContains(t, err, "wait for job slow")
	})
}

func TestListJobs(t *testing.T) {
	t.Parallel()

	keep := &batchv1.Job{
		Name:      "other",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "other", spec.LabelEnvironment: "dev"},
	}
	match := &batchv1.Job{
		Name:      "shop-job",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	cs := fake.NewSimpleClientset(keep, match)
	got, err := ListJobs(t.Context(), cs, "default", "shop", "dev")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "shop-job", got[0].Name)
}

func TestListJobs_Error(t *testing.T) {
	t.Parallel()

	cs := fake.NewSimpleClientset()
	cs.PrependReactor("list", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver timeout")
	})
	_, err := ListJobs(t.Context(), cs, "default", "shop", "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiserver timeout")

	err = DeleteJobs(t.Context(), cs, "default", "shop", "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiserver timeout")
}

func TestDeleteJobs(t *testing.T) {
	t.Parallel()

	keep := &batchv1.Job{
		Name:      "other",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "other", spec.LabelEnvironment: "dev"},
	}
	drop := &batchv1.Job{
		Name:      "shop-job",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	cs := fake.NewSimpleClientset(keep, drop)
	require.NoError(t, DeleteJobs(t.Context(), cs, "default", "shop", "dev"))

	list, err := cs.BatchV1().Jobs("default").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "other", list.Items[0].Name)
}

func TestDeleteJobs_IgnoresNotFound(t *testing.T) {
	t.Parallel()

	gone := &batchv1.Job{
		Name:      "shop-gone",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	stay := &batchv1.Job{
		Name:      "shop-stay",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	cs := fake.NewSimpleClientset(gone, stay)
	cs.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		del, ok := action.(k8stesting.DeleteAction)
		if !ok || del.GetName() != "shop-gone" {
			return false, nil, nil
		}
		return true, nil, apierrors.NewNotFound(
			schema.GroupResource{Group: "batch", Resource: "jobs"},
			"shop-gone",
		)
	})
	require.NoError(t, DeleteJobs(t.Context(), cs, "default", "shop", "dev"))

	list, err := cs.BatchV1().Jobs("default").List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, "shop-gone", list.Items[0].Name)
}

func TestDeleteJobs_JoinsDeleteErrors(t *testing.T) {
	t.Parallel()

	first := &batchv1.Job{
		Name:      "shop-a",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	second := &batchv1.Job{
		Name:      "shop-b",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	cs := fake.NewSimpleClientset(first, second)
	cs.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		del, ok := action.(k8stesting.DeleteAction)
		if !ok {
			return false, nil, nil
		}
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "batch", Resource: "jobs"},
			del.GetName(),
			errors.New("denied"),
		)
	})
	err := DeleteJobs(t.Context(), cs, "default", "shop", "dev")
	require.Error(t, err)
	assert.ErrorContains(t, err, "shop-a")
	assert.ErrorContains(t, err, "shop-b")
}

func TestDeleteJobs_BackgroundPropagation(t *testing.T) {
	t.Parallel()

	job := &batchv1.Job{
		Name:      "shop-job",
		Namespace: "default",
		Labels:    map[string]string{spec.LabelProject: "shop", spec.LabelEnvironment: "dev"},
	}
	cs := fake.NewSimpleClientset(job)
	var got *metav1.DeletionPropagation
	cs.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		del, ok := action.(k8stesting.DeleteAction)
		if !ok {
			return false, nil, nil
		}
		opts := del.GetDeleteOptions()
		got = opts.PropagationPolicy
		return false, nil, nil
	})
	require.NoError(t, DeleteJobs(t.Context(), cs, "default", "shop", "dev"))
	require.NotNil(t, got)
	assert.Equal(t, metav1.DeletePropagationBackground, *got)
}

func TestBuildTaskJob_InvalidTimeout(t *testing.T) {
	t.Parallel()

	_, err := BuildTaskJob(TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "migrate",
		Task: spec.Task{
			Image:   "busybox:1.36",
			Command: []string{"true"},
			Timeout: "not-a-duration",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestBuildTaskJob_AppliesProfile(t *testing.T) {
	t.Parallel()

	job, err := BuildTaskJob(TaskJobOptions{
		Project:     "shop",
		Environment: "dev",
		Namespace:   "default",
		TaskName:    "migrate",
		Task:        spec.Task{Image: "busybox:1.36", Command: []string{"true"}},
		Profile: &spec.PlatformProfile{
			NodeSelector:   map[string]string{"workload": "batch"},
			PodLabels:      map[string]string{"tier": "jobs"},
			PodAnnotations: map[string]string{"team": "platform"},
			Tolerations: []corev1.Toleration{
				{Key: "batch", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
			SecurityContext:          &corev1.PodSecurityContext{RunAsNonRoot: new(true)},
			ContainerSecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: new(true)},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"workload": "batch"}, job.Spec.Template.Spec.NodeSelector)
	assert.Equal(t, "jobs", job.Spec.Template.Labels["tier"])
	assert.Equal(t, "platform", job.Spec.Template.Annotations["team"])
	require.Len(t, job.Spec.Template.Spec.Tolerations, 1)
	assert.Equal(t, "batch", job.Spec.Template.Spec.Tolerations[0].Key)
	require.NotNil(t, job.Spec.Template.Spec.SecurityContext)
	require.NotNil(t, job.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	assert.True(t, *job.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	require.NotNil(t, job.Spec.Template.Spec.Containers[0].SecurityContext)
	require.NotNil(t, job.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem)
	assert.True(t, *job.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem)
}
