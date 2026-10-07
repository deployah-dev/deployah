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

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
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
	gotCRDs     []extras.RawFile
	calls       int
}

func (f *fakeBuildClient) RenderManifestsWithPrep(
	ctx context.Context,
	_ *spec.ResolvedSpec,
	postRenderer postrenderer.PostRenderer,
	crds []extras.RawFile,
) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	f.calls++
	f.gotCtx = ctx
	f.gotRenderer = postRenderer
	f.gotCRDs = crds
	return f.result, f.prep, f.cleanup, f.err
}

type fakeRESTMapper struct {
	cluster map[schema.GroupKind]bool
	err     map[schema.GroupKind]error
	calls   []schema.GroupVersionKind
}

func newMapper() *fakeRESTMapper {
	return &fakeRESTMapper{
		cluster: map[schema.GroupKind]bool{
			{Kind: "Namespace"}: true,
			{Group: "rbac.authorization.k8s.io", Kind: "ClusterRole"}:         true,
			{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}: true,
		},
		err: map[schema.GroupKind]error{},
	}
}

func (f *fakeRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	version := ""
	if len(versions) > 0 {
		version = versions[0]
	}
	f.calls = append(f.calls, gk.WithVersion(version))
	if err, ok := f.err[gk]; ok {
		return nil, err
	}
	scope := meta.RESTScopeNamespace
	if f.cluster[gk] {
		scope = meta.RESTScopeRoot
	}
	return &meta.RESTMapping{
		Resource:         schema.GroupVersionResource{Group: gk.Group, Version: version, Resource: strings.ToLower(gk.Kind) + "s"},
		GroupVersionKind: gk.WithVersion(version),
		Scope:            scope,
	}, nil
}

func configMapYAML(name, namespace, data string) string {
	ns := ""
	if namespace != "" {
		ns = "\n  namespace: " + namespace
	}
	return fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s%s\ndata:\n  key: %s\n", name, ns, data)
}

func kindYAML(apiVersion, kind, name, namespace, body string) string {
	ns := ""
	if namespace != "" {
		ns = "\n  namespace: " + namespace
	}
	if body != "" {
		body = "\n" + body
	}
	return fmt.Sprintf("apiVersion: %s\nkind: %s\nmetadata:\n  name: %s%s%s\n", apiVersion, kind, name, ns, body)
}

func joinManifests(docs ...string) string {
	return strings.Join(docs, "---\n")
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

func installClient(manifest string) *fakeBuildClient {
	return &fakeBuildClient{
		result:  installResult(manifest),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
}

func upgradeClient(previous, desired string, revision int) *fakeBuildClient {
	current := &v1.Release{Version: revision - 1, Manifest: previous}
	return &fakeBuildClient{
		result: upgradeResult(desired, revision),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       current,
			NextRevision: revision,
		},
		cleanup: func() {},
	}
}

func buildInput(clusterContext string, resolved *spec.ResolvedSpec, post postrenderer.PostRenderer) plan.SemanticBuildInput {
	return plan.SemanticBuildInput{
		ClusterContext: clusterContext,
		Resolved:       resolved,
		PostRenderer:   post,
	}
}

func liveFor(client *fakeBuildClient) plan.LiveReader {
	if client != nil && client.prep.Operation == helm.OperationUpgrade {
		return &fakeLive{}
	}
	return nil
}

func mustBuild(t *testing.T, client *fakeBuildClient, mapper plan.RESTMapper) semantic.Plan {
	t.Helper()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, mapper, liveFor(client), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	return p
}

func changeByName(t *testing.T, changes []semantic.ResourceChange, name string) semantic.ResourceChange {
	t.Helper()
	for _, c := range changes {
		if c.Resource.Name == name || c.Resource.GenerateName == name {
			return c
		}
	}
	t.Fatalf("missing change %q", name)
	return semantic.ResourceChange{}
}

func TestBuildSemanticPlan_RequiresClient(t *testing.T) {
	t.Parallel()
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), nil, newMapper(), nil, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "semantic plan requires a helm client")
	assert.Zero(t, p)
	assert.Nil(t, result)
}

