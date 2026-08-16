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
	"fmt"
	"maps"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"deployah.dev/deployah/internal/spec"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	jobGenerateNameMax = 57
	// jobNotFoundLimit is how many consecutive Get NotFound results to
	// retry before treating the Job as gone. Covers brief apiserver lag
	// after Create without spinning until ctx times out if the Job was
	// deleted.
	jobNotFoundLimit = 5
)

// TaskJobOptions controls a CLI-created Job.
type TaskJobOptions struct {
	Project     string
	Environment string
	Namespace   string
	TaskName    string
	Task        spec.Task
	Count       int
	Parallelism int
	Profile     *spec.PlatformProfile
}

// BuildTaskJob builds an Indexed batch/v1 Job for a CLI run. The name is
// left empty; GenerateName is set so concurrent runs do not collide.
// serviceAccountName is omitted so the pod uses the namespace default
// ServiceAccount.
func BuildTaskJob(opts TaskJobOptions) (*batchv1.Job, error) {
	fields, err := spec.NewTaskJobSpec(opts.Task, opts.Count, opts.Parallelism)
	if err != nil {
		return nil, err
	}

	envName := spec.NormalizeEnv(opts.Environment).K8sSafe
	release := opts.Project + "-" + envName

	ttl := int32(spec.DefaultCLIJobTTLSeconds)
	if fields.TTLSecondsAfterFinished != nil {
		ttl = *fields.TTLSecondsAfterFinished
	}

	envVars := make([]corev1.EnvVar, 0, len(fields.Env))
	for _, k := range slices.Sorted(maps.Keys(fields.Env)) {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: fields.Env[k]})
	}

	container := corev1.Container{
		Name:  opts.TaskName,
		Image: fields.Image,
		Env:   envVars,
	}
	if len(fields.Command) > 0 {
		container.Command = fields.Command
	}
	if len(fields.Args) > 0 {
		container.Args = fields.Args
	}
	if req := resourceList(fields.Resources); len(req) > 0 {
		container.Resources.Requests = req
	}

	podLabels := map[string]string{
		spec.LabelProject:     opts.Project,
		spec.LabelComponent:   opts.TaskName,
		spec.LabelEnvironment: envName,
		spec.LabelManagedBy:   spec.ManagedByValue,
	}
	var podAnnotations map[string]string
	podSpec := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyOnFailure,
		AutomountServiceAccountToken: new(false),
		Containers:                   []corev1.Container{container},
	}
	applyProfileToPod(&podSpec, podLabels, &podAnnotations, opts.Profile)

	job := &batchv1.Job{
		GenerateName: jobGenerateName(release, opts.TaskName),
		Namespace:    opts.Namespace,
		Labels: map[string]string{
			spec.LabelProject:     opts.Project,
			spec.LabelComponent:   opts.TaskName,
			spec.LabelEnvironment: envName,
			spec.LabelManagedBy:   spec.ManagedByValue,
		},
		Annotations: map[string]string{
			spec.AnnotationSource:  spec.SourceSpec,
			spec.AnnotationProject: opts.Project,
		},
		Spec: batchv1.JobSpec{
			CompletionMode:          new(batchv1.IndexedCompletion),
			Completions:             new(fields.Completions),
			Parallelism:             new(fields.Parallelism),
			BackoffLimit:            new(fields.BackoffLimit),
			TTLSecondsAfterFinished: new(ttl),
			ActiveDeadlineSeconds:   fields.ActiveDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		},
	}
	return job, nil
}

func applyProfileToPod(pod *corev1.PodSpec, labels map[string]string, annotations *map[string]string, profile *spec.PlatformProfile) {
	if profile == nil {
		return
	}
	if len(profile.NodeSelector) > 0 {
		pod.NodeSelector = maps.Clone(profile.NodeSelector)
	}
	if len(profile.Tolerations) > 0 {
		pod.Tolerations = slices.Clone(profile.Tolerations)
	}
	if len(profile.PodLabels) > 0 {
		maps.Copy(labels, profile.PodLabels)
	}
	if len(profile.PodAnnotations) > 0 {
		*annotations = maps.Clone(profile.PodAnnotations)
	}
	if profile.SecurityContext != nil {
		pod.SecurityContext = profile.SecurityContext.DeepCopy()
	}
	if profile.ContainerSecurityContext != nil && len(pod.Containers) > 0 {
		pod.Containers[0].SecurityContext = profile.ContainerSecurityContext.DeepCopy()
	}
}

