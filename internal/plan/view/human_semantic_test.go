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

package view_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	productClusterContext = "kind-prod"
	productNamespace      = "prod"
	productEnv            = "prod"
)

// TestWriteHuman_SemanticPlan is the canonical human output of
// RenderOffline -> prediction -> BuildSemanticPlan -> WriteHuman.
// human_all_actions.golden stays the synthetic generic renderer contract.
func TestWriteHuman_SemanticPlan(t *testing.T) {
	t.Parallel()
	p := semanticUpgradePlan(t, previousProductSpec(), currentProductSpec())
	text := writeHuman(t, p)
	assertGolden(t, "human_semantic_plan", text) // full-output contract; Contains below are invariants only
	assertHumanLayout(t, text)
	assertSemanticHeader(t, text, 2)
	assert.Equal(t, "web-prod", p.Header.Release)
	assert.Equal(t, productNamespace, p.Header.Namespace)
	assert.False(t, p.Header.FreshInstall)
	assert.Contains(t, text, `~ update apps/v1/Deployment "web-prod-api"`)
	assert.Contains(t, text, `+ create apps/v1/Deployment "web-prod-worker"`)
	assert.Contains(t, text, `- delete apps/v1/Deployment "web-prod-legacy"`)
	assert.Contains(t, text, "deployah.dev/project: web")
	assert.Contains(t, text, "deployah.dev/environment: prod")
	assert.Contains(t, text, "deployah.dev/instance: web-prod")
	assert.Contains(t, text, "app.kubernetes.io/managed-by: Helm")
	assert.Contains(t, text, "helm.sh/chart:")
	assert.Contains(t, text, "meta.helm.sh/release-name: web-prod")
	assert.Contains(t, text, "meta.helm.sh/release-namespace: prod")
	assert.Contains(t, text, "helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded")
	assert.Contains(t, text, "  preDeploy")
	assert.Contains(t, text, "~ migrate  changed, will run")
	assert.Contains(t, text, "  postDeploy")
	assert.Contains(t, text, "+ smoke  new, will run")
	assert.Contains(t, text, "- old-check  removed")
	assert.Contains(t, text, "  schedule")
	assert.Contains(t, text, "~ cleanup  changed")
	assert.NotContains(t, text, "~ cleanup  changed, will run")
	assert.Contains(t, text, `-   schedule: 0 2 * * *`)
	assert.Contains(t, text, `+   schedule: 0 3 * * *`)
	assertCronJobOnlyUnderTasks(t, text, "web-prod-cleanup")
	assert.NotContains(t, text, `create v1/Namespace`)
	assert.NotContains(t, text, "-/+")
	assert.NotContains(t, strings.ToLower(text), "recreate")
	assert.NotContains(t, text, "Actions:")
	assert.NotContains(t, text, "completeness:")
}

func TestWriteHuman_SemanticScheduleCreate(t *testing.T) {
	t.Parallel()
	previous := productSpec(
		map[string]spec.Component{"api": productAPI("ghcr.io/example/web:1.0")},
		map[string]spec.Task{"migrate": hookTask(spec.TaskOnPreDeploy, "migrate", "up")},
	)
	current := productSpec(
		map[string]spec.Component{"api": productAPI("ghcr.io/example/web:1.0")},
		map[string]spec.Task{
			"migrate": hookTask(spec.TaskOnPreDeploy, "migrate", "up"),
			"cleanup": scheduleTask("0 3 * * *", "./cleanup"),
		},
	)
	text := writeHuman(t, semanticUpgradePlan(t, previous, current))
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  schedule")
	assert.Contains(t, text, "+ cleanup  new")
	assert.NotContains(t, text, "+ cleanup  new, will run")
	assert.Contains(t, text, `+ create batch/v1/CronJob "web-prod-cleanup"`)
	assertCronJobOnlyUnderTasks(t, text, "web-prod-cleanup")
	assertSemanticCronJobBody(t, text)
	assert.NotContains(t, text, "helm.sh/hook:")
	assert.NotContains(t, text, `create v1/Namespace`)
	assert.NotContains(t, text, "-/+")
}