func TestBuildSemanticPlan_RequiresInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mapper  plan.RESTMapper
		in      plan.SemanticBuildInput
		wantErr string
	}{
		{
			name:    "nil resolved",
			mapper:  newMapper(),
			in:      buildInput("ctx", nil, nil),
			wantErr: "semantic plan requires resolved spec; call spec.Resolve first",
		},
		{
			name:    "nil spec",
			mapper:  newMapper(),
			in:      buildInput("ctx", &spec.ResolvedSpec{}, nil),
			wantErr: "semantic plan requires resolved spec; call spec.Resolve first",
		},
		{
			name:    "nil mapper",
			in:      buildInput("ctx", resolvedSpec(), nil),
			wantErr: "semantic plan requires a REST mapper",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{}
			p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, tt.mapper, nil, tt.in)
			t.Cleanup(cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Zero(t, p)
			assert.Nil(t, result)
			assert.Equal(t, 0, client.calls)
		})
	}
}

func TestBuildSemanticPlan_FreshInstallCreatesDesired(t *testing.T) {
	t.Parallel()
	manifest := joinManifests(configMapYAML("app", "prod", "v1"), configMapYAML("extra", "prod", "v1"))
	var post postrenderer.PostRenderer = &identityPostRenderer{}
	var cleanups int
	client := installClient(manifest)
	client.cleanup = func() { cleanups++ }
	ctx := context.WithValue(t.Context(), ctxKey{}, "pipeline")

	p, got, cleanup, err := plan.BuildSemanticPlan(ctx, client, newMapper(), liveFor(client), buildInput("kind-dev", resolvedSpec(), post))
	require.NotNil(t, cleanup)
	t.Cleanup(func() {
		if cleanups == 0 {
			cleanup()
		}
	})
	require.NoError(t, err)
	assert.Same(t, client.result, got)
	assert.Equal(t, post, client.gotRenderer)
	assert.Equal(t, "pipeline", client.gotCtx.Value(ctxKey{}))
	assert.Equal(t, "shop", p.Header.Project)
	assert.Equal(t, "prod", p.Header.Environment)
	assert.Equal(t, "web", p.Header.Release)
	assert.Equal(t, "prod", p.Header.Namespace)
	assert.Equal(t, "kind-dev", p.Header.Context)
	assert.Equal(t, 1, p.Header.Revision)
	assert.True(t, p.Header.FreshInstall)
	assert.Equal(t, semantic.HelmInstall, p.HelmAction)
	require.Len(t, p.Changes, 2)
	for _, c := range p.Changes {
		assert.Equal(t, semantic.Create, c.Action)
		assert.Nil(t, c.Before)
		require.NotNil(t, c.After)
		assert.Empty(t, c.Fields)
		_, found, nestErr := unstructured.NestedFieldNoCopy(c.After.Object, "metadata", "annotations")
		require.NoError(t, nestErr)
		assert.False(t, found)
	}
	assert.Equal(t, 0, cleanups)
	cleanup()
	assert.Equal(t, 1, cleanups)
}

func TestBuildSemanticPlan_NoResourceChange(t *testing.T) {
	t.Parallel()
	same := configMapYAML("app", "prod", "same")
	bookkeeping := strings.Replace(same, "metadata:\n", "metadata:\n  creationTimestamp: null\n", 1)
	bookkeeping += "status:\n  observedGeneration: 1\n"
	tests := []struct {
		name     string
		previous string
		desired  string
		wantHelm semantic.HelmAction
		wantNoOp bool
	}{
		{
			name:     "identical release",
			previous: same,
			desired:  same,
			wantHelm: semantic.HelmNone,
			wantNoOp: true,
		},
		{
			name:     "bookkeeping only",
			previous: same,
			desired:  bookkeeping,
			wantHelm: semantic.HelmUpgrade,
		},
		{
			name:     "omitted namespace matches explicit release namespace",
			previous: configMapYAML("app", "", "v1"),
			desired:  configMapYAML("app", "prod", "v1"),
			wantHelm: semantic.HelmUpgrade,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustBuild(t, upgradeClient(tt.previous, tt.desired, 4), newMapper())
			assert.Equal(t, tt.wantHelm, p.HelmAction)
			assert.Empty(t, p.Changes)
			assert.False(t, p.HasEffects())
			assert.Equal(t, tt.wantNoOp, p.IsNoOp())
		})
	}
}

