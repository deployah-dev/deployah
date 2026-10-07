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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
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
// fixture render -> BuildSemanticPlan -> WriteHuman.
// human_all_actions.golden stays the synthetic generic renderer contract.
func TestWriteHuman_SemanticPlan(t *testing.T) {
	t.Parallel()
	p := semanticUpgradePlan(t, previousProductSpec(), currentProductSpec())
	text := writeHuman(t, p)
	assertGolden(t, "human_semantic_plan", text)
	assert.Equal(t, "web-prod", p.Header.Release)
	assert.Equal(t, productNamespace, p.Header.Namespace)
	assert.False(t, p.Header.FreshInstall)
	assertCronJobOnlyUnderTasks(t, text, "web-prod-cleanup")
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
	cache := helm.NewChartCache(time.Hour)
	prevResolved := resolveProductSpec(t, previous)
	prevResult := renderDesired(t, cache, prevResolved)
	prevRelease := previousReleaseFromRender(t, prevResult)

	currResolved := resolveProductSpec(t, current)
	currResult := renderDesired(t, cache, currResolved)
	return buildSemanticPlan(t, &fakeBuildClient{
		result: upgradeResultFrom(currResult, 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      prevRelease,
			Newest:       prevRelease,
			NextRevision: 2,
		},
	}, currResolved, liveMatchingManifest(t, prevRelease.Manifest, prevRelease.Namespace))
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

// upgradeResultFrom copies a fresh-install fixture render and sets the
// upgrade flags [plan.BuildSemanticPlan] checks. Manifest and Hooks stay
// the fresh-install fixture render.
func upgradeResultFrom(result *render.RenderResult, revision int) *render.RenderResult {
	out := *result
	out.IsUpgrade = true
	out.Revision = revision
	return &out
}

// renderDesired renders resolved as a fresh install in productNamespace
// without Kubernetes access. cache is shared by the previous and current
// renders in one test. The chart comes from [helm.PrepareChart]; Helm
// renders it with a client-only install dry run. This stays in the view
// tests because internal/testing and this package both register -update.
func renderDesired(t *testing.T, cache *helm.ChartCache, resolved *spec.ResolvedSpec) *render.RenderResult {
	t.Helper()

	chartPath, err := helm.PrepareChart(t.Context(), resolved, cache, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		if removeErr := os.RemoveAll(chartPath); removeErr != nil {
			t.Errorf("remove chart dir %s: %v", chartPath, removeErr)
		}
	})

	ch, err := loader.Load(chartPath)
	require.NoError(t, err)

	releaseName := helm.GenerateReleaseName(resolved.Spec.Project, resolved.Env.Original)
	install := action.NewInstall(action.NewConfiguration())
	install.ReleaseName = releaseName
	install.Namespace = productNamespace
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true

	rel, err := install.RunWithContext(t.Context(), ch, map[string]any{})
	require.NoError(t, err)
	v1rel, ok := rel.(*v1.Release)
	require.True(t, ok, "unexpected helm release type %T", rel)

	return &render.RenderResult{
		ReleaseName: releaseName,
		Namespace:   install.Namespace,
		Manifest:    v1rel.Manifest,
		Hooks:       v1rel.Hooks,
		IsUpgrade:   false,
		Revision:    1,
		ChartPath:   chartPath,
	}
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
	cur := values
	var tasks map[string]any
	ok := true
	for _, key := range []string{"deployah", "resolved", "tasks"} {
		next, isMap := cur[key].(map[string]any)
		if !isMap || next == nil {
			ok = false
			break
		}
		cur = next
		tasks = next
	}
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

func buildSemanticPlan(t *testing.T, client plan.SemanticBuildClient, resolved *spec.ResolvedSpec, live plan.LiveReader) semantic.Plan {
	t.Helper()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, productMapper{}, live, plan.SemanticBuildInput{
		ClusterContext: productClusterContext,
		Resolved:       resolved,
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
	_ []extras.RawFile,
) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	return c.result, c.prep, func() {}, nil
}

type productMapper struct{}

func (productMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}
	scope := meta.RESTScopeNamespace
	if clusterScopedKind(gk) {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gk.Group, Version: version, Resource: strings.ToLower(gk.Kind) + "s"},
		GroupVersionKind: gk.WithVersion(version),
		Scope:            scope,
	}, nil
}

func clusterScopedKind(gk schema.GroupKind) bool {
	switch gk.Kind {
	case "Namespace", "CustomResourceDefinition", "ClusterRole", "ClusterRoleBinding":
		return true
	default:
		return false
	}
}

// manifestLive returns Previous objects on Get and nothing on List, so an
// unchanged upgrade does not report drift.
type manifestLive struct {
	namespace string
	items     []*unstructured.Unstructured
}

func liveMatchingManifest(t *testing.T, manifest, namespace string) plan.LiveReader {
	t.Helper()
	if strings.TrimSpace(manifest) == "" {
		return &manifestLive{namespace: namespace}
	}
	infos, err := resource.NewLocalBuilder().
		ContinueOnError().
		Flatten().
		Unstructured().
		Stream(bytes.NewBufferString(manifest), "manifest").
		Do().Infos()
	require.NoError(t, err)
	items := make([]*unstructured.Unstructured, 0, len(infos))
	for _, info := range infos {
		obj, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			m, convErr := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
			require.NoError(t, convErr)
			obj = &unstructured.Unstructured{Object: m}
		}
		items = append(items, obj.DeepCopy())
	}
	return &manifestLive{namespace: namespace, items: items}
}

func (m *manifestLive) Get(_ context.Context, mapping *meta.RESTMapping, namespace, name string) (*unstructured.Unstructured, error) {
	for _, obj := range m.items {
		if obj.GetName() != name || obj.GetKind() != mapping.GroupVersionKind.Kind {
			continue
		}
		if obj.GroupVersionKind().Group != mapping.GroupVersionKind.Group || obj.GroupVersionKind().Version != mapping.GroupVersionKind.Version {
			continue
		}
		ns := ""
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns = obj.GetNamespace()
			if ns == "" {
				ns = m.namespace
			}
		}
		if ns != namespace {
			continue
		}
		return obj.DeepCopy(), nil
	}
	return nil, apierrors.NewNotFound(mapping.Resource.GroupResource(), name)
}

func (m *manifestLive) List(context.Context, *meta.RESTMapping, string, labels.Selector) ([]unstructured.Unstructured, error) {
	return nil, nil
}
