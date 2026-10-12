// Copyright 2026 The Deployah Authors
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

//go:build e2e

package e2e_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"deployah.dev/deployah/internal/testing/e2e"

	planCmd "deployah.dev/deployah/internal/cmd/plan"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// releaseTarget is one Helm release the scenario cleanup uninstalls.
type releaseTarget struct {
	Project string
	Env     string
}

func execCommand(t *testing.T) e2e.ExecFunc {
	t.Helper()
	return func(ctx context.Context, dir string, args []string) e2e.CommandResult {
		stdout, stderr, err := runInErrContext(t, ctx, dir, args...)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return e2e.CommandResult{Stdout: stdout, Stderr: stderr, Interrupted: true}
		}
		code := 0
		if err != nil {
			code = 1
			if errors.Is(err, planCmd.ErrChangesPresent) {
				code = 2
			}
		}
		retryable := len(args) > 0 && args[0] == "logs" && code != 0 && isRetryableAPIError(err)
		return e2e.CommandResult{Stdout: stdout, Stderr: stderr, ExitCode: code, Retryable: retryable}
	}
}

func (env *e2eEnv) claimNamespace(ctx context.Context, name string) (e2e.Claim, error) {
	runID := rand.Text()
	ns := &corev1.Namespace{}
	ns.Name = name
	ns.Annotations = map[string]string{e2e.RunAnnotation: runID}
	res, err := env.newResources("")
	if err != nil {
		return e2e.Claim{}, err
	}
	createErr := res.Create(ctx, ns)
	class := classifyCreate(createErr)
	if class == e2e.CreateRejected {
		return e2e.Claim{}, fmt.Errorf("create namespace %s: %w", name, createErr)
	}
	var got *e2e.NamespaceView
	var getErr error
	if class == e2e.CreateSucceeded && ns.UID != "" {
		run := ""
		if ns.Annotations != nil {
			run = ns.Annotations[e2e.RunAnnotation]
		}
		got = &e2e.NamespaceView{Name: ns.Name, UID: string(ns.UID), Run: run}
	}
	for attempt := range e2e.NamespaceReconcileAttempts {
		if attempt > 0 || got == nil {
			got, getErr = env.namespaceView(ctx, name)
		}
		decision := e2e.ResolveNamespaceCreate(runID, name, class, got, getErr)
		if decision.Owned {
			return decision.Claim, nil
		}
		if !decision.Retry {
			if decision.Err != nil {
				return e2e.Claim{}, decision.Err
			}
			return e2e.Claim{}, fmt.Errorf("namespace %s: not owned", name)
		}
		if attempt == e2e.NamespaceReconcileAttempts-1 {
			return e2e.Claim{}, e2e.FinishUncertain(name, createErr, getErr)
		}
		select {
		case <-ctx.Done():
			return e2e.Claim{}, e2e.FinishUncertain(name, createErr, ctx.Err())
		case <-time.After(e2e.NamespaceReconcileInterval):
		}
	}
	return e2e.Claim{}, e2e.FinishUncertain(name, createErr, getErr)
}

func classifyCreate(err error) e2e.CreateClass {
	switch {
	case err == nil:
		return e2e.CreateSucceeded
	case apierrors.IsAlreadyExists(err):
		return e2e.CreateAlreadyExists
	case apierrors.IsInvalid(err), apierrors.IsForbidden(err), apierrors.IsBadRequest(err):
		return e2e.CreateRejected
	default:
		return e2e.CreateAmbiguous
	}
}

func (env *e2eEnv) namespaceView(ctx context.Context, name string) (*e2e.NamespaceView, error) {
	res, err := env.newResources("")
	if err != nil {
		return nil, err
	}
	ns := &corev1.Namespace{}
	if err = res.Get(ctx, name, "", ns); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, e2e.ErrNotFound
		}
		return nil, err
	}
	return &e2e.NamespaceView{
		Name: ns.Name,
		UID:  string(ns.UID),
		Run:  ns.Annotations[e2e.RunAnnotation],
	}, nil
}

func (env *e2eEnv) cleanupClaim(t *testing.T, dir string, claim e2e.Claim, releases []releaseTarget) error {
	t.Helper()
	phases := make([]e2e.CleanupPhase, 0, len(releases)+3)
	for _, rel := range releases {
		phases = append(phases, e2e.CleanupPhase{
			Name: "delete " + rel.Project + "/" + rel.Env,
			Run:  env.gated(claim, env.deleteRelease(t, dir, claim.Name, rel)),
		})
	}
	phases = append(phases,
		e2e.CleanupPhase{Name: "workloads", Run: env.gated(claim, func(ctx context.Context) error {
			return env.deleteWorkloads(ctx, claim.Name)
		})},
		e2e.CleanupPhase{Name: "pvcs", Run: env.gated(claim, func(ctx context.Context) error {
			return env.deletePVCs(ctx, claim.Name)
		})},
		e2e.CleanupPhase{Name: "namespace", Run: env.gated(claim, func(ctx context.Context) error {
			return env.deleteNamespaceObject(ctx, claim)
		})},
	)
	return e2e.RunCleanup(context.Background(), phases, e2e.WithPhaseTimeout(namespaceWaitTimeout))
}

