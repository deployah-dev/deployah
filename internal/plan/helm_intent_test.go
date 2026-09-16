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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestBuildSemanticPlan_HelmMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		install    bool
		previous   string
		desired    string
		lives      []*unstructured.Unstructured
		wantAction semantic.Action
		wantName   string
		wantGen    string
	}{
		{
			name:       "install live absent is create",
			install:    true,
			desired:    configMapYAML("app", "prod", "v1"),
			wantAction: semantic.Create,
			wantName:   "app",
		},
		{
			name:       "install live present is update",
			install:    true,
			desired:    configMapYAML("app", "prod", "v1"),
			lives:      []*unstructured.Unstructured{unownedConfigMap("app", "prod", "old")},
			wantAction: semantic.Update,
			wantName:   "app",
		},
		{
			name:       "upgrade field change is live to desired",
			previous:   configMapYAML("app", "prod", "old"),
			desired:    configMapYAML("app", "prod", "new"),
			lives:      []*unstructured.Unstructured{ownedConfigMap("app", "prod", "web", "old")},
			wantAction: semantic.Update,
			wantName:   "app",
		},
		{
			name:     "prune delete when live exists",
			previous: configMapYAML("app", "prod", "v1") + "---\n" + configMapYAML("old", "prod", "x"),
			desired:  configMapYAML("app", "prod", "v1"),
			lives: []*unstructured.Unstructured{
				ownedConfigMap("old", "prod", "web", "x"),
				ownedConfigMap("app", "prod", "web", "v1"),
			},
			wantAction: semantic.Delete,
			wantName:   "old",
		},
		{
			name:       "generateName create with empty name",
			install:    true,
			desired:    generateNameYAML("app-"),
			wantAction: semantic.Create,
			wantGen:    "app-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cluster := readyCluster()
			for _, live := range tt.lives {
				cluster.store(live)
			}
			p := buildPlan(t, cluster, tt.install, tt.previous, tt.desired, nil)
			assertReadOnly(t, cluster)
			got := helmChangeNamed(p, tt.wantName, tt.wantGen)
			require.NotNil(t, got)
			assert.Equal(t, semantic.OriginHelm, got.Origin.Kind)
			assert.Equal(t, tt.wantAction, got.Action)
			assert.Equal(t, tt.wantName, got.Resource.Name)
			assert.Equal(t, tt.wantGen, got.Resource.GenerateName)
		})
	}
}

func TestBuildSemanticPlan_PruneAlreadyAbsent(t *testing.T) {
	t.Parallel()
	cluster := readyCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "v1"))
	p := buildPlan(t, cluster, false,
		configMapYAML("app", "prod", "v1")+"---\n"+configMapYAML("old", "prod", "x"),
		configMapYAML("app", "prod", "v1"),
		nil,
	)
	assertReadOnly(t, cluster)
	assert.False(t, hasResourceNamed(p, "old"))
}

func TestBuildSemanticPlan_KeepPolicy(t *testing.T) {
	t.Parallel()
	hook := planHook("migrate", "busybox")
	desired := configMapYAML("app", "prod", "v1")
	tests := []struct {
		name       string
		previous   string
		kept       *unstructured.Unstructured
		wantDelete bool
	}{
		{
			name:       "live keep skips delete",
			previous:   configMapYAML("app", "prod", "v1") + "---\n" + configMapYAML("kept", "prod", "x"),
			kept:       withKeep(ownedConfigMap("kept", "prod", "web", "x")),
			wantDelete: false,
		},
		{
			name:       "live without keep deletes",
			previous:   configMapYAML("app", "prod", "v1") + "---\n" + configMapYAML("kept", "prod", "x"),
			kept:       ownedConfigMap("kept", "prod", "web", "x"),
			wantDelete: true,
		},
		{
			name:       "previous keep does not hide delete",
			previous:   configMapYAML("app", "prod", "v1") + "---\n" + keepPolicyYAML("kept"),
			kept:       ownedConfigMap("kept", "prod", "web", "x"),
			wantDelete: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cluster := readyCluster()
			cluster.store(ownedConfigMap("app", "prod", "web", "v1"))
			cluster.store(tt.kept)
			p := buildUpgradeWithHook(t, cluster, tt.previous, desired, hook)
			assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
			assert.False(t, p.IsNoOp())
			assert.Equal(t, tt.wantDelete, hasActionNamed(p, "kept", semantic.Delete))
			require.NotEmpty(t, p.Tasks)
			assert.True(t, p.Tasks[0].WillRun)
		})
	}
}