func TestBuildSemanticPlan_FieldChangeIsUpdate(t *testing.T) {
	t.Parallel()
	const revision = 7
	client := upgradeClient(configMapYAML("app", "prod", "old"), configMapYAML("app", "prod", "new"), revision)
	p := mustBuild(t, client, newMapper())
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	assert.Equal(t, revision, p.Header.Revision)
	assert.False(t, p.Header.FreshInstall)
	require.Len(t, p.Changes, 1)
	c := p.Changes[0]
	assert.Equal(t, semantic.Update, c.Action)
	assert.Equal(t, "v1", c.Resource.APIVersion)
	require.NotNil(t, c.Before)
	require.NotNil(t, c.After)
	beforeKey, found, err := unstructured.NestedString(c.Before.Object, "data", "key")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "old", beforeKey)
	afterKey, found, err := unstructured.NestedString(c.After.Object, "data", "key")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "new", afterKey)
	require.Len(t, c.Fields, 1)
	assert.Equal(t, "/data/key", c.Fields[0].Path)
	assert.Equal(t, semantic.FieldReplace, c.Fields[0].Op)
	assert.Equal(t, "old", c.Fields[0].Before)
	assert.Equal(t, "new", c.Fields[0].After)

	again := mustBuild(t, upgradeClient(configMapYAML("app", "prod", "old"), configMapYAML("app", "prod", "new"), revision), newMapper())
	assert.Equal(t, p.Changes[0].Fields, again.Changes[0].Fields)
}

func TestBuildSemanticPlan_AddedAndRemoved(t *testing.T) {
	t.Parallel()
	goneKeep := configMapYAML("gone", "prod", "v1")
	goneKeep = strings.Replace(goneKeep, "metadata:\n", "metadata:\n  annotations:\n    helm.sh/resource-policy: keep\n", 1)
	previous := joinManifests(configMapYAML("kept", "prod", "v1"), goneKeep)
	desired := joinManifests(configMapYAML("kept", "prod", "v1"), configMapYAML("added", "prod", "v1"))
	p := mustBuild(t, upgradeClient(previous, desired, 4), newMapper())
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	added := changeByName(t, p.Changes, "added")
	assert.Equal(t, semantic.Create, added.Action)
	assert.Nil(t, added.Before)
	gone := changeByName(t, p.Changes, "gone")
	assert.Equal(t, semantic.Delete, gone.Action)
	assert.Nil(t, gone.After)
	policy, found, err := unstructured.NestedString(gone.Before.Object, "metadata", "annotations", "helm.sh/resource-policy")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "keep", policy)
	for _, c := range p.Changes {
		assert.NotEqual(t, "kept", c.Resource.Name)
	}
}

