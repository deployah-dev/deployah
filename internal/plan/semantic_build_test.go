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

package plan_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type ctxKey struct{}

type identityPostRenderer struct{}

func (identityPostRenderer) Run(rendered *bytes.Buffer) (*bytes.Buffer, error) {
	return rendered, nil
}

type fakeBuildClient struct {
	result      *render.RenderResult
	prep        helm.ReleasePrep
	cleanup     func()
	err         error
	gotCtx      context.Context
	gotRenderer postrenderer.PostRenderer
	calls       int
}

func (f *fakeBuildClient) RenderManifestsWithPrep(
	ctx context.Context,
	_ *spec.ResolvedSpec,
	postRenderer postrenderer.PostRenderer,
) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	f.calls++
	f.gotCtx = ctx
	f.gotRenderer = postRenderer
	return f.result, f.prep, f.cleanup, f.err
}

type fakeCluster struct {
	objects  map[string]*unstructured.Unstructured
	getErr   map[string]error
	applyErr error
	gotCtx   context.Context
	gets     int
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{
		objects: make(map[string]*unstructured.Unstructured),
		getErr:  make(map[string]error),
	}
}

func (f *fakeCluster) store(obj *unstructured.Unstructured) {
	f.objects[clusterKey(identityOf(obj))] = obj.DeepCopy()
}

func (f *fakeCluster) Get(ctx context.Context, id predict.Identity) (*unstructured.Unstructured, error) {
	f.gotCtx = ctx
	f.gets++
	if err, ok := f.getErr[clusterKey(id)]; ok {
		return nil, err
	}
	obj, ok := f.objects[clusterKey(id)]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: strings.ToLower(id.Kind) + "s"}, id.Name)
	}
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) Create(context.Context, *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return nil, f.applyErr
}

func (f *fakeCluster) Apply(ctx context.Context, obj *unstructured.Unstructured, _ predict.ApplyOptions) (*unstructured.Unstructured, error) {
	f.gotCtx = ctx
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	return obj.DeepCopy(), nil
}

func (f *fakeCluster) JSONPatch(context.Context, predict.Identity, []byte) error {
	return nil
}

func (f *fakeCluster) Delete(context.Context, predict.Identity) error {
	return nil
}

func (f *fakeCluster) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: strings.ToLower(gvk.Kind) + "s"},
		GroupVersionKind: gvk,
		Scope:            meta.RESTScopeNamespace,
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

