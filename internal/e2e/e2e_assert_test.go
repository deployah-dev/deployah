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

//go:build e2e

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"

	inttest "deployah.dev/deployah/internal/testing"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	netutil "k8s.io/apimachinery/pkg/util/net"
)

const (
	resourcePollInterval = 2 * time.Second
	logsRetryInterval    = 5 * time.Second
	namespaceWaitTimeout = 2 * time.Minute
	e2eSchemaModeLine    = "# $schema: ../../internal/testing/e2e.schema.json"
)

func (s *E2ESuite) assertE2EFixture(t *testing.T, dir, project, ns string, fx inttest.E2EFixture) {
	t.Helper()
	if len(fx.Steps) > 0 {
		for i, step := range fx.Steps {
			s.executeStep(t, dir, project, ns, fx, i, step)
		}
		return
	}
	s.executeStep(t, dir, project, ns, fx, 0, inttest.Step{
		Deploy:    &inttest.DeployOp{},
		Resources: fx.Resources,
	})
	if *flagScaffold {
		s.scaffoldSimple(t, dir, ns, fx)
	}
}

func (s *E2ESuite) executeStep(t *testing.T, dir, project, ns string, fx inttest.E2EFixture, index int, step inttest.Step) {
	t.Helper()
	timeout, err := fx.StepTimeout(step)
	require.NoErrorf(t, err, "steps[%d] timeout", index)
	args := stepArgs(project, fx.Env, ns, step)

	if step.Logs != nil {
		s.retryLogs(t, dir, args, step.Logs.Contains, timeout)
	} else {
		stdout, stderr := runIn(t, dir, args...)
		if step.StdoutContains != "" {
			require.Containsf(t, stdout, step.StdoutContains, "steps[%d] stdout", index)
		}
		if step.StderrContains != "" {
			require.Containsf(t, stderr, step.StderrContains, "steps[%d] stderr", index)
		}
	}

	if len(step.Resources) == 0 {
		return
	}
	s.waitForResources(t, ns, step.Resources, timeout)
}

func (s *E2ESuite) retryLogs(t *testing.T, dir string, args []string, contains string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var stdout, stderr string
	var runErr error
	for {
		stdout, stderr, runErr = runInErr(t, dir, args...)
		if runErr == nil && strings.Contains(stdout, contains) {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-t.Context().Done():
			require.Fail(t, "logs retry canceled", "want %q in stdout\nstdout:\n%s\nstderr:\n%s\nerr: %v",
				contains, stdout, stderr, runErr)
		case <-time.After(logsRetryInterval):
		}
	}
	require.Fail(t, "logs never matched", "want %q in stdout within %s\nstdout:\n%s\nstderr:\n%s\nerr: %v",
		contains, timeout, stdout, stderr, runErr)
}

func stepArgs(project, env, ns string, step inttest.Step) []string {
	switch step.OpName() {
	case "deploy":
		args := []string{"deploy", env, "--context", kindContext, "--yes", "--namespace", ns}
		if step.Deploy != nil && step.Deploy.Spec != "" {
			args = append(args, "--spec", step.Deploy.Spec)
		}
		if step.Deploy != nil {
			args = append(args, step.Deploy.Args...)
		}
		return args
	case "run":
		return []string{"run", step.Run.Task, env, "--context", kindContext, "--yes", "--namespace", ns}
	case "logs":
		return []string{
			"logs", project,
			"--component=" + step.Logs.Component,
			"--environment=" + env,
			"--no-follow",
			"--context", kindContext,
			"--namespace", ns,
		}
	case "delete":
		return []string{
			"delete", project, env,
			"--yes", "--wait", "--allow-missing-platform",
			"--context", kindContext,
			"--namespace", ns,
		}
	default:
		return nil
	}
}