func TestBuildSemanticPlan_APIVersionScopeAndSnapshots(t *testing.T) {
	t.Parallel()
	hpa := func(version string, replicas int) string {
		return fmt.Sprintf(`apiVersion: autoscaling/%s
kind: HorizontalPodAutoscaler
metadata:
  name: app
  namespace: prod
spec:
  maxReplicas: %d
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: app
`, version, replicas)
	}
	t.Run("apiVersion only", func(t *testing.T) {
		t.Parallel()
		p := mustBuild(t, upgradeClient(hpa("v2beta2", 2), hpa("v2", 2), 4), newMapper())
		require.Len(t, p.Changes, 1)
		c := p.Changes[0]
		assert.Equal(t, semantic.Update, c.Action)
		assert.Equal(t, "autoscaling/v2", c.Resource.APIVersion)
		assert.Equal(t, "prod", c.Resource.Namespace)
		require.Len(t, c.Fields, 1)
		assert.Equal(t, "/apiVersion", c.Fields[0].Path)
		assert.Equal(t, "autoscaling/v2beta2", c.Fields[0].Before)
		assert.Equal(t, "autoscaling/v2", c.Fields[0].After)
	})
	t.Run("other field ignores injected namespace", func(t *testing.T) {
		t.Parallel()
		p := mustBuild(t, upgradeClient(configMapYAML("app", "", "old"), configMapYAML("app", "prod", "new"), 4), newMapper())
		require.Len(t, p.Changes, 1)
		c := p.Changes[0]
		assert.Equal(t, "prod", c.Resource.Namespace)
		require.Len(t, c.Fields, 1)
		assert.Equal(t, "/data/key", c.Fields[0].Path)
		_, beforeFound, err := unstructured.NestedFieldNoCopy(c.Before.Object, "metadata", "namespace")
		require.NoError(t, err)
		_, afterFound, err := unstructured.NestedFieldNoCopy(c.After.Object, "metadata", "namespace")
		require.NoError(t, err)
		assert.False(t, beforeFound)
		assert.True(t, afterFound)
	})
	t.Run("cluster scope ignores identical stray namespace", func(t *testing.T) {
		t.Parallel()
		role := func(ns, rule string) string {
			return fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: view
  namespace: %s
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["%s"]
`, ns, rule)
		}
		same := mustBuild(t, upgradeClient(role("stray", "get"), role("stray", "get"), 4), newMapper())
		assert.Empty(t, same.Changes)
		changed := mustBuild(t, upgradeClient(role("stray", "get"), role("stray", "list"), 4), newMapper())
		require.Len(t, changed.Changes, 1)
		c := changed.Changes[0]
		assert.Empty(t, c.Resource.Namespace)
		beforeNS, _, err := unstructured.NestedString(c.Before.Object, "metadata", "namespace")
		require.NoError(t, err)
		afterNS, _, err := unstructured.NestedString(c.After.Object, "metadata", "namespace")
		require.NoError(t, err)
		assert.Equal(t, "stray", beforeNS)
		assert.Equal(t, "stray", afterNS)
		for _, f := range c.Fields {
			assert.NotEqual(t, "/metadata/namespace", f.Path)
		}
	})
	t.Run("cluster scope namespace change is a field change", func(t *testing.T) {
		t.Parallel()
		role := func(ns string) string {
			return fmt.Sprintf(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: view
  namespace: %s
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get"]
`, ns)
		}
		p := mustBuild(t, upgradeClient(role("one"), role("two"), 4), newMapper())
		require.Len(t, p.Changes, 1)
		c := p.Changes[0]
		assert.Equal(t, semantic.Update, c.Action)
		assert.Empty(t, c.Resource.Namespace)
		require.Len(t, c.Fields, 1)
		assert.Equal(t, "/metadata/namespace", c.Fields[0].Path)
	})
	t.Run("different namespaces are different resources", func(t *testing.T) {
		t.Parallel()
		p := mustBuild(t, upgradeClient(configMapYAML("app", "prod", "v1"), configMapYAML("app", "other", "v1"), 4), newMapper())
		require.Len(t, p.Changes, 2)
		var actions []semantic.Action
		for _, c := range p.Changes {
			actions = append(actions, c.Action)
			assert.Equal(t, "app", c.Resource.Name)
		}
		assert.ElementsMatch(t, []semantic.Action{semantic.Delete, semantic.Create}, actions)
	})
	t.Run("create snapshot is the declaration", func(t *testing.T) {
		t.Parallel()
		declared := configMapYAML("app", "", "v1")
		p := mustBuild(t, installClient(declared), newMapper())
		require.Len(t, p.Changes, 1)
		c := p.Changes[0]
		assert.Equal(t, "prod", c.Resource.Namespace)
		_, found, err := unstructured.NestedFieldNoCopy(c.After.Object, "metadata", "namespace")
		require.NoError(t, err)
		assert.False(t, found)
		value, found, err := unstructured.NestedString(c.After.Object, "data", "key")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "v1", value)
	})
}