func TestBuildSemanticPlan_UnstampedDesiredVsStampedPrevious(t *testing.T) {
	t.Parallel()
	desired := configMapYAML("app", "prod", "v1")
	stamped := ownedConfigMap("app", "prod", "web", "v1")
	previous, err := toYAMLString(stamped)
	require.NoError(t, err)
	cluster := readyCluster()
	cluster.store(stamped)
	p := buildPlan(t, cluster, false, previous, desired, nil)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.OriginHelm, c.Origin.Kind)
	}
}

func TestBuildSemanticPlan_ThreeWay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		previous      string
		desired       string
		appLive       string
		lives         []*unstructured.Unstructured
		wantHelm      semantic.HelmAction
		wantNoOp      bool
		wantAppFrom   string
		wantAppTo     string
		wantDriftKind semantic.DriftKind
		wantDriftFrom string
		wantDriftTo   string
	}{
		{
			name:        "2/2/3 config change",
			previous:    configMapYAML("app", "prod", "2"),
			desired:     configMapYAML("app", "prod", "3"),
			appLive:     "2",
			wantHelm:    semantic.HelmUpgrade,
			wantAppFrom: "2",
			wantAppTo:   "3",
		},
		{
			name:          "2/4/3 visible live to desired plus drift",
			previous:      configMapYAML("app", "prod", "2"),
			desired:       configMapYAML("app", "prod", "3"),
			appLive:       "4",
			wantHelm:      semantic.HelmUpgrade,
			wantAppFrom:   "4",
			wantAppTo:     "3",
			wantDriftKind: semantic.DriftModified,
			wantDriftFrom: "2",
			wantDriftTo:   "4",
		},
		{
			name:          "2/4/2 helm none plus drift",
			previous:      configMapYAML("app", "prod", "2"),
			desired:       configMapYAML("app", "prod", "2"),
			appLive:       "4",
			wantHelm:      semantic.HelmNone,
			wantNoOp:      true,
			wantDriftKind: semantic.DriftModified,
			wantDriftFrom: "2",
			wantDriftTo:   "4",
		},
		{
			name:     "2/4/2 plus other resource upgrades",
			previous: configMapYAML("app", "prod", "2") + "---\n" + configMapYAML("other", "prod", "a"),
			desired:  configMapYAML("app", "prod", "2") + "---\n" + configMapYAML("other", "prod", "b"),
			appLive:  "4",
			lives: []*unstructured.Unstructured{
				ownedConfigMap("other", "prod", "web", "a"),
			},
			wantHelm:      semantic.HelmUpgrade,
			wantAppFrom:   "4",
			wantAppTo:     "2",
			wantDriftKind: semantic.DriftModified,
			wantDriftFrom: "2",
			wantDriftTo:   "4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cluster := readyCluster()
			cluster.store(ownedConfigMap("app", "prod", "web", tt.appLive))
			for _, live := range tt.lives {
				cluster.store(live)
			}
			p := buildPlan(t, cluster, false, tt.previous, tt.desired, nil)
			assert.Equal(t, tt.wantHelm, p.HelmAction)
			assert.Equal(t, tt.wantNoOp, p.IsNoOp())
			from, to := fieldValues(helmFieldsNamed(p, "app"), "/data/key")
			assert.Equal(t, tt.wantAppFrom, from)
			assert.Equal(t, tt.wantAppTo, to)
			kind, dFrom, dTo := firstDriftKey(p)
			assert.Equal(t, tt.wantDriftKind, kind)
			assert.Equal(t, tt.wantDriftFrom, dFrom)
			assert.Equal(t, tt.wantDriftTo, dTo)
		})
	}
}