func (s *E2ESuite) waitForResources(t *testing.T, ns string, assertions []inttest.ResourceAssertion, timeout time.Duration) {
	t.Helper()
	var last []string
	err := wait.For(func(ctx context.Context) (bool, error) {
		var all []string
		for i, ra := range assertions {
			diffs, checkErr := s.checkAssertion(ctx, ns, ra)
			if checkErr != nil {
				if isRetryableAPIError(checkErr) {
					all = append(all, fmt.Sprintf("resources[%d]: %v", i, checkErr))
					continue
				}
				return false, fmt.Errorf("resources[%d]: %w", i, checkErr)
			}
			for _, d := range diffs {
				all = append(all, fmt.Sprintf("resources[%d]: %s", i, d))
			}
		}
		last = all
		return len(all) == 0, nil
	}, wait.WithTimeout(timeout), wait.WithInterval(resourcePollInterval),
		wait.WithContext(t.Context()), wait.WithImmediate())
	if err == nil {
		return
	}
	s.dumpDiagnostics(t, ns, assertions)
	require.NoErrorf(t, err, "resource assertions failed:\n%s", strings.Join(last, "\n"))
}

// isRetryableAPIError reports whether resource polling should retry err.
// It returns true for NotFound, conflict, timeout, and transport failures.
// REST mapping misses, forbidden requests, and canceled contexts fail
// immediately.
func isRetryableAPIError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch {
	case apierrors.IsNotFound(err),
		apierrors.IsConflict(err),
		apierrors.IsServerTimeout(err),
		apierrors.IsTooManyRequests(err),
		apierrors.IsServiceUnavailable(err),
		apierrors.IsTimeout(err),
		apierrors.IsInternalError(err):
		return true
	}
	return netutil.IsProbableEOF(err) ||
		netutil.IsConnectionReset(err) ||
		netutil.IsConnectionRefused(err) ||
		netutil.IsTimeout(err)
}

func (s *E2ESuite) checkAssertion(ctx context.Context, ns string, ra inttest.ResourceAssertion) ([]string, error) {
	gvk, err := gvkFromMatch(ra.Match)
	if err != nil {
		return nil, err
	}
	namespaced, err := s.isNamespaced(gvk)
	if err != nil {
		return nil, err
	}
	lookupNS := ""
	if namespaced {
		lookupNS = ns
	}
	name, labelSet := matchMeta(ra.Match)
	want := ra.Count()
	res, err := s.newResources(lookupNS)
	if err != nil {
		return nil, err
	}

	if name != "" {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		getErr := res.Get(ctx, name, lookupNS, u)
		if apierrors.IsNotFound(getErr) {
			if want == 0 {
				return nil, nil
			}
			return []string{fmt.Sprintf("%s %s not found", gvk.Kind, name)}, nil
		}
		if getErr != nil {
			return nil, getErr
		}
		if want == 0 {
			return []string{fmt.Sprintf("%s %s still exists", gvk.Kind, name)}, nil
		}
		return inttest.DiffSubset("$", ra.Match, u.Object), nil
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})
	var opts []resources.ListOption
	if len(labelSet) > 0 {
		opts = append(opts, resources.WithLabelSelector(labels.Set(labelSet).String()))
	}
	if listErr := res.List(ctx, list, opts...); listErr != nil {
		return nil, listErr
	}

	matched := 0
	var sample []string
	for i := range list.Items {
		diffs := inttest.DiffSubset("$", ra.Match, list.Items[i].Object)
		if len(diffs) == 0 {
			matched++
			continue
		}
		if sample == nil {
			sample = diffs
		}
	}
	if want == 0 {
		if matched == 0 {
			return nil, nil
		}
		return []string{fmt.Sprintf("want 0 %s matching, got %d", gvk.Kind, matched)}, nil
	}
	if matched >= want {
		return nil, nil
	}
	msg := fmt.Sprintf("want >= %d %s matching, got %d", want, gvk.Kind, matched)
	if sample != nil {
		return append([]string{msg}, sample...), nil
	}
	return []string{msg}, nil
}

func (s *E2ESuite) isNamespaced(gvk schema.GroupVersionKind) (bool, error) {
	s.mapperMu.Lock()
	defer s.mapperMu.Unlock()
	mapping, err := s.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		s.mapper.Reset()
		mapping, err = s.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	}
	if err != nil {
		return false, fmt.Errorf("RESTMapping %s: %w", gvk.String(), err)
	}
	return mapping.Scope.Name() == meta.RESTScopeNameNamespace, nil
}