func TestBuildSemanticPlan_Tasks(t *testing.T) {
	t.Parallel()
	t.Run("unchanged release", func(t *testing.T) {
		t.Parallel()
		manifest := configMapYAML("app", "prod", "same")
		cron := cronJobYAML("cleanup", "0 3 * * *")
		manifest = joinManifests(manifest, cron)
		pre := planHook("migrate", "busybox")
		post := planHook("smoke", "busybox")
		post.Events = []v1.HookEvent{v1.HookPostInstall, v1.HookPostUpgrade}
		client := upgradeClient(manifest, manifest, 4)
		client.result.Hooks = []*v1.Hook{pre, post}
		client.prep.Current.Hooks = []*v1.Hook{pre, post}
		client.prep.Current.Config = map[string]any{
			"deployah": map[string]any{
				"resolved": map[string]any{
					"tasks": map[string]any{
						"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1},
						"smoke":   map[string]any{"on": "postDeploy", "hookWeight": 2},
						"cleanup": map[string]any{"on": "schedule"},
					},
				},
			},
		}
		resolved := resolvedSpec()
		resolved.Tasks = map[string]spec.ResolvedTask{
			"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
			"smoke":   {Task: spec.Task{On: spec.TaskOnPostDeploy}, HookWeight: 2},
			"cleanup": {Task: spec.Task{On: spec.TaskOnSchedule}},
			"manual":  {Task: spec.Task{On: spec.TaskOnManual}},
		}
		p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolved, nil))
		t.Cleanup(cleanup)
		require.NoError(t, err)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		assert.Empty(t, p.Changes)
		require.Len(t, p.Tasks, 2)
		byName := map[string]semantic.TaskPlan{}
		for _, task := range p.Tasks {
			byName[task.Name] = task
			assert.Equal(t, semantic.TaskUnchanged, task.Action)
			assert.False(t, task.WillRun)
		}
		assert.Equal(t, semantic.TaskPreDeploy, byName["migrate"].Phase)
		assert.Equal(t, semantic.TaskPostDeploy, byName["smoke"].Phase)
		assert.NotContains(t, byName, "cleanup")
		assert.NotContains(t, byName, "manual")
	})
	t.Run("changed cronjob", func(t *testing.T) {
		t.Parallel()
		resolved := resolvedSpec()
		resolved.Tasks = map[string]spec.ResolvedTask{
			"cleanup": {Task: spec.Task{On: spec.TaskOnSchedule}},
		}
		client := upgradeClient(cronJobYAML("cleanup", "0 2 * * *"), cronJobYAML("cleanup", "0 3 * * *"), 4)
		client.prep.Current.Config = map[string]any{
			"deployah": map[string]any{
				"resolved": map[string]any{
					"tasks": map[string]any{
						"cleanup": map[string]any{"on": string(spec.TaskOnSchedule)},
					},
				},
			},
		}
		p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolved, nil))
		t.Cleanup(cleanup)
		require.NoError(t, err)
		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Update, p.Changes[0].Action)
		require.Len(t, p.Tasks, 1)
		assert.Equal(t, semantic.TaskSchedule, p.Tasks[0].Phase)
		assert.Equal(t, semantic.TaskUpdate, p.Tasks[0].Action)
		require.Len(t, p.Tasks[0].Resources, 1)
		assert.Equal(t, "cleanup", p.Tasks[0].Resources[0].Name)
	})
}

func TestBuildSemanticPlan_HelmOrderFollowsUpgradeSequence(t *testing.T) {
	t.Parallel()
	previous := joinManifests(
		kindYAML("apps/v1", "Deployment", "api", "prod", "spec:\n  replicas: 1"),
		kindYAML("v1", "Service", "api", "prod", "spec:\n  ports:\n  - port: 80"),
		configMapYAML("gone", "prod", "old"),
		kindYAML("v1", "ServiceAccount", "gone", "prod", ""),
	)
	desired := joinManifests(
		kindYAML("v1", "Service", "api", "prod", "spec:\n  ports:\n  - port: 81"),
		configMapYAML("first", "prod", "v1"),
		configMapYAML("second", "prod", "v1"),
		kindYAML("apps/v1", "Deployment", "api", "prod", "spec:\n  replicas: 2"),
	)
	p := mustBuild(t, upgradeClient(previous, desired, 4), newMapper())
	var got []string
	for _, c := range p.Changes {
		got = append(got, c.Resource.Kind+"/"+c.Resource.Name+"/"+c.Action.String())
	}
	assert.Equal(t, []string{
		"ConfigMap/first/create",
		"ConfigMap/second/create",
		"Service/api/update",
		"Deployment/api/update",
		"ConfigMap/gone/delete",
		"ServiceAccount/gone/delete",
	}, got)
}

func TestBuildSemanticPlan_TargetNamespaceOwnership(t *testing.T) {
	t.Parallel()
	ns := func(name string) string {
		return kindYAML("v1", "Namespace", name, "", "")
	}
	tests := []struct {
		name     string
		install  bool
		previous string
		desired  string
		wantErr  string
	}{
		{
			name:    "desired on install",
			install: true,
			desired: joinManifests(configMapYAML("app", "prod", "v1"), ns("prod")),
			wantErr: "rendered manifest declares target namespace",
		},
		{
			name:     "desired on upgrade",
			previous: configMapYAML("app", "prod", "v1"),
			desired:  joinManifests(configMapYAML("app", "prod", "v1"), ns("prod")),
			wantErr:  "rendered manifest declares target namespace",
		},
		{
			name:     "previous on upgrade",
			previous: joinManifests(configMapYAML("app", "prod", "v1"), ns("prod")),
			desired:  configMapYAML("app", "prod", "v1"),
			wantErr:  "previous release revision 3 declares target namespace",
		},
		{
			name:     "both sides",
			previous: ns("prod"),
			desired:  ns("prod"),
			wantErr:  "rendered manifest declares target namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var client *fakeBuildClient
			if tt.install {
				client = installClient(tt.desired)
			} else {
				client = upgradeClient(tt.previous, tt.desired, 4)
			}
			_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolvedSpec(), nil))
			t.Cleanup(cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.ErrorContains(t, err, "must not claim it")
		})
	}
}