func TestBuildSemanticPlan_MissingDrift(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "v1")
	tests := []struct {
		name          string
		previous      string
		desired       string
		lives         []*unstructured.Unstructured
		hook          *v1.Hook
		wantHelm      semantic.HelmAction
		wantNoOp      bool
		wantAppAction semantic.Action
		wantWillRun   bool
	}{
		{
			name:     "unchanged spec missing live",
			previous: manifest,
			desired:  manifest,
			wantHelm: semantic.HelmNone,
			wantNoOp: true,
		},
		{
			name:     "other upgrade creates missing identity",
			previous: manifest + "---\n" + configMapYAML("other", "prod", "a"),
			desired:  manifest + "---\n" + configMapYAML("other", "prod", "b"),
			lives: []*unstructured.Unstructured{
				ownedConfigMap("other", "prod", "web", "a"),
			},
			hook:          planHook("migrate", "busybox"),
			wantHelm:      semantic.HelmUpgrade,
			wantAppAction: semantic.Create,
			wantWillRun:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cluster := readyCluster()
			for _, live := range tt.lives {
				cluster.store(live)
			}
			p := buildUpgradeMaybeHook(t, cluster, tt.previous, tt.desired, tt.hook)
			assert.Equal(t, tt.wantHelm, p.HelmAction)
			assert.Equal(t, tt.wantNoOp, p.IsNoOp())
			assert.Equal(t, tt.wantAppAction, helmActionNamed(p, "app"))
			assert.Equal(t, tt.wantWillRun, firstTaskWillRun(p))
			require.Len(t, p.Drift, 1)
			assert.Equal(t, semantic.DriftMissing, p.Drift[0].Kind)
			assert.Empty(t, p.Drift[0].Fields)
		})
	}
}

func TestBuildSemanticPlan_APIVersionTransition(t *testing.T) {
	t.Parallel()
	previous := "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: app\n  namespace: prod\nspec:\n  color: blue\n"
	desired := "apiVersion: example.com/v2\nkind: Widget\nmetadata:\n  name: app\n  namespace: prod\nspec:\n  color: blue\n"
	cluster := readyCluster()
	cluster.store(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"name":      "app",
			"namespace": "prod",
			"labels":    map[string]any{"app.kubernetes.io/managed-by": "Helm"},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      "web",
				"meta.helm.sh/release-namespace": "prod",
			},
		},
		"spec": map[string]any{"color": "blue"},
	}})
	p := buildPlan(t, cluster, false, previous, desired, nil)
	var helmRows int
	for _, c := range p.Changes {
		if c.Origin.Kind != semantic.OriginHelm {
			continue
		}
		helmRows++
		assert.Equal(t, "app", c.Resource.Name)
		assert.Equal(t, "Widget", c.Resource.Kind)
		assert.NotEqual(t, semantic.Delete, c.Action)
	}
	assert.Equal(t, 1, helmRows)
}

func TestBuildSemanticPlan_ArrayAndLiveOnly(t *testing.T) {
	t.Parallel()
	previous := deployYAML("web", "nginx:1")
	desired := deployYAML("web", "nginx:2")
	live := ownedDeploy("web", "prod", "web", "nginx:1")
	containers, ok, err := unstructured.NestedSlice(live.Object, "spec", "template", "spec", "containers")
	require.NoError(t, err)
	require.True(t, ok)
	containers = append(containers, map[string]any{"name": "sidecar", "image": "busybox"})
	require.NoError(t, unstructured.SetNestedSlice(live.Object, containers, "spec", "template", "spec", "containers"))
	require.NoError(t, unstructured.SetNestedField(live.Object, map[string]any{"replicas": int64(1)}, "status"))
	cluster := readyCluster()
	cluster.store(live)
	p := buildPlan(t, cluster, false, previous, desired, nil)
	require.NotEmpty(t, p.Changes)
	var dep *semantic.ResourceChange
	for i := range p.Changes {
		if p.Changes[i].Resource.Kind == "Deployment" {
			dep = &p.Changes[i]
			break
		}
	}
	require.NotNil(t, dep)
	assert.True(t, hasFieldPath(dep.Fields, "/spec/template/spec/containers/0/image"))
	for _, f := range dep.Fields {
		assert.NotEqual(t, semantic.FieldRemove, f.Op)
		assert.NotContains(t, f.Path, "sidecar")
		assert.NotContains(t, f.Path, "/status")
	}
}

