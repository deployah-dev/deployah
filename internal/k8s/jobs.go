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
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
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
	// dns1123NameMax is the Kubernetes DNS-1123 label length limit.
	dns1123NameMax = 63
	// generateNameSuffixLen is the number of random characters Kubernetes
	// appends to GenerateName (k8s.io/apiserver/pkg/storage/names).
	generateNameSuffixLen = 5
	// generateNamePrefixMax is the longest GenerateName prefix that stays
	// within DNS-1123 after Kubernetes appends generateNameSuffixLen
	// characters (63 - 5 = 58).
	generateNamePrefixMax = dns1123NameMax - generateNameSuffixLen
	// dns1123HashReserved is the extra characters inserted when a name is
	// hashed: "-xxxx-" around a 4-hex-character digest.
	dns1123HashReserved = 6
	// jobNotFoundLimit is how many consecutive Get NotFound results to
	// retry before treating the Job as gone. Covers brief apiserver lag
	// after Create without spinning until ctx times out if the Job was
	// deleted.
	jobNotFoundLimit = 5
	// runJobCleanupTimeout bounds best-effort rollback after a partial
	// CreateRunJob. The cleanup context is detached from caller
	// cancellation so Ctrl+C or a command timeout can still delete the
	// temporary ConfigMap (and Job, when one was created).
	runJobCleanupTimeout = 5 * time.Second
)