func TestBuildSemanticPlan_OtherNamespaceIsAChange(t *testing.T) {
	t.Parallel()
	install := mustBuild(t, installClient(kindYAML("v1", "Namespace", "extra", "", "")), newMapper())
	require.Len(t, install.Changes, 1)
	assert.Equal(t, semantic.Create, install.Changes[0].Action)
	assert.Equal(t, "Namespace", install.Changes[0].Resource.Kind)
	assert.Equal(t, "extra", install.Changes[0].Resource.Name)
	assert.Empty(t, install.Changes[0].Resource.Namespace)

	upgrade := mustBuild(t, upgradeClient(
		kindYAML("v1", "Namespace", "extra", "", ""),
		kindYAML("v1", "Namespace", "other", "", ""),
		4,
	), newMapper())
	var actions []string
	for _, c := range upgrade.Changes {
		actions = append(actions, c.Resource.Name+"/"+c.Action.String())
	}
	assert.ElementsMatch(t, []string{"extra/delete", "other/create"}, actions)
}

func TestBuildSemanticPlan_PairingErrors(t *testing.T) {
	t.Parallel()
	dup := joinManifests(configMapYAML("app", "prod", "a"), configMapYAML("app", "prod", "b"))
	both := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  generateName: app-
  namespace: prod
data:
  key: v1
`
	gen := `apiVersion: v1
kind: ConfigMap
metadata:
  generateName: app-
  namespace: prod
data:
  key: %s
`
	tests := []struct {
		name     string
		install  bool
		previous string
		desired  string
		wantErr  string
	}{
		{name: "duplicate desired", install: true, desired: dup, wantErr: "rendered manifest: duplicate resource"},
		{name: "duplicate previous", previous: dup, desired: configMapYAML("other", "prod", "v1"), wantErr: "previous release revision 3: duplicate resource"},
		{name: "name and generateName", install: true, desired: both, wantErr: "metadata.name and metadata.generateName cannot both be set"},
		{name: "duplicate generateName", install: true, desired: joinManifests(fmt.Sprintf(gen, "a"), fmt.Sprintf(gen, "b")), wantErr: "ambiguous generateName pairing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var client *fakeBuildClient
			if tt.install {
				client = installClient(tt.desired)
			} else {
				client = upgradeClient(tt.previous, tt.desired, 4)
			}
			_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolvedSpec(), nil))
			t.Cleanup(cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestBuildSemanticPlan_GenerateNamePairing(t *testing.T) {
	t.Parallel()
	gen := func(value string) string {
		return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  generateName: app-
  namespace: prod
data:
  key: %s
`, value)
	}
	tests := []struct {
		name   string
		client *fakeBuildClient
		want   []semantic.Action
	}{
		{name: "unchanged", client: upgradeClient(gen("v1"), gen("v1"), 4)},
		{name: "changed", client: upgradeClient(gen("old"), gen("new"), 4), want: []semantic.Action{semantic.Update}},
		{name: "desired only", client: installClient(gen("v1")), want: []semantic.Action{semantic.Create}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustBuild(t, tt.client, newMapper())
			require.Len(t, p.Changes, len(tt.want))
			for i, action := range tt.want {
				assert.Equal(t, action, p.Changes[i].Action)
				assert.Empty(t, p.Changes[i].Resource.Name)
				assert.Equal(t, "app-", p.Changes[i].Resource.GenerateName)
				assert.Equal(t, "prod", p.Changes[i].Resource.Namespace)
			}
		})
	}
}