func configMapYAML(name, namespace, data string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  key: %s
`, name, namespace, data)
}

func ownedConfigMap(name, namespace, release, data string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "Helm",
			},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      release,
				"meta.helm.sh/release-namespace": namespace,
			},
		},
		"data": map[string]any{"key": data},
	}}
}

func resolvedSpec() *spec.ResolvedSpec {
	return &spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "shop"},
		Env:  spec.EnvIdentity{Original: "prod"},
	}
}

func installResult(manifest string) *render.RenderResult {
	return &render.RenderResult{
		ReleaseName: "web",
		Namespace:   "prod",
		Manifest:    manifest,
		IsUpgrade:   false,
		Revision:    1,
	}
}

func upgradeResult(manifest string, revision int) *render.RenderResult {
	return &render.RenderResult{
		ReleaseName: "web",
		Namespace:   "prod",
		Manifest:    manifest,
		IsUpgrade:   true,
		Revision:    revision,
	}
}

func hasFieldPath(fields []semantic.FieldChange, path string) bool {
	for _, f := range fields {
		if f.Path == path {
			return true
		}
	}
	return false
}

func registerCleanup(t *testing.T, cleanup func()) {
	t.Helper()
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup)
}

func TestBuildSemanticPlan_RequiresClient(t *testing.T) {
	t.Parallel()
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), nil, newFakeCluster(), "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "semantic plan requires a helm client")
	assert.Zero(t, p)
	assert.Nil(t, result)
}

func TestBuildSemanticPlan_RequiresResolvedSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resolved *spec.ResolvedSpec
	}{
		{name: "nil resolved", resolved: nil},
		{name: "nil spec", resolved: &spec.ResolvedSpec{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{}
			p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), "ctx", tt.resolved, nil)
			registerCleanup(t, cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, "semantic plan requires resolved spec; call spec.Resolve first")
			assert.Zero(t, p)
			assert.Nil(t, result)
			assert.Equal(t, 0, client.calls)
		})
	}
}

func TestBuildSemanticPlan_FreshInstall(t *testing.T) {
	t.Parallel()
	result := installResult(configMapYAML("app", "prod", "v1"))
	var post postrenderer.PostRenderer = &identityPostRenderer{}
	var cleanups int
	client := &fakeBuildClient{
		result:  result,
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() { cleanups++ },
	}
	ctx := context.WithValue(t.Context(), ctxKey{}, "pipeline")
	cluster := newFakeCluster()

	p, got, cleanup, err := plan.BuildSemanticPlan(ctx, client, cluster, "kind-dev", resolvedSpec(), post)
	require.NotNil(t, cleanup)
	t.Cleanup(func() {
		if cleanups == 0 {
			cleanup()
		}
	})
	require.NoError(t, err)
	assert.Same(t, result, got)
	assert.Equal(t, post, client.gotRenderer)
	assert.Equal(t, "pipeline", client.gotCtx.Value(ctxKey{}))
	assert.Equal(t, "pipeline", cluster.gotCtx.Value(ctxKey{}))
	assert.Equal(t, "shop", p.Header.Project)
	assert.Equal(t, "prod", p.Header.Environment)
	assert.Equal(t, "web", p.Header.Release)
	assert.Equal(t, "prod", p.Header.Namespace)
	assert.Equal(t, "kind-dev", p.Header.Context)
	assert.Equal(t, 1, p.Header.Revision)
	assert.True(t, p.Header.FreshInstall)
	assert.Equal(t, semantic.HelmInstall, p.HelmAction)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, "app", p.Changes[0].Resource.Name)
	assert.Nil(t, p.Changes[0].Before)
	require.NotNil(t, p.Changes[0].After)
	assert.Equal(t, 0, cleanups)
	cleanup()
	assert.Equal(t, 1, cleanups)
}

func TestBuildSemanticPlan_Upgrade(t *testing.T) {
	t.Parallel()
	current := &v1.Release{Version: 6, Manifest: configMapYAML("app", "prod", "old")}
	result := upgradeResult(configMapYAML("app", "prod", "new"), 7)
	client := &fakeBuildClient{
		result: result,
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       current,
			NextRevision: 7,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "old"))

	p, got, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "kind-dev", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Same(t, result, got)
	assert.Equal(t, "kind-dev", p.Header.Context)
	assert.Equal(t, 7, p.Header.Revision)
	assert.False(t, p.Header.FreshInstall)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	require.NotNil(t, p.Changes[0].Before)
	require.NotNil(t, p.Changes[0].After)
	beforeData, ok := p.Changes[0].Before.Object["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "old", beforeData["key"])
	afterData, ok := p.Changes[0].After.Object["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "new", afterData["key"])
	assert.True(t, hasFieldPath(p.Changes[0].Fields, "/data/key"))
}

func TestBuildSemanticPlan_PreviousUsesCurrentNotNewest(t *testing.T) {
	t.Parallel()
	removed := configMapYAML("removed", "prod", "old")
	kept := configMapYAML("app", "prod", "keep")
	current := &v1.Release{Version: 3, Manifest: removed + "---\n" + kept}
	newest := &v1.Release{Version: 4, Manifest: kept}
	result := upgradeResult(kept, 5)
	client := &fakeBuildClient{
		result: result,
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       newest,
			NextRevision: 5,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("removed", "prod", "web", "old"))
	cluster.store(ownedConfigMap("app", "prod", "web", "keep"))

	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "kind-dev", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Equal(t, 5, p.Header.Revision)
	assert.False(t, p.Header.FreshInstall)
	byName := map[string]semantic.ResourceChange{}
	for _, c := range p.Changes {
		byName[c.Resource.Name] = c
	}
	require.Contains(t, byName, "removed")
	assert.Equal(t, semantic.Delete, byName["removed"].Action)
}

func TestBuildSemanticPlan_FailedOnlyHistoryIsUpgrade(t *testing.T) {
	t.Parallel()
	failed := &v1.Release{Version: 1, Manifest: configMapYAML("app", "prod", "old")}
	result := upgradeResult(configMapYAML("app", "prod", "new"), 2)
	client := &fakeBuildClient{
		result: result,
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      failed,
			Newest:       failed,
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "old"))

	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "kind-dev", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Equal(t, 2, p.Header.Revision)
	assert.False(t, p.Header.FreshInstall)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
}

func TestBuildSemanticPlan_PrepRenderMismatch(t *testing.T) {
	t.Parallel()
	current := &v1.Release{Version: 4, Manifest: configMapYAML("app", "prod", "old")}
	tests := []struct {
		name   string
		prep   helm.ReleasePrep
		result *render.RenderResult
		want   string
	}{
		{
			name:   "prepared install rendered upgrade",
			prep:   helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			result: upgradeResult(configMapYAML("app", "prod", "v1"), 1),
			want:   "prepared install but render used upgrade",
		},
		{
			name: "prepared upgrade rendered install",
			prep: helm.ReleasePrep{
				Operation:    helm.OperationUpgrade,
				Current:      current,
				NextRevision: 5,
			},
			result: installResult(configMapYAML("app", "prod", "v1")),
			want:   "prepared upgrade but render used install",
		},
		{
			name: "revision mismatch",
			prep: helm.ReleasePrep{
				Operation:    helm.OperationUpgrade,
				Current:      current,
				NextRevision: 5,
			},
			result: upgradeResult(configMapYAML("app", "prod", "v1"), 6),
			want:   "rendered revision 6 does not match prepared revision 5",
		},
		{
			name:   "unknown operation",
			prep:   helm.ReleasePrep{NextRevision: 1},
			result: installResult(configMapYAML("app", "prod", "v1")),
			want:   "invalid helm operation",
		},
		{
			name:   "nil render result",
			prep:   helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			result: nil,
			want:   "render result is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{result: tt.result, prep: tt.prep, cleanup: func() {}}
			cluster := newFakeCluster()
			p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolvedSpec(), nil)
			registerCleanup(t, cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, "render preparation:")
			assert.ErrorContains(t, err, tt.want)
			assert.Zero(t, p)
			assert.Nil(t, result)
			assert.Equal(t, 0, cluster.gets)
		})
	}
}

func TestBuildSemanticPlan_InvalidPrep(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result: upgradeResult(configMapYAML("app", "prod", "new"), 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      nil,
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "build prediction input:")
	assert.ErrorContains(t, err, "upgrade prep requires a current release")
	assert.Zero(t, p)
	assert.Nil(t, result)
	assert.Equal(t, 0, cluster.gets)
}

func TestBuildSemanticPlan_PredictFailureLeavesCleanup(t *testing.T) {
	t.Parallel()
	var cleanups int
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() { cleanups++ },
	}
	cluster := newFakeCluster()
	cluster.applyErr = errors.New("ssa dry-run failed")

	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolvedSpec(), nil)
	require.NotNil(t, cleanup)
	t.Cleanup(func() {
		if cleanups == 0 {
			cleanup()
		}
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "predict resources:")
	assert.Zero(t, p)
	assert.Nil(t, result)
	assert.Equal(t, 0, cleanups)
	cleanup()
	assert.Equal(t, 1, cleanups)
}

func TestBuildSemanticPlan_NilCleanup(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: nil,
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes)
}

func TestBuildSemanticPlan_RenderError(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{err: errors.New("chart missing")}
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render manifests:")
	assert.ErrorContains(t, err, "chart missing")
	assert.Zero(t, p)
	assert.Nil(t, result)
}

func TestBuildSemanticPlan_NoChange(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	current := &v1.Release{Version: 3, Manifest: manifest}
	client := &fakeBuildClient{
		result: upgradeResult(manifest, 4),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       current,
			NextRevision: 4,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))

	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	assert.Equal(t, 0, p.Summary.Total())
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	assert.False(t, p.HasEffects())
	assert.True(t, p.IsNoOp())
}

func TestBuildSemanticPlan_InstallHookWillRun(t *testing.T) {
	t.Parallel()
	hook := planHook("migrate", "busybox")
	result := installResult(configMapYAML("app", "prod", "v1"))
	result.Hooks = []*v1.Hook{hook}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	client := &fakeBuildClient{
		result:  result,
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), "ctx", resolved, nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmInstall, p.HelmAction)
	require.Len(t, p.Tasks, 1)
	assert.Equal(t, semantic.TaskCreate, p.Tasks[0].Action)
	assert.True(t, p.Tasks[0].WillRun)
}

func TestBuildSemanticPlan_UnchangedHookNoHelmChange(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	hook := planHook("migrate", "busybox")
	result := upgradeResult(manifest, 4)
	result.Hooks = []*v1.Hook{hook}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	client := &fakeBuildClient{
		result:  result,
		prep:    upgradePrepWithHook(manifest, 3, 4, hook),
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolved, nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	assert.Empty(t, p.Changes)
	require.Len(t, p.Tasks, 1)
	assert.Equal(t, semantic.TaskUnchanged, p.Tasks[0].Action)
	assert.False(t, p.Tasks[0].WillRun)
	assert.True(t, p.IsNoOp())
}

func TestBuildSemanticPlan_HelmChangeUnchangedHookWillRun(t *testing.T) {
	t.Parallel()
	hook := planHook("migrate", "busybox")
	result := upgradeResult(configMapYAML("app", "prod", "new"), 7)
	result.Hooks = []*v1.Hook{hook}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	client := &fakeBuildClient{
		result:  result,
		prep:    upgradePrepWithHook(configMapYAML("app", "prod", "old"), 6, 7, hook),
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "old"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolved, nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.Len(t, p.Changes, 1)
	require.Len(t, p.Tasks, 1)
	assert.Equal(t, semantic.TaskUnchanged, p.Tasks[0].Action)
	assert.True(t, p.Tasks[0].WillRun)
}

func TestBuildSemanticPlan_HookCreateWithoutResourceChange(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	hook := planHook("migrate", "busybox")
	result := upgradeResult(manifest, 4)
	result.Hooks = []*v1.Hook{hook}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	current := &v1.Release{
		Version:  3,
		Manifest: manifest,
	}
	client := &fakeBuildClient{
		result: result,
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       current,
			NextRevision: 4,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolved, nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.Len(t, p.Tasks, 1)
	assert.Equal(t, semantic.TaskCreate, p.Tasks[0].Action)
	assert.True(t, p.Tasks[0].WillRun)
	assert.False(t, p.IsNoOp())
}

func TestBuildSemanticPlan_HookUpdateWithoutResourceChange(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	prev := planHook("migrate", "old")
	desired := planHook("migrate", "new")
	result := upgradeResult(manifest, 4)
	result.Hooks = []*v1.Hook{desired}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	client := &fakeBuildClient{
		result:  result,
		prep:    upgradePrepWithHook(manifest, 3, 4, prev),
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolved, nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.Len(t, p.Tasks, 1)
	assert.Equal(t, semantic.TaskUpdate, p.Tasks[0].Action)
	assert.True(t, p.Tasks[0].WillRun)
}

func TestBuildSemanticPlan_HookDeleteWithoutResourceChange(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	prev := planHook("migrate", "busybox")
	result := upgradeResult(manifest, 4)
	client := &fakeBuildClient{
		result:  result,
		prep:    upgradePrepWithHook(manifest, 3, 4, prev),
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.Len(t, p.Tasks, 1)
	assert.Equal(t, "migrate", p.Tasks[0].Name)
	assert.Equal(t, semantic.TaskDelete, p.Tasks[0].Action)
	assert.False(t, p.Tasks[0].WillRun)
	assert.False(t, p.IsNoOp())
}

func TestBuildSemanticPlan_ScheduleCronJobChange(t *testing.T) {
	t.Parallel()
	previous := cronJobYAML("cleanup", "0 2 * * *")
	desired := cronJobYAML("cleanup", "0 3 * * *")
	current := &v1.Release{Version: 3, Manifest: previous}
	client := &fakeBuildClient{
		result: upgradeResult(desired, 4),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       current,
			NextRevision: 4,
		},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	cluster.store(ownedCronJob("cleanup", "prod", "web", "0 2 * * *"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, "ctx", resolvedSpec(), nil)
	registerCleanup(t, cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, "CronJob", p.Changes[0].Resource.Kind)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
}

func planHook(name, image string) *v1.Hook {
	return &v1.Hook{
		Name:   name,
		Kind:   "Job",
		Weight: 1,
		Events: []v1.HookEvent{v1.HookPreInstall, v1.HookPreUpgrade},
		Manifest: fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s
  namespace: prod
  labels:
    %s: %s
    %s: %s
spec:
  template:
    spec:
      containers:
      - name: job
        image: %s
`, name, spec.LabelComponent, name, spec.LabelTask, name, image),
	}
}