func (s *E2ESuite) dumpDiagnostics(t *testing.T, ns string, assertions []inttest.ResourceAssertion) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i, ra := range assertions {
		gvk, err := gvkFromMatch(ra.Match)
		if err != nil {
			t.Logf("resources[%d]: %v", i, err)
			continue
		}
		name, labelSet := matchMeta(ra.Match)
		namespaced, nsErr := s.isNamespaced(gvk)
		lookupNS := ""
		if nsErr == nil && namespaced {
			lookupNS = ns
		}
		res, resErr := s.newResources(lookupNS)
		if resErr != nil {
			t.Logf("resources client: %v", resErr)
			continue
		}
		if name != "" {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			if getErr := res.Get(ctx, name, lookupNS, u); getErr != nil {
				t.Logf("describe %s/%s: %v", gvk.Kind, name, getErr)
				continue
			}
			logLiveYAML(t, gvk.Kind, name, u.Object)
			continue
		}
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List",
		})
		var opts []resources.ListOption
		if len(labelSet) > 0 {
			opts = append(opts, resources.WithLabelSelector(labels.Set(labelSet).String()))
		}
		if listErr := res.List(ctx, list, opts...); listErr != nil {
			t.Logf("list %s: %v", gvk.Kind, listErr)
			continue
		}
		for j := range list.Items {
			logLiveYAML(t, gvk.Kind, list.Items[j].GetName(), list.Items[j].Object)
		}
	}
	var events corev1.EventList
	eventRes, eventResErr := s.newResources(ns)
	if eventResErr != nil {
		t.Logf("events client: %v", eventResErr)
		return
	}
	if listErr := eventRes.List(ctx, &events); listErr != nil {
		t.Logf("list events: %v", listErr)
		return
	}
	for i := range events.Items {
		ev := events.Items[i]
		t.Logf("event %s %s/%s: %s", ev.Type, ev.InvolvedObject.Kind, ev.InvolvedObject.Name, ev.Message)
	}
}

func (s *E2ESuite) createNamespace(t *testing.T, name string) {
	t.Helper()
	ns := &corev1.Namespace{Name: name}
	res, err := s.newResources("")
	require.NoError(t, err)
	err = res.Create(t.Context(), ns)
	if apierrors.IsAlreadyExists(err) {
		return
	}
	require.NoErrorf(t, err, "create namespace %s", name)
}

func (s *E2ESuite) deleteNamespace(t *testing.T, name string) {
	t.Helper()
	res, err := s.newResources(name)
	require.NoError(t, err)

	// Each phase gets its own deadline. A shared ctx that deletePVCs
	// exhausted used to make namespace Delete fail with rate limiter Wait.
	workCtx, workCancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
	defer workCancel()
	if workErr := s.deleteWorkloads(workCtx, t, res); workErr != nil {
		t.Logf("delete workloads in %s: %v", name, workErr)
	}

	pvcCtx, pvcCancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
	defer pvcCancel()
	if pvcErr := s.deletePVCs(pvcCtx, t, res); pvcErr != nil {
		t.Errorf("delete pvcs in %s: %v", name, pvcErr)
	}

	ns := &corev1.Namespace{Name: name}
	clusterRes, clusterErr := s.newResources("")
	require.NoError(t, clusterErr)
	nsCtx, nsCancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
	defer nsCancel()
	if delErr := clusterRes.Delete(nsCtx, ns); delErr != nil && !apierrors.IsNotFound(delErr) {
		t.Errorf("delete namespace %s: %v", name, delErr)
		return
	}
	waitErr := wait.For(func(ctx context.Context) (bool, error) {
		getErr := clusterRes.Get(ctx, name, "", ns)
		if apierrors.IsNotFound(getErr) {
			return true, nil
		}
		if getErr != nil && !isRetryableAPIError(getErr) {
			return false, getErr
		}
		return false, nil
	}, wait.WithTimeout(namespaceWaitTimeout), wait.WithInterval(resourcePollInterval),
		wait.WithContext(nsCtx), wait.WithImmediate())
	if waitErr != nil {
		t.Errorf("wait for namespace %s deletion: %v", name, waitErr)
	}
}