func (env *e2eEnv) gated(claim e2e.Claim, fn func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		view, err := env.namespaceView(ctx, claim.Name)
		if confirmErr := e2e.ConfirmNamespaceOwner(claim, view, err); confirmErr != nil {
			if errors.Is(confirmErr, e2e.ErrNamespaceGone) {
				return nil
			}
			return confirmErr
		}
		return fn(ctx)
	}
}

func (env *e2eEnv) deleteRelease(t *testing.T, dir, namespace string, rel releaseTarget) func(context.Context) error {
	t.Helper()
	return func(ctx context.Context) error {
		_, _, err := runInErrContext(t, ctx, dir, "delete", rel.Project, rel.Env,
			"--yes", "--wait", "--allow-missing-platform",
			"--context", kindContext, "--namespace", namespace)
		return err
	}
}

func (env *e2eEnv) deleteNamespaceObject(ctx context.Context, claim e2e.Claim) error {
	pre, err := e2e.UIDPrecondition(claim.UID)
	if err != nil {
		return err
	}
	res, err := env.newResources("")
	if err != nil {
		return err
	}
	ns := &corev1.Namespace{}
	ns.Name = claim.Name
	err = res.Delete(ctx, ns, func(opts *metav1.DeleteOptions) {
		opts.Preconditions = &pre
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return wait.For(func(waitCtx context.Context) (bool, error) {
		getErr := res.Get(waitCtx, claim.Name, "", ns)
		if apierrors.IsNotFound(getErr) {
			return true, nil
		}
		if getErr != nil && !isRetryableAPIError(getErr) {
			return false, getErr
		}
		return false, nil
	}, wait.WithTimeout(namespaceWaitTimeout), wait.WithInterval(resourcePollInterval),
		wait.WithContext(ctx), wait.WithImmediate())
}

func (env *e2eEnv) deleteWorkloads(ctx context.Context, namespace string) error {
	res, err := env.newResources(namespace)
	if err != nil {
		return err
	}
	var errs []error
	var cronjobs batchv1.CronJobList
	if listErr := res.List(ctx, &cronjobs); listErr != nil && !apierrors.IsNotFound(listErr) {
		errs = append(errs, fmt.Errorf("list cronjobs: %w", listErr))
	}
	for i := range cronjobs.Items {
		if delErr := res.Delete(ctx, &cronjobs.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			errs = append(errs, fmt.Errorf("delete cronjob %s: %w", cronjobs.Items[i].Name, delErr))
		}
	}
	var jobs batchv1.JobList
	if listErr := res.List(ctx, &jobs); listErr != nil && !apierrors.IsNotFound(listErr) {
		errs = append(errs, fmt.Errorf("list jobs: %w", listErr))
	}
	for i := range jobs.Items {
		if delErr := res.Delete(ctx, &jobs.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			errs = append(errs, fmt.Errorf("delete job %s: %w", jobs.Items[i].Name, delErr))
		}
	}
	var sets appsv1.StatefulSetList
	if listErr := res.List(ctx, &sets); listErr != nil && !apierrors.IsNotFound(listErr) {
		errs = append(errs, fmt.Errorf("list statefulsets: %w", listErr))
	}
	for i := range sets.Items {
		if delErr := res.Delete(ctx, &sets.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			errs = append(errs, fmt.Errorf("delete statefulset %s: %w", sets.Items[i].Name, delErr))
		}
	}
	var deploys appsv1.DeploymentList
	if listErr := res.List(ctx, &deploys); listErr != nil && !apierrors.IsNotFound(listErr) {
		errs = append(errs, fmt.Errorf("list deployments: %w", listErr))
	}
	for i := range deploys.Items {
		if delErr := res.Delete(ctx, &deploys.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			errs = append(errs, fmt.Errorf("delete deployment %s: %w", deploys.Items[i].Name, delErr))
		}
	}
	waitErr := wait.For(func(waitCtx context.Context) (bool, error) {
		var pods corev1.PodList
		if listErr := res.List(waitCtx, &pods); listErr != nil {
			if apierrors.IsNotFound(listErr) {
				return true, nil
			}
			if isRetryableAPIError(listErr) {
				return false, nil
			}
			return false, listErr
		}
		return len(pods.Items) == 0, nil
	}, wait.WithTimeout(namespaceWaitTimeout), wait.WithInterval(resourcePollInterval),
		wait.WithContext(ctx), wait.WithImmediate())
	if waitErr != nil {
		errs = append(errs, fmt.Errorf("wait for pods: %w", waitErr))
	}
	return errors.Join(errs...)
}

func (env *e2eEnv) deletePVCs(ctx context.Context, namespace string) error {
	res, err := env.newResources(namespace)
	if err != nil {
		return err
	}
	var pvcs corev1.PersistentVolumeClaimList
	if listErr := res.List(ctx, &pvcs); listErr != nil {
		if apierrors.IsNotFound(listErr) {
			return nil
		}
		return fmt.Errorf("list pvcs: %w", listErr)
	}
	var errs []error
	for i := range pvcs.Items {
		if delErr := res.Delete(ctx, &pvcs.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			errs = append(errs, fmt.Errorf("delete pvc %s: %w", pvcs.Items[i].Name, delErr))
		}
	}
	waitErr := wait.For(func(waitCtx context.Context) (bool, error) {
		var remaining corev1.PersistentVolumeClaimList
		if listErr := res.List(waitCtx, &remaining); listErr != nil {
			if apierrors.IsNotFound(listErr) {
				return true, nil
			}
			if isRetryableAPIError(listErr) {
				return false, nil
			}
			return false, listErr
		}
		return len(remaining.Items) == 0, nil
	}, wait.WithTimeout(namespaceWaitTimeout), wait.WithInterval(resourcePollInterval),
		wait.WithContext(ctx), wait.WithImmediate())
	if waitErr != nil {
		errs = append(errs, fmt.Errorf("wait for pvcs: %w", waitErr))
	}
	return errors.Join(errs...)
}

func (env *e2eEnv) observe(ctx context.Context, sel e2e.ResourceSelect, scenarioNS string) ([]map[string]any, error) {
	gvk, err := parseSelectGVK(sel)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", e2e.ErrObserveTerminal, err)
	}
	namespaced, err := env.isNamespaced(gvk)
	if err != nil {
		return nil, wrapObserve(err)
	}
	lookupNS := ""
	if namespaced {
		lookupNS = scenarioNS
		if sel.Namespace != "" {
			lookupNS = sel.Namespace
		}
	}
	res, err := env.newResources(lookupNS)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", e2e.ErrObserveTerminal, err)
	}
	if sel.Name != "" {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		getErr := res.Get(ctx, sel.Name, lookupNS, obj)
		if apierrors.IsNotFound(getErr) {
			return []map[string]any{}, nil
		}
		if getErr != nil {
			return nil, wrapObserve(getErr)
		}
		return []map[string]any{obj.Object}, nil
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List",
	})
	var opts []resources.ListOption
	if len(sel.Labels) > 0 {
		opts = append(opts, resources.WithLabelSelector(labels.Set(sel.Labels).String()))
	}
	if listErr := res.List(ctx, list, opts...); listErr != nil {
		if apierrors.IsNotFound(listErr) {
			return []map[string]any{}, nil
		}
		return nil, wrapObserve(listErr)
	}
	out := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, list.Items[i].Object)
	}
	return out, nil
}

func wrapObserve(err error) error {
	if isRetryableAPIError(err) {
		return fmt.Errorf("%w: %w", e2e.ErrObserveRetry, err)
	}
	return fmt.Errorf("%w: %w", e2e.ErrObserveTerminal, err)
}

func parseSelectGVK(sel e2e.ResourceSelect) (schema.GroupVersionKind, error) {
	if sel.Kind == "" || sel.APIVersion == "" {
		return schema.GroupVersionKind{}, errors.New("select requires apiVersion and kind")
	}
	gv, err := schema.ParseGroupVersion(sel.APIVersion)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("parse apiVersion %q: %w", sel.APIVersion, err)
	}
	return gv.WithKind(sel.Kind), nil
}

func (env *e2eEnv) isNamespaced(gvk schema.GroupVersionKind) (bool, error) {
	env.mapperMu.Lock()
	defer env.mapperMu.Unlock()
	mapping, err := env.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		env.mapper.Reset()
		mapping, err = env.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	if err != nil {
		return false, fmt.Errorf("RESTMapping %s: %w", gvk.String(), err)
	}
	return mapping.Scope.Name() == meta.RESTScopeNameNamespace, nil
}