func TestWriteHuman_SemanticScheduleDelete(t *testing.T) {
	t.Parallel()
	previous := productSpec(
		map[string]spec.Component{"api": productAPI("ghcr.io/example/web:1.0")},
		map[string]spec.Task{
			"migrate": hookTask(spec.TaskOnPreDeploy, "migrate", "up"),
			"cleanup": scheduleTask("0 3 * * *", "./cleanup"),
		},
	)
	current := productSpec(
		map[string]spec.Component{"api": productAPI("ghcr.io/example/web:1.0")},
		map[string]spec.Task{"migrate": hookTask(spec.TaskOnPreDeploy, "migrate", "up")},
	)
	text := writeHuman(t, semanticUpgradePlan(t, previous, current))
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  schedule")
	assert.Contains(t, text, "- cleanup  removed")
	assert.NotContains(t, text, "- cleanup  removed, will run")
	assert.Contains(t, text, `- delete batch/v1/CronJob "web-prod-cleanup"`)
	assertCronJobOnlyUnderTasks(t, text, "web-prod-cleanup")
	assertSemanticCronJobBody(t, text)
	assert.Contains(t, text, "meta.helm.sh/release-name: web-prod")
	assert.NotContains(t, text, `create v1/Namespace`)
	assert.NotContains(t, text, "-/+")
}

func assertSemanticHeader(t *testing.T, text string, revision int) {
	t.Helper()
	block := fmt.Sprintf("Context:   %s\nNamespace: %s\nRelease:   web-prod\nRevision:  %d\n", productClusterContext, productNamespace, revision)
	assert.Contains(t, text, block)
	contextIdx := strings.Index(text, "Context:")
	nsIdx := strings.Index(text, "Namespace:")
	relIdx := strings.Index(text, "Release:")
	revIdx := strings.Index(text, "Revision:")
	require.Greater(t, contextIdx, -1)
	assert.Greater(t, nsIdx, contextIdx)
	assert.Greater(t, relIdx, nsIdx)
	assert.Greater(t, revIdx, relIdx)
}

func assertCronJobOnlyUnderTasks(t *testing.T, text, name string) {
	t.Helper()
	needle := fmt.Sprintf(`batch/v1/CronJob %q`, name)
	assert.Equal(t, 1, strings.Count(text, needle))
	tasksIdx := strings.Index(text, "\nTasks\n")
	require.Greater(t, tasksIdx, -1)
	assert.Contains(t, text[tasksIdx:], needle)
	resourcesIdx := strings.Index(text, "\nResources\n")
	if resourcesIdx >= 0 && tasksIdx > resourcesIdx {
		assert.NotContains(t, text[resourcesIdx:tasksIdx], needle)
	}
}

func assertSemanticCronJobBody(t *testing.T, text string) {
	t.Helper()
	assert.Contains(t, text, "deployah.dev/task: cleanup")
	assert.Contains(t, text, "deployah.dev/project: web")
	assert.Contains(t, text, "app.kubernetes.io/managed-by: Helm")
	assert.Contains(t, text, "helm.sh/chart:")
	assert.Contains(t, text, "meta.helm.sh/release-name: web-prod")
	assert.Contains(t, text, "meta.helm.sh/release-namespace: prod")
	assert.Contains(t, text, "jobTemplate:")
	assert.Contains(t, text, "schedule: 0 3 * * *")
}

func previousProductSpec() *spec.Spec {
	return productSpec(
		map[string]spec.Component{
			"api":    productAPI("ghcr.io/example/web:1.0"),
			"legacy": productWorker("ghcr.io/example/legacy:1.0"),
		},
		map[string]spec.Task{
			"migrate":   hookTask(spec.TaskOnPreDeploy, "migrate", "up"),
			"old-check": hookTask(spec.TaskOnPostDeploy, "./old-check"),
			"cleanup":   scheduleTask("0 2 * * *", "./cleanup"),
		},
	)
}