func TestBuildSemanticPlan_NamedArrayMisalignment(t *testing.T) {
	t.Parallel()
	manifest := deployYAML("web", "nginx:1")
	live := ownedDeploy("web", "prod", "web", "nginx:1")
	require.NoError(t, unstructured.SetNestedSlice(live.Object, []any{
		map[string]any{"name": "sidecar", "image": "busybox"},
	}, "spec", "template", "spec", "containers"))
	cluster := readyCluster()
	cluster.store(live)
	client := &fakeBuildClient{
		result: upgradeResult(manifest, 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      &v1.Release{Version: 1, Manifest: manifest},
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "misaligned")
}

func TestBuildSemanticPlan_CRDCreateReplaceExactAfter(t *testing.T) {
	t.Parallel()
	o := widgetCRDObject(t, nil)
	body, err := extras.ApplyObject(o)
	require.NoError(t, err)
	live := body.DeepCopy()
	live.SetAnnotations(map[string]string{"live.only": "x"})
	cluster := readyCluster()
	cluster.store(live)
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{o}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var crd *semantic.ResourceChange
	for i := range p.Changes {
		if p.Changes[i].Origin.Kind == semantic.OriginCRD {
			crd = &p.Changes[i]
			break
		}
	}
	require.NotNil(t, crd)
	assert.Equal(t, semantic.Update, crd.Action)
	assert.Equal(t, body.Object, crd.After.Object)
	for _, f := range crd.Fields {
		assert.NotContains(t, f.Path, "live.only")
	}
}

func TestBuildSemanticPlan_NamespaceUpdateDropsLiveOnly(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	cluster.store(&unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name": "prod",
			"annotations": map[string]any{
				"live.only": "x",
			},
		},
	}})
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes)
	assert.Equal(t, semantic.OriginNamespace, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	for _, f := range p.Changes[0].Fields {
		assert.NotContains(t, f.Path, "live.only")
	}
}

func TestBuildSemanticPlan_HelmNoneDriftWillRunFalse(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	hook := planHook("migrate", "busybox")
	result := upgradeResult(manifest, 4)
	result.Hooks = []*v1.Hook{hook}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	cluster := readyCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "drifted"))
	client := &fakeBuildClient{
		result:  result,
		prep:    upgradePrepWithHook(manifest, 3, 4, hook),
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolved, nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	assert.Empty(t, p.Changes)
	require.NotEmpty(t, p.Drift)
	assert.Equal(t, semantic.DriftModified, p.Drift[0].Kind)
	require.Len(t, p.Tasks, 1)
	assert.False(t, p.Tasks[0].WillRun)
	assert.True(t, p.IsNoOp())
}

func generateNameYAML(prefix string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  generateName: %s
  namespace: prod
data:
  key: v1
`, prefix)
}

func keepPolicyYAML(name string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: prod
  annotations:
    %s: %s
data:
  key: x
`, name, kube.ResourcePolicyAnno, kube.KeepPolicy)
}

func unownedConfigMap(name, namespace, data string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"data":       map[string]any{"key": data},
	}}
}