func upgradePrepWithHook(manifest string, currentRevision, nextRevision int, hook *v1.Hook) helm.ReleasePrep {
	current := &v1.Release{
		Version:  currentRevision,
		Manifest: manifest,
		Hooks:    []*v1.Hook{hook},
		Config: map[string]any{
			"deployah": map[string]any{
				"resolved": map[string]any{
					"tasks": map[string]any{
						hook.Name: map[string]any{"on": "preDeploy", "hookWeight": 1},
					},
				},
			},
		},
	}
	return helm.ReleasePrep{
		Operation:    helm.OperationUpgrade,
		Current:      current,
		Newest:       current,
		NextRevision: nextRevision,
	}
}

func cronJobYAML(name, schedule string) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: CronJob
metadata:
  name: %s
  namespace: prod
spec:
  schedule: %q
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: job
            image: busybox
          restartPolicy: OnFailure
`, name, schedule)
}

func ownedCronJob(name, namespace, release, schedule string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]any{
				"app.kubernetes.io/managed-by": "Helm",
			},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      release,
				"meta.helm.sh/release-namespace": namespace,
			},
		},
		"spec": map[string]any{
			"schedule": schedule,
			"jobTemplate": map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "job", "image": "busybox"},
							},
							"restartPolicy": "OnFailure",
						},
					},
				},
			},
		},
	}}
}