func currentProductSpec() *spec.Spec {
	return productSpec(
		map[string]spec.Component{
			"api":    productAPI("ghcr.io/example/web:2.0"),
			"worker": productWorker("ghcr.io/example/worker:1.0"),
		},
		map[string]spec.Task{
			"migrate": hookTask(spec.TaskOnPreDeploy, "migrate", "up", "--check"),
			"smoke":   hookTask(spec.TaskOnPostDeploy, "./smoke"),
			"cleanup": scheduleTask("0 3 * * *", "./cleanup"),
		},
	)
}

func productSpec(components map[string]spec.Component, tasks map[string]spec.Task) *spec.Spec {
	return &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "web",
		Components: components,
		Tasks:      tasks,
		Environments: map[string]spec.Environment{
			productEnv: {},
		},
	}
}

func productAPI(image string) spec.Component {
	return spec.Component{
		Role:  spec.ComponentRoleService,
		Image: image,
		Port:  8080,
		Env:   spec.StringMap{"LOG": "info"},
	}
}

func productWorker(image string) spec.Component {
	return spec.Component{
		Role:  spec.ComponentRoleWorker,
		Image: image,
	}
}

func hookTask(on spec.TaskOn, command ...string) spec.Task {
	return spec.Task{
		From:    "api",
		On:      on,
		Command: command,
	}
}

func scheduleTask(schedule string, command ...string) spec.Task {
	return spec.Task{
		From:     "api",
		On:       spec.TaskOnSchedule,
		Schedule: schedule,
		Command:  command,
	}
}

func semanticUpgradePlan(t *testing.T, previous, current *spec.Spec) semantic.Plan {
	t.Helper()
	client := productHelmClient(t)
	prevResolved := resolveProductSpec(t, previous)
	prevResult := renderOffline(t, client, prevResolved)
	prevRelease := previousReleaseFromRender(t, prevResult)

	cluster := newFakeCluster()
	seedTargetNamespace(cluster, prevResult.Namespace)
	installPlan := buildSemanticPlan(t, &fakeBuildClient{
		result: prevResult,
		prep:   helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
	}, cluster, prevResolved)
	storePredictedLive(cluster, installPlan.Changes)

	currResolved := resolveProductSpec(t, current)
	currResult := renderOffline(t, client, currResolved)
	return buildSemanticPlan(t, &fakeBuildClient{
		result: offlineUpgradeResult(currResult, 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      prevRelease,
			Newest:       prevRelease,
			NextRevision: 2,
		},
	}, cluster, currResolved)
}

func productHelmClient(t *testing.T) *helm.Client {
	t.Helper()
	client, err := helm.NewClient(helm.WithNamespace(productNamespace))
	require.NoError(t, err)
	return client
}

func resolveProductSpec(t *testing.T, manifest *spec.Spec) *spec.ResolvedSpec {
	t.Helper()
	if manifest.SpecDir == "" {
		manifest.SpecDir = t.TempDir()
	}
	require.NoError(t, spec.FillSpecWithDefaults(manifest, spec.CurrentManifestVersion))
	resolved, _, err := spec.Resolve(manifest, nil, spec.NormalizeEnv(productEnv), spec.SubstitutionReport{})
	require.NoError(t, err)
	return resolved
}

// offlineUpgradeResult copies an offline install render and sets the
// upgrade flags [plan.BuildSemanticPlan] checks. Manifest and Hooks stay
// the [helm.Client.RenderOffline] output; that API cannot emit an upgrade
// dry-run.
func offlineUpgradeResult(result *render.RenderResult, revision int) *render.RenderResult {
	out := *result
	out.IsUpgrade = true
	out.Revision = revision
	return &out
}