func deployYAML(name, image string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: prod
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: app
        image: %s
`, name, name, name, image)
}

func ownedDeploy(name, namespace, release, image string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels":    map[string]any{"app.kubernetes.io/managed-by": "Helm"},
			"annotations": map[string]any{
				"meta.helm.sh/release-name":      release,
				"meta.helm.sh/release-namespace": namespace,
			},
		},
		"spec": map[string]any{
			"replicas": int64(1),
			"selector": map[string]any{"matchLabels": map[string]any{"app": name}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": name}},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "app", "image": image},
					},
				},
			},
		},
	}}
}

func toYAMLString(obj *unstructured.Unstructured) (string, error) {
	b, err := obj.MarshalJSON()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func withKeep(obj *unstructured.Unstructured) *unstructured.Unstructured {
	cp := obj.DeepCopy()
	ann := cp.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[kube.ResourcePolicyAnno] = kube.KeepPolicy
	cp.SetAnnotations(ann)
	return cp
}

func helmChangeNamed(p semantic.Plan, name, generateName string) *semantic.ResourceChange {
	for i := range p.Changes {
		c := &p.Changes[i]
		if c.Origin.Kind != semantic.OriginHelm {
			continue
		}
		if generateName != "" && c.Resource.GenerateName == generateName {
			return c
		}
		if name != "" && c.Resource.Name == name {
			return c
		}
	}
	return nil
}

func helmActionNamed(p semantic.Plan, name string) semantic.Action {
	c := helmChangeNamed(p, name, "")
	if c == nil {
		return 0
	}
	return c.Action
}

func helmFieldsNamed(p semantic.Plan, name string) []semantic.FieldChange {
	c := helmChangeNamed(p, name, "")
	if c == nil {
		return nil
	}
	return c.Fields
}

func hasResourceNamed(p semantic.Plan, name string) bool {
	for _, c := range p.Changes {
		if c.Resource.Name == name {
			return true
		}
	}
	return false
}

func hasActionNamed(p semantic.Plan, name string, action semantic.Action) bool {
	for _, c := range p.Changes {
		if c.Resource.Name == name && c.Action == action {
			return true
		}
	}
	return false
}

func fieldValues(fields []semantic.FieldChange, path string) (before, after string) {
	for _, f := range fields {
		if f.Path == path {
			return fmt.Sprint(f.Before), fmt.Sprint(f.After)
		}
	}
	return "", ""
}

func firstDriftKey(p semantic.Plan) (kind semantic.DriftKind, before, after string) {
	if len(p.Drift) == 0 {
		return 0, "", ""
	}
	d := p.Drift[0]
	if len(d.Fields) == 0 {
		return d.Kind, "", ""
	}
	return d.Kind, fmt.Sprint(d.Fields[0].Before), fmt.Sprint(d.Fields[0].After)
}

func firstTaskWillRun(p semantic.Plan) bool {
	if len(p.Tasks) == 0 {
		return false
	}
	return p.Tasks[0].WillRun
}

func buildPlan(t *testing.T, cluster *fakeCluster, install bool, previous, desired string, mappingErr map[schema.GroupVersionKind]error) semantic.Plan {
	t.Helper()
	if mappingErr != nil {
		cluster.mappingErr = mappingErr
	}
	var client *fakeBuildClient
	if install {
		client = &fakeBuildClient{
			result:  installResult(desired),
			prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			cleanup: func() {},
		}
	} else {
		client = &fakeBuildClient{
			result: upgradeResult(desired, 2),
			prep: helm.ReleasePrep{
				Operation:    helm.OperationUpgrade,
				Current:      &v1.Release{Version: 1, Manifest: previous},
				NextRevision: 2,
			},
			cleanup: func() {},
		}
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	return p
}

func buildUpgradeWithHook(t *testing.T, cluster *fakeCluster, previous, desired string, hook *v1.Hook) semantic.Plan {
	t.Helper()
	result := upgradeResult(desired, 2)
	result.Hooks = []*v1.Hook{hook}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		hook.Name: {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
	}
	client := &fakeBuildClient{
		result:  result,
		prep:    upgradePrepWithHook(previous, 1, 2, hook),
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolved, nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	return p
}

func buildUpgradeMaybeHook(t *testing.T, cluster *fakeCluster, previous, desired string, hook *v1.Hook) semantic.Plan {
	t.Helper()
	if hook == nil {
		return buildPlan(t, cluster, false, previous, desired, nil)
	}
	return buildUpgradeWithHook(t, cluster, previous, desired, hook)
}