func (s *E2ESuite) deleteWorkloads(ctx context.Context, t *testing.T, res *resources.Resources) error {
	t.Helper()
	var cronjobs batchv1.CronJobList
	if listErr := res.List(ctx, &cronjobs); listErr != nil && !apierrors.IsNotFound(listErr) {
		return fmt.Errorf("list cronjobs: %w", listErr)
	}
	for i := range cronjobs.Items {
		if delErr := res.Delete(ctx, &cronjobs.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			t.Logf("delete cronjob %s: %v", cronjobs.Items[i].Name, delErr)
		}
	}
	var jobs batchv1.JobList
	if listErr := res.List(ctx, &jobs); listErr != nil && !apierrors.IsNotFound(listErr) {
		return fmt.Errorf("list jobs: %w", listErr)
	}
	for i := range jobs.Items {
		if delErr := res.Delete(ctx, &jobs.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			t.Logf("delete job %s: %v", jobs.Items[i].Name, delErr)
		}
	}
	var sts appsv1.StatefulSetList
	if listErr := res.List(ctx, &sts); listErr != nil && !apierrors.IsNotFound(listErr) {
		return fmt.Errorf("list statefulsets: %w", listErr)
	}
	for i := range sts.Items {
		if delErr := res.Delete(ctx, &sts.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			t.Logf("delete statefulset %s: %v", sts.Items[i].Name, delErr)
		}
	}
	var deploys appsv1.DeploymentList
	if listErr := res.List(ctx, &deploys); listErr != nil && !apierrors.IsNotFound(listErr) {
		return fmt.Errorf("list deployments: %w", listErr)
	}
	for i := range deploys.Items {
		if delErr := res.Delete(ctx, &deploys.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			t.Logf("delete deployment %s: %v", deploys.Items[i].Name, delErr)
		}
	}
	return wait.For(func(pollCtx context.Context) (bool, error) {
		var pods corev1.PodList
		if listErr := res.List(pollCtx, &pods); listErr != nil {
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
}

func (s *E2ESuite) deletePVCs(ctx context.Context, t *testing.T, res *resources.Resources) error {
	t.Helper()
	var pvcs corev1.PersistentVolumeClaimList
	if listErr := res.List(ctx, &pvcs); listErr != nil {
		if apierrors.IsNotFound(listErr) {
			return nil
		}
		return fmt.Errorf("list pvcs: %w", listErr)
	}
	for i := range pvcs.Items {
		if delErr := res.Delete(ctx, &pvcs.Items[i]); delErr != nil && !apierrors.IsNotFound(delErr) {
			t.Logf("delete pvc %s: %v", pvcs.Items[i].Name, delErr)
		}
	}
	return wait.For(func(ctx context.Context) (bool, error) {
		var remaining corev1.PersistentVolumeClaimList
		if listErr := res.List(ctx, &remaining); listErr != nil {
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
}

func logLiveYAML(t *testing.T, kind, name string, obj map[string]any) {
	t.Helper()
	raw, err := yaml.Marshal(obj)
	if err != nil {
		t.Logf("marshal %s/%s: %v", kind, name, err)
		return
	}
	t.Logf("live %s/%s:\n%s", kind, name, raw)
}

func (s *E2ESuite) scaffoldSimple(t *testing.T, dir, ns string, fx inttest.E2EFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	kinds := []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "StatefulSet"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
		{Group: "batch", Version: "v1", Kind: "Job"},
		{Group: "batch", Version: "v1", Kind: "CronJob"},
	}
	var resourcesOut []map[string]any
	for _, gvk := range kinds {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List",
		})
		if listErr := s.listKindInNamespace(ctx, ns, list); listErr != nil {
			t.Logf("scaffold list %s: %v", gvk.Kind, listErr)
			continue
		}
		for i := range list.Items {
			resourcesOut = append(resourcesOut, map[string]any{
				"match": scaffoldObject(list.Items[i].Object),
			})
		}
	}
	doc := map[string]any{
		"env":       fx.Env,
		"resources": resourcesOut,
	}
	body, err := yaml.Marshal(doc)
	require.NoError(t, err)
	out := e2eSchemaModeLine + "\n" + string(body)

	path := filepath.Join(dir, inttest.E2EFixtureFile)
	if _, statErr := os.Stat(path); statErr == nil {
		t.Logf("-e2e.scaffold: %s already exists; skeleton follows\n%s", path, out)
		return
	}
	require.NoError(t, os.WriteFile(path, []byte(out), 0o600))
	t.Logf("wrote scaffold %s", path)
}

func (s *E2ESuite) listKindInNamespace(ctx context.Context, ns string, list *unstructured.UnstructuredList) error {
	res, err := s.newResources(ns)
	if err != nil {
		return err
	}
	return res.List(ctx, list)
}

func scaffoldObject(obj map[string]any) map[string]any {
	kind := stringField(obj, "kind")
	out := map[string]any{
		"apiVersion": obj["apiVersion"],
		"kind":       kind,
	}
	if metaMap := mapField(obj, "metadata"); metaMap != nil {
		m := map[string]any{}
		if name, hasName := metaMap["name"]; hasName {
			m["name"] = name
		}
		if rawLabels := mapField(metaMap, "labels"); rawLabels != nil {
			cleaned := maps.Clone(rawLabels)
			delete(cleaned, "helm.sh/chart")
			if len(cleaned) > 0 {
				m["labels"] = cleaned
			}
		}
		out["metadata"] = m
	}
	if specMap := mapField(obj, "spec"); specMap != nil {
		out["spec"] = stripGeneratedSpec(deepCopyMap(specMap))
	}
	if st := mapField(obj, "status"); st != nil {
		ready := map[string]any{}
		switch kind {
		case "Deployment", "StatefulSet":
			if v, hasReady := st["readyReplicas"]; hasReady {
				ready["readyReplicas"] = v
			}
		case "Job":
			if v, hasSucceeded := st["succeeded"]; hasSucceeded {
				ready["succeeded"] = v
			}
		case "PersistentVolumeClaim":
			if v, hasPhase := st["phase"]; hasPhase {
				ready["phase"] = v
			}
		}
		if len(ready) > 0 {
			out["status"] = ready
		}
	}
	return out
}

func stripGeneratedSpec(spec map[string]any) map[string]any {
	delete(spec, "clusterIP")
	delete(spec, "clusterIPs")
	ports, isPorts := spec["ports"].([]any)
	if !isPorts {
		return spec
	}
	for i := range ports {
		pm, isMap := ports[i].(map[string]any)
		if !isMap {
			continue
		}
		delete(pm, "nodePort")
	}
	return spec
}

func deepCopyMap(in map[string]any) map[string]any {
	out := maps.Clone(in)
	for k, v := range out {
		switch n := v.(type) {
		case map[string]any:
			out[k] = deepCopyMap(n)
		case []any:
			cp := make([]any, 0, len(n))
			for _, item := range n {
				m, isMap := item.(map[string]any)
				if isMap {
					cp = append(cp, deepCopyMap(m))
					continue
				}
				cp = append(cp, item)
			}
			out[k] = cp
		}
	}
	return out
}

func gvkFromMatch(match map[string]any) (schema.GroupVersionKind, error) {
	kind := stringField(match, "kind")
	apiVersion := stringField(match, "apiVersion")
	if kind == "" || apiVersion == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("match requires apiVersion and kind")
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("parse apiVersion %q: %w", apiVersion, err)
	}
	return gv.WithKind(kind), nil
}

func matchMeta(match map[string]any) (name string, labelSet map[string]string) {
	metaMap := mapField(match, "metadata")
	if metaMap == nil {
		return "", nil
	}
	name = stringField(metaMap, "name")
	raw := mapField(metaMap, "labels")
	if raw == nil {
		return name, nil
	}
	labelSet = make(map[string]string, len(raw))
	for k, v := range raw {
		s, isString := v.(string)
		if !isString {
			continue
		}
		labelSet[k] = s
	}
	return name, labelSet
}

func stringField(m map[string]any, key string) string {
	s, ok := m[key].(string)
	if !ok {
		return ""
	}
	return s
}

func mapField(m map[string]any, key string) map[string]any {
	child, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return child
}