func TestBuildSemanticPlan_PreviousMappingFailure(t *testing.T) {
	t.Parallel()
	widget := `apiVersion: example.com/v1
kind: Widget
metadata:
  name: app
  namespace: prod
spec:
  color: blue
`
	mapper := newMapper()
	gk := schema.GroupKind{Group: "example.com", Kind: "Widget"}
	mapper.err[gk] = &meta.NoKindMatchError{GroupKind: gk}
	client := upgradeClient(widget, configMapYAML("app", "prod", "v1"), 4)
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, mapper, liveFor(client), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "previous release revision 3: resolve resource mapping")
	assert.ErrorContains(t, err, "Widget")
}

func TestBuildSemanticPlan_UnknownCustomResource(t *testing.T) {
	t.Parallel()
	widget := `apiVersion: example.com/v1
kind: Widget
metadata:
  name: app
  namespace: prod
spec:
  color: blue
`
	mapper := newMapper()
	gk := schema.GroupKind{Group: "example.com", Kind: "Widget"}
	mapper.err[gk] = &meta.NoKindMatchError{GroupKind: gk}
	client := installClient(widget)
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{{Path: "widgets.yaml", Raw: []byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\n")}}
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, mapper, liveFor(client), in)
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "rendered manifest: resolve resource mapping")
	assert.NotContains(t, err.Error(), "widgets.yaml")
	require.NotEmpty(t, mapper.calls)
}

func TestBuildSemanticPlan_PreviousUsesCurrentNotNewest(t *testing.T) {
	t.Parallel()
	removed := configMapYAML("removed", "prod", "old")
	kept := configMapYAML("app", "prod", "keep")
	current := &v1.Release{Version: 3, Manifest: joinManifests(removed, kept)}
	newest := &v1.Release{Version: 4, Manifest: kept}
	client := &fakeBuildClient{
		result: upgradeResult(kept, 5),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       newest,
			NextRevision: 5,
		},
		cleanup: func() {},
	}
	p := mustBuild(t, client, newMapper())
	assert.Equal(t, 5, p.Header.Revision)
	gone := changeByName(t, p.Changes, "removed")
	assert.Equal(t, semantic.Delete, gone.Action)
}

func TestBuildSemanticPlan_PrepRenderMismatch(t *testing.T) {
	t.Parallel()
	current := &v1.Release{Version: 4, Manifest: configMapYAML("app", "prod", "old")}
	tests := []struct {
		name   string
		prep   helm.ReleasePrep
		result *render.RenderResult
		prefix string
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
		{
			name: "upgrade without current release",
			prep: helm.ReleasePrep{
				Operation:    helm.OperationUpgrade,
				NextRevision: 2,
			},
			result: upgradeResult(configMapYAML("app", "prod", "new"), 2),
			prefix: "determine helm release intent:",
			want:   "upgrade prep requires a current release",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{result: tt.result, prep: tt.prep, cleanup: func() {}}
			p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolvedSpec(), nil))
			t.Cleanup(cleanup)
			require.Error(t, err)
			prefix := tt.prefix
			if prefix == "" {
				prefix = "render preparation:"
			}
			assert.ErrorContains(t, err, prefix)
			assert.ErrorContains(t, err, tt.want)
			assert.Zero(t, p)
			assert.Nil(t, result)
		})
	}
}

func TestBuildSemanticPlan_MappingFailureLeavesCleanup(t *testing.T) {
	t.Parallel()
	var cleanups int
	client := installClient(configMapYAML("app", "prod", "v1"))
	client.cleanup = func() { cleanups++ }
	mapper := newMapper()
	mapper.err[schema.GroupKind{Kind: "ConfigMap"}] = errors.New("discovery failed")
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, mapper, liveFor(client), buildInput("ctx", resolvedSpec(), nil))
	require.NotNil(t, cleanup)
	t.Cleanup(func() {
		if cleanups == 0 {
			cleanup()
		}
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "rendered manifest: resolve resource mapping")
	assert.Zero(t, p)
	assert.Nil(t, result)
	assert.Equal(t, 0, cleanups)
	cleanup()
	assert.Equal(t, 1, cleanups)
}

func TestBuildSemanticPlan_NilCleanup(t *testing.T) {
	t.Parallel()
	client := installClient(configMapYAML("app", "prod", "v1"))
	client.cleanup = nil
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolvedSpec(), nil))
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes)
}

func TestBuildSemanticPlan_RenderError(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{err: errors.New("chart missing")}
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), liveFor(client), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "render manifests:")
	assert.ErrorContains(t, err, "chart missing")
	assert.Zero(t, p)
	assert.Nil(t, result)
}