func jobGenerateName(release, task string) string {
	prefix := release + "-" + task + "-"
	if len(prefix) > jobGenerateNameMax {
		prefix = prefix[:jobGenerateNameMax]
	}
	return prefix
}

func resourceList(res spec.Resources) corev1.ResourceList {
	out := corev1.ResourceList{}
	if res.CPU != nil && !res.CPU.IsZero() {
		out[corev1.ResourceCPU] = *res.CPU
	}
	if res.Memory != nil && !res.Memory.IsZero() {
		out[corev1.ResourceMemory] = *res.Memory
	}
	if res.EphemeralStorage != nil && !res.EphemeralStorage.IsZero() {
		out[corev1.ResourceEphemeralStorage] = *res.EphemeralStorage
	}
	return out
}

// CreateTaskJob creates the Job and returns the server copy (with Name).
func CreateTaskJob(ctx context.Context, cs kubernetes.Interface, job *batchv1.Job) (*batchv1.Job, error) {
	created, err := cs.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	return created, nil
}

// WaitForJob waits until the Job succeeds or fails. ctx should already
// carry the session timeout. Get errors that are not a permanent API
// status (Forbidden, Unauthorized, Invalid, BadRequest) are retried
// until ctx is done. Consecutive NotFound results are retried a few
// times, then treated as a permanent miss (the Job was deleted or never
// became visible). Every failure is wrapped as "wait for job <name>".
func WaitForJob(ctx context.Context, cs kubernetes.Interface, namespace, name string) error {
	notFound := 0
	err := wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		job, err := cs.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if ctx.Err() != nil {
				return false, err
			}
			if apierrors.IsNotFound(err) {
				notFound++
				if notFound >= jobNotFoundLimit {
					return false, err
				}
				return false, nil
			}
			notFound = 0
			if isPermanentJobGet(err) {
				return false, err
			}
			return false, nil
		}
		notFound = 0
		want := int32(1)
		if job.Spec.Completions != nil {
			want = *job.Spec.Completions
		}
		if job.Status.Succeeded >= want {
			return true, nil
		}
		for _, cond := range job.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				msg := cond.Message
				if msg == "" {
					msg = cond.Reason
				}
				return false, fmt.Errorf("job %s failed: %s", name, msg)
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait for job %s: %w", name, err)
	}
	return nil
}

func isPermanentJobGet(err error) bool {
	return apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) ||
		apierrors.IsInvalid(err) ||
		apierrors.IsBadRequest(err)
}

// ListJobs returns Jobs labeled with project and environment.
func ListJobs(ctx context.Context, cs kubernetes.Interface, namespace, project, environment string) ([]batchv1.Job, error) {
	selector, err := BuildLabelSelector(project, environment)
	if err != nil {
		return nil, fmt.Errorf("build job selector: %w", err)
	}
	list, err := cs.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return list.Items, nil
}

// DeleteJobs deletes Jobs labeled with project and environment.
// A NotFound result is ignored (the Job is already gone). Other delete
// errors are collected with [errors.Join] so one failure does not skip
// the rest.
func DeleteJobs(ctx context.Context, cs kubernetes.Interface, namespace, project, environment string) error {
	jobs, err := ListJobs(ctx, cs, namespace, project, environment)
	if err != nil {
		return err
	}
	propagation := metav1.DeletePropagationBackground
	var errs []error
	for i := range jobs {
		job := &jobs[i]
		if delErr := cs.BatchV1().Jobs(namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		}); delErr != nil && !apierrors.IsNotFound(delErr) {
			errs = append(errs, fmt.Errorf("delete job %s: %w", job.Name, delErr))
		}
	}
	return errors.Join(errs...)
}