// TaskJobOptions controls a CLI-created Job.
type TaskJobOptions struct {
	Project     string
	Environment string
	Namespace   string
	TaskName    string
	Task        spec.Task
	Runtime     spec.ResolvedRuntimeEnvironment
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

	env := spec.NormalizeEnv(opts.Environment)
	release := env.ReleaseName(opts.Project)

	ttl := int32(spec.DefaultCLIJobTTLSeconds)
	if fields.TTLSecondsAfterFinished != nil {
		ttl = *fields.TTLSecondsAfterFinished
	}

	explicit := maps.Clone(opts.Runtime.ExplicitValues)
	envVars := make([]corev1.EnvVar, 0, len(explicit))
	for _, k := range slices.Sorted(maps.Keys(explicit)) {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: explicit[k]})
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
		spec.LabelEnvironment: env.MapKey,
		spec.LabelManagedBy:   spec.ManagedByValue,
		InstanceLabel:         release,
	}
	var podAnnotations map[string]string
	podSpec := corev1.PodSpec{
		RestartPolicy:                corev1.RestartPolicyOnFailure,
		AutomountServiceAccountToken: new(false),
		Containers:                   []corev1.Container{container},
	}
	applyProfileToPod(&podSpec, podLabels, &podAnnotations, opts.Profile)

	job := &batchv1.Job{
		GenerateName: generateNamePrefix(release, opts.TaskName),
		Namespace:    opts.Namespace,
		Labels: map[string]string{
			spec.LabelProject:     opts.Project,
			spec.LabelComponent:   opts.TaskName,
			spec.LabelEnvironment: env.MapKey,
			spec.LabelManagedBy:   spec.ManagedByValue,
			InstanceLabel:         release,
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

func runConfigMapGenerateName(jobPrefix string) string {
	base := strings.TrimSuffix(jobPrefix, "-")
	if base == "" {
		base = "deployah"
	}
	return generateNamePrefix(base, "run")
}

// generateNamePrefix returns a GenerateName prefix of at most
// [generateNamePrefixMax] characters that ends with "-". It uses the same
// hash-or-trim rule as the Helm deployah.dns1123Name helper, leaving room
// for Kubernetes to append [generateNameSuffixLen] random characters.
func generateNamePrefix(base, keep string) string {
	return dns1123PrefixedName(base, keep, generateNamePrefixMax-1) + "-"
}

// dns1123PrefixedName returns base-keep when that fits in limit, otherwise
// {truncatedBase}-{4hex}-{keep}. The hash is the first four hex characters
// of sha256(base-keep) so two long names that share a prefix cannot collide.
// This matches deployah.dns1123Name in the Helm chart helpers.
func dns1123PrefixedName(base, keep string, limit int) string {
	full := base + "-" + keep
	if len(full) <= limit {
		return full
	}
	budget := max(1, limit-(len(keep)+dns1123HashReserved))
	prefix := base
	if len(prefix) > budget {
		prefix = prefix[:budget]
	}
	prefix = strings.TrimSuffix(prefix, "-")
	sum := sha256.Sum256([]byte(full))
	return prefix + "-" + fmt.Sprintf("%04x", sum[:2]) + "-" + keep
}

// applyJobEnvFrom sets the job container envFrom to configMapName.
func applyJobEnvFrom(job *batchv1.Job, configMapName string) {
	if job == nil || configMapName == "" || len(job.Spec.Template.Spec.Containers) == 0 {
		return
	}
	job.Spec.Template.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{
		ConfigMapRef: &corev1.ConfigMapEnvSource{
			Name: configMapName,
		},
	}}
}

func runEnvConfigMap(job *batchv1.Job, fileValues map[string]string) *corev1.ConfigMap {
	data := make(map[string]string, len(fileValues))
	for _, k := range slices.Sorted(maps.Keys(fileValues)) {
		data[k] = fileValues[k]
	}
	prefix := job.GenerateName
	if prefix == "" && job.Name != "" {
		prefix = job.Name + "-"
	}
	return &corev1.ConfigMap{
		GenerateName: runConfigMapGenerateName(prefix),
		Namespace:    job.Namespace,
		Labels:       maps.Clone(job.Labels),
		Annotations:  maps.Clone(job.Annotations),
		Data:         data,
	}
}

// CreateRunJob creates a CLI run Job. When fileValues is non-empty it
// creates a GenerateName ConfigMap first, points the Job envFrom at the
// returned name, then sets a Job ownerReference on the ConfigMap.
// Detach is safe only after this function returns.
func CreateRunJob(ctx context.Context, cs kubernetes.Interface, job *batchv1.Job, fileValues map[string]string) (*batchv1.Job, error) {
	if len(fileValues) == 0 {
		return CreateTaskJob(ctx, cs, job)
	}

	cm, err := cs.CoreV1().ConfigMaps(job.Namespace).Create(ctx, runEnvConfigMap(job, fileValues), metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create run configmap: %w", err)
	}
	applyJobEnvFrom(job, cm.Name)

	created, err := CreateTaskJob(ctx, cs, job)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runJobCleanupTimeout)
		defer cancel()
		if delErr := deleteConfigMap(cleanupCtx, cs, cm); delErr != nil {
			return nil, fmt.Errorf("%w; also failed to delete configmap %s: %w", err, cm.Name, delErr)
		}
		return nil, err
	}

	if ownErr := setConfigMapJobOwner(ctx, cs, cm, created); ownErr != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runJobCleanupTimeout)
		defer cancel()
		jobDel := deleteJob(cleanupCtx, cs, created)
		cmDel := deleteConfigMap(cleanupCtx, cs, cm)
		switch {
		case jobDel != nil && cmDel != nil:
			return nil, fmt.Errorf("%w; also failed to delete job %s: %w; also failed to delete configmap %s: %w", ownErr, created.Name, jobDel, cm.Name, cmDel)
		case jobDel != nil:
			return nil, fmt.Errorf("%w; also failed to delete job %s: %w; leftover job %s", ownErr, created.Name, jobDel, created.Name)
		case cmDel != nil:
			return nil, fmt.Errorf("%w; also failed to delete configmap %s: %w; leftover configmap %s", ownErr, cm.Name, cmDel, cm.Name)
		default:
			return nil, ownErr
		}
	}
	return created, nil
}

func setConfigMapJobOwner(ctx context.Context, cs kubernetes.Interface, cm *corev1.ConfigMap, job *batchv1.Job) error {
	cm.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       job.Name,
		UID:        job.UID,
	}}
	_, err := cs.CoreV1().ConfigMaps(cm.Namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("set configmap owner: %w", err)
	}
	return nil
}

func deleteConfigMap(ctx context.Context, cs kubernetes.Interface, cm *corev1.ConfigMap) error {
	if err := cs.CoreV1().ConfigMaps(cm.Namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func deleteJob(ctx context.Context, cs kubernetes.Interface, job *batchv1.Job) error {
	propagation := metav1.DeletePropagationBackground
	if err := cs.BatchV1().Jobs(job.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
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

// ListJobs returns Jobs labeled with project, environment, and the Helm
// release instance. Logical names such as review match shop-review, not
// sibling review/pr-123 Jobs.
func ListJobs(ctx context.Context, cs kubernetes.Interface, namespace, project, environment string) ([]batchv1.Job, error) {
	selector, err := BuildLabelSelector(project, environment)
	if err != nil {
		return nil, fmt.Errorf("build job selector: %w", err)
	}
	selector, err = withReleaseInstance(selector, project, environment)
	if err != nil {
		return nil, err
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