func TestBuildSemanticPlan_Hooks(t *testing.T) {
	t.Parallel()
	same := configMapYAML("app", "prod", "same")
	hook := planHook("migrate", "busybox")
	install := installClient(configMapYAML("app", "prod", "v1"))
	install.result.Hooks = []*v1.Hook{hook}
	hookOnly := upgradeClient(same, same, 4)
	hookOnly.result.Hooks = []*v1.Hook{planHook("migrate", "busybox")}
	migrate := map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	tests := []struct {
		name        string
		client      *fakeBuildClient
		tasks       map[string]spec.ResolvedTask
		wantHelm    semantic.HelmAction
		wantChanges int
		wantAction  semantic.TaskAction
		wantWillRun bool
		wantNoOp    bool
	}{
		{
			name:        "install hook will run",
			client:      install,
			tasks:       migrate,
			wantHelm:    semantic.HelmInstall,
			wantChanges: 1,
			wantAction:  semantic.TaskCreate,
			wantWillRun: true,
		},
		{
			name:        "new hook without a resource change",
			client:      hookOnly,
			tasks:       migrate,
			wantHelm:    semantic.HelmUpgrade,
			wantAction:  semantic.TaskCreate,
			wantWillRun: true,
		},
		{
			name:       "unchanged hook and no helm change",
			client:     upgradePrepWithHook(same, same, 3, 4, hook, hook),
			tasks:      migrate,
			wantHelm:   semantic.HelmNone,
			wantAction: semantic.TaskUnchanged,
			wantNoOp:   true,
		},
		{
			name:        "helm change runs an unchanged hook",
			client:      upgradePrepWithHook(configMapYAML("app", "prod", "old"), configMapYAML("app", "prod", "new"), 6, 7, hook, hook),
			tasks:       migrate,
			wantHelm:    semantic.HelmUpgrade,
			wantChanges: 1,
			wantAction:  semantic.TaskUnchanged,
			wantWillRun: true,
		},
		{
			name:        "hook update without a resource change",
			client:      upgradePrepWithHook(same, same, 3, 4, planHook("migrate", "old"), planHook("migrate", "new")),
			tasks:       migrate,
			wantHelm:    semantic.HelmUpgrade,
			wantAction:  semantic.TaskUpdate,
			wantWillRun: true,
		},
		{
			name:       "hook delete without a resource change",
			client:     upgradePrepWithHook(same, same, 3, 4, planHook("migrate", "busybox"), nil),
			wantHelm:   semantic.HelmUpgrade,
			wantAction: semantic.TaskDelete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resolved := resolvedSpec()
			resolved.Tasks = tt.tasks
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), tt.client, newMapper(), liveFor(tt.client), buildInput("ctx", resolved, nil))
			t.Cleanup(cleanup)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHelm, p.HelmAction)
			assert.Len(t, p.Changes, tt.wantChanges)
			require.Len(t, p.Tasks, 1)
			assert.Equal(t, "migrate", p.Tasks[0].Name)
			assert.Equal(t, tt.wantAction, p.Tasks[0].Action)
			assert.Equal(t, tt.wantWillRun, p.Tasks[0].WillRun)
			assert.Equal(t, tt.wantNoOp, p.IsNoOp())
		})
	}
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

func upgradePrepWithHook(previous, desired string, currentRevision, nextRevision int, prev, desiredHook *v1.Hook) *fakeBuildClient {
	var currentHooks []*v1.Hook
	if prev != nil {
		currentHooks = []*v1.Hook{prev}
	}
	current := &v1.Release{
		Version:  currentRevision,
		Manifest: previous,
		Hooks:    currentHooks,
		Config: map[string]any{
			"deployah": map[string]any{
				"resolved": map[string]any{
					"tasks": map[string]any{
						"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1},
					},
				},
			},
		},
	}
	var hooks []*v1.Hook
	if desiredHook != nil {
		hooks = []*v1.Hook{desiredHook}
	}
	result := upgradeResult(desired, nextRevision)
	result.Hooks = hooks
	return &fakeBuildClient{
		result: result,
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			Newest:       current,
			NextRevision: nextRevision,
		},
		cleanup: func() {},
	}
}

func cronJobYAML(name, schedule string) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: CronJob
metadata:
  name: %s
  namespace: prod
  labels:
    %s: %s
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
`, name, spec.LabelTask, name, schedule)
}