func renderOffline(t *testing.T, client *helm.Client, resolved *spec.ResolvedSpec) *render.RenderResult {
	t.Helper()
	result, cleanup, err := client.RenderOffline(t.Context(), resolved, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup)
	return result
}

func previousReleaseFromRender(t *testing.T, result *render.RenderResult) *v1.Release {
	t.Helper()
	require.NotEmpty(t, result.ChartPath)
	raw, err := os.ReadFile(filepath.Join(result.ChartPath, "values.yaml"))
	require.NoError(t, err)
	var values map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &values))
	if values == nil {
		values = map[string]any{}
	}
	tasks, ok := nestedMap(values, "deployah", "resolved", "tasks")
	require.True(t, ok && len(tasks) > 0, "previous chart values must include deployah.resolved.tasks")
	return &v1.Release{
		Name:      result.ReleaseName,
		Namespace: result.Namespace,
		Manifest:  result.Manifest,
		Hooks:     result.Hooks,
		Config:    values,
		Version:   1,
	}
}

func nestedMap(root map[string]any, keys ...string) (map[string]any, bool) {
	cur := root
	for _, key := range keys {
		next, ok := cur[key].(map[string]any)
		if !ok || next == nil {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func seedTargetNamespace(cluster *fakeCluster, namespace string) {
	cluster.store(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   namespace,
			"labels": map[string]any{"name": namespace},
		},
	}})
}

func storePredictedLive(cluster *fakeCluster, changes []semantic.ResourceChange) {
	for _, c := range changes {
		if c.After == nil || c.After.Object == nil {
			continue
		}
		cluster.store(&unstructured.Unstructured{Object: c.After.Object})
	}
}

func buildSemanticPlan(t *testing.T, client plan.SemanticBuildClient, cluster predict.Cluster, resolved *spec.ResolvedSpec) semantic.Plan {
	t.Helper()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, plan.SemanticBuildInput{
		ClusterContext: productClusterContext,
		Resolved:       resolved,
		CRDPolicy:      extras.PolicyCreate,
	})
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	return p
}

type fakeBuildClient struct {
	result *render.RenderResult
	prep   helm.ReleasePrep
}

func (c *fakeBuildClient) RenderManifestsWithPrep(
	_ context.Context,
	_ *spec.ResolvedSpec,
	_ postrenderer.PostRenderer,
	_ []extras.Object,
) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	return c.result, c.prep, func() {}, nil
}

type fakeCluster struct {
	objects map[string]*unstructured.Unstructured
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: make(map[string]*unstructured.Unstructured)}
}

func (f *fakeCluster) store(obj *unstructured.Unstructured) {
	f.objects[clusterKey(identityOf(obj))] = obj.DeepCopy()
}

func (f *fakeCluster) Get(_ context.Context, id predict.Identity) (*unstructured.Unstructured, error) {
	obj, ok := f.objects[clusterKey(id)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: strings.ToLower(id.Kind) + "s"}, id.Name)
	}
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) Create(_ context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) Apply(_ context.Context, obj *unstructured.Unstructured, _ predict.ApplyOptions) (*unstructured.Unstructured, error) {
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) JSONPatch(context.Context, predict.Identity, []byte) error {
	return nil
}

func (f *fakeCluster) Delete(context.Context, predict.Identity) error {
	return nil
}

func (f *fakeCluster) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	scope := meta.RESTScopeNamespace
	if gvk.Group == "" && gvk.Kind == "Namespace" {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: strings.ToLower(gvk.Kind) + "s"},
		GroupVersionKind: gvk,
		Scope:            scope,
	}, nil
}

func clusterKey(id predict.Identity) string {
	gv := schema.GroupVersion{Group: id.Group, Version: id.Version}
	return fmt.Sprintf("%s/%s/%s/%s", gv.String(), id.Kind, id.Namespace, id.Name)
}

func identityOf(obj *unstructured.Unstructured) predict.Identity {
	gvk := obj.GroupVersionKind()
	return predict.Identity{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}
