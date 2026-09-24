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
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func widgetManifest(name, generateName string) string {
	meta := "  name: " + name
	if generateName != "" {
		meta = "  generateName: " + generateName
	}
	return "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n" + meta + "\n  namespace: prod\nspec:\n  color: blue\n"
}

func storedCRD(name, group, kind, plural, version string, extraSpec map[string]any) *unstructured.Unstructured {
	spec := map[string]any{
		"group": group,
		"scope": "Namespaced",
		"names": map[string]any{"kind": kind, "plural": plural},
		"versions": []any{map[string]any{
			"name":    version,
			"served":  true,
			"storage": true,
			"schema":  map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}},
		}},
	}
	maps.Copy(spec, extraSpec)
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
	}}
}

func TestBuildSemanticPlan_InstallCreatesNamespace(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(p.Changes), 2)
	assert.Equal(t, semantic.OriginNamespace, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, semantic.WriteServerSide, p.Changes[0].Apply.Write.Method)
	assert.False(t, p.Changes[0].Apply.Write.ForceConflicts)
	require.NotEmpty(t, p.Diagnostics)
	assert.Contains(t, p.Diagnostics[0].Message, "target namespace is created earlier in this deployment")
	require.Len(t, cluster.applies, 1)
	assert.Equal(t, "deployah", cluster.applies[0].Opts.FieldManager)
	assert.False(t, cluster.applies[0].Opts.ForceConflicts)
}

func TestBuildSemanticPlan_InstallNamespaceLabelUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ns   *unstructured.Unstructured
	}{
		{
			name: "missing name label",
			ns: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata":   map[string]any{"name": "prod"},
			}},
		},
		{
			name: "wrong name label",
			ns: &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata": map[string]any{
					"name":   "prod",
					"labels": map[string]any{"name": "other"},
				},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{
				result:  installResult(configMapYAML("app", "prod", "v1")),
				prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
				cleanup: func() {},
			}
			cluster := newFakeCluster()
			cluster.store(tt.ns)
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
			t.Cleanup(cleanup)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(p.Changes), 1)
			assert.Equal(t, semantic.OriginNamespace, p.Changes[0].Origin.Kind)
			assert.Equal(t, semantic.Update, p.Changes[0].Action)
			assert.True(t, hasFieldPath(p.Changes[0].Fields, "/metadata/labels") || hasFieldPath(p.Changes[0].Fields, "/metadata/labels/name"))
		})
	}
}

func TestBuildSemanticPlan_UpgradeMissingNamespace(t *testing.T) {
	t.Parallel()
	current := &v1.Release{Version: 1, Manifest: configMapYAML("app", "prod", "old")}
	client := &fakeBuildClient{
		result: upgradeResult(configMapYAML("app", "prod", "new"), 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, `upgrade requires namespace "prod" to exist`)
}

func TestBuildSemanticPlan_InstallTargetNamespaceOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "bare namespace",
			manifest: `apiVersion: v1
kind: Namespace
metadata:
  name: prod
`,
		},
		{
			name: "namespace in list",
			manifest: `apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: Namespace
  metadata:
    name: prod
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{
				result:  installResult(tt.manifest),
				prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
				cleanup: func() {},
			}
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), buildInput("ctx", resolvedSpec(), nil))
			t.Cleanup(cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, "cannot plan target namespace")
			assert.Zero(t, p)
		})
	}
}

func TestBuildSemanticPlan_InstallOtherNamespaceAllowed(t *testing.T) {
	t.Parallel()
	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: extra
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: prod
data:
  key: v1
`
	client := &fakeBuildClient{
		result:  installResult(manifest),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var kinds []string
	for _, c := range p.Changes {
		kinds = append(kinds, c.Origin.Kind.String()+":"+c.Resource.Kind+"/"+c.Resource.Name)
	}
	assert.Contains(t, kinds, "helm:Namespace/extra")
	assert.NotContains(t, kinds, "namespace:Namespace/prod")
}

func TestBuildSemanticPlan_UpgradeRenderedTargetNamespace(t *testing.T) {
	t.Parallel()
	manifest := `apiVersion: v1
kind: Namespace
metadata:
  name: prod
`
	current := &v1.Release{Version: 1, Manifest: manifest}
	desired := `apiVersion: v1
kind: Namespace
metadata:
  name: prod
  labels:
    env: prod
`
	client := &fakeBuildClient{
		result: upgradeResult(desired, 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      current,
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	cluster := readyCluster()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.NotEmpty(t, p.Changes)
	assert.Equal(t, semantic.OriginHelm, p.Changes[0].Origin.Kind)
	assert.Equal(t, "Namespace", p.Changes[0].Resource.Kind)
}

func TestBuildSemanticPlan_CRDFilesDoNotProduceResourceChanges(t *testing.T) {
	t.Parallel()
	crd := extras.RawFile{Path: "widgets.yaml", Raw: []byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\n")}
	rawCopy := append([]byte(nil), crd.Raw...)
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{crd}
	in.CRDDocs = []extras.CRDDoc{{
		Path: "/abs/.deployah/crds/widgets.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
	}}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, rawCopy, in.CRDs[0].Raw)
	for _, c := range p.Changes {
		assert.True(t, c.Origin.Kind == semantic.OriginHelm || c.Origin.Kind == semantic.OriginNamespace)
		assert.NotEqual(t, "CustomResourceDefinition", c.Resource.Kind)
	}
	require.Len(t, p.ChartCRDs, 1)
	assert.Equal(t, semantic.ChartCRDProcess, p.ChartCRDs[0].Lifecycle)
	assert.True(t, p.ChartCRDs[0].WillProcess)
	assert.Equal(t, "widgets.example.com", p.ChartCRDs[0].Name)
}

func TestBuildSemanticPlan_ChartCRDLifecycle(t *testing.T) {
	t.Parallel()
	docs := []extras.CRDDoc{{
		Path: "/abs/.deployah/crds/widget.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
	}}
	tests := []struct {
		name        string
		op          helm.Operation
		skip        bool
		wantLife    semantic.ChartCRDLifecycle
		wantProcess bool
		wantAction  semantic.HelmAction
	}{
		{name: "fresh install", op: helm.OperationInstall, wantLife: semantic.ChartCRDProcess, wantProcess: true, wantAction: semantic.HelmInstall},
		{name: "fresh skip", op: helm.OperationInstall, skip: true, wantLife: semantic.ChartCRDSkip, wantAction: semantic.HelmInstall},
		{name: "upgrade", op: helm.OperationUpgrade, wantLife: semantic.ChartCRDUpgrade, wantAction: semantic.HelmNone},
		{name: "upgrade ignores skip", op: helm.OperationUpgrade, skip: true, wantLife: semantic.ChartCRDUpgrade, wantAction: semantic.HelmNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			manifest := configMapYAML("app", "prod", "same")
			client := &fakeBuildClient{cleanup: func() {}}
			cluster := readyCluster()
			in := buildInput("ctx", resolvedSpec(), nil)
			in.CRDDocs = docs
			in.SkipCRDs = tc.skip
			switch tc.op {
			case helm.OperationInstall:
				client.result = installResult(configMapYAML("app", "prod", "v1"))
				client.prep = helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1}
			case helm.OperationUpgrade:
				current := &v1.Release{Version: 3, Manifest: manifest}
				client.result = upgradeResult(manifest, 4)
				client.prep = helm.ReleasePrep{
					Operation:    helm.OperationUpgrade,
					Current:      current,
					Newest:       current,
					NextRevision: 4,
				}
				cluster.store(ownedConfigMap("app", "prod", "web", "same"))
			}
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
			t.Cleanup(cleanup)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAction, p.HelmAction)
			require.Len(t, p.ChartCRDs, 1)
			assert.Equal(t, ".deployah/crds/widget.yaml", p.ChartCRDs[0].Source)
			assert.Equal(t, "CustomResourceDefinition", p.ChartCRDs[0].Kind)
			assert.Equal(t, "widgets.example.com", p.ChartCRDs[0].Name)
			assert.Equal(t, tc.wantLife, p.ChartCRDs[0].Lifecycle)
			assert.Equal(t, tc.wantProcess, p.ChartCRDs[0].WillProcess)
			for _, c := range p.Changes {
				assert.NotEqual(t, "CustomResourceDefinition", c.Resource.Kind)
			}
			if tc.op == helm.OperationUpgrade {
				assert.Empty(t, p.Changes)
				assert.False(t, p.HasEffects())
				assert.True(t, p.IsNoOp())
			}
		})
	}
}

func TestBuildSemanticPlan_UnknownCustomResourceNotInventedFromCRDFiles(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(widgetManifest("app", "")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := readyCluster()
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	cluster.mappingErr[gvk] = &meta.NoKindMatchError{GroupKind: gvk.GroupKind()}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{{Path: "widgets.yaml", Raw: []byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\n")}}
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "predict resources")
	assert.Empty(t, cluster.creates)
}

func TestBuildSemanticPlan_GenerateNameMissingNamespaceConfigMap(t *testing.T) {
	t.Parallel()
	manifest := `apiVersion: v1
kind: ConfigMap
metadata:
  generateName: app-
  namespace: prod
data:
  key: v1
`
	client := &fakeBuildClient{
		result:  installResult(manifest),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var gens []string
	for _, d := range p.Diagnostics {
		if d.Resource != nil {
			gens = append(gens, d.Resource.GenerateName)
			assert.Contains(t, d.Message, "target namespace is created earlier in this deployment")
		}
	}
	assert.Contains(t, gens, "app-")
	require.Len(t, cluster.applies, 1)
	assert.Equal(t, "Namespace", cluster.applies[0].Obj.GetKind())
}

func TestBuildSemanticPlan_GenerateNameMissingNamespace(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(widgetManifest("", "app-") + "---\n" + widgetManifest("", "job-")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var gens []string
	for _, d := range p.Diagnostics {
		if d.Resource != nil {
			gens = append(gens, d.Resource.GenerateName)
		}
	}
	assert.Contains(t, gens, "app-")
	assert.Contains(t, gens, "job-")
}

func TestBuildSemanticPlan_PruneSyntheticGetNoLimitation(t *testing.T) {
	t.Parallel()
	previous := widgetManifest("old", "")
	client := &fakeBuildClient{
		result: upgradeResult(configMapYAML("app", "prod", "v1"), 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      &v1.Release{Version: 1, Manifest: previous + "---\n" + configMapYAML("app", "prod", "v1")},
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	cluster := readyCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "v1"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	for _, c := range p.Changes {
		assert.NotEqual(t, "old", c.Resource.Name)
	}
	for _, d := range p.Diagnostics {
		if d.Resource != nil {
			assert.NotEqual(t, "old", d.Resource.Name)
		}
	}
}

func TestBuildSemanticPlan_IdleReleaseDoesNotRunHooks(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	pre := planHook("migrate", "busybox")
	post := planHook("smoke", "busybox")
	post.Events = []v1.HookEvent{v1.HookPostInstall, v1.HookPostUpgrade}
	result := upgradeResult(manifest, 4)
	result.Hooks = []*v1.Hook{pre, post}
	resolved := resolvedSpec()
	resolved.Tasks = map[string]spec.ResolvedTask{
		"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
		"smoke":   {Task: spec.Task{On: spec.TaskOnPostDeploy}, HookWeight: 2},
	}
	current := &v1.Release{
		Version:  3,
		Manifest: manifest,
		Hooks:    []*v1.Hook{pre, post},
		Config: map[string]any{
			"deployah": map[string]any{
				"resolved": map[string]any{
					"tasks": map[string]any{
						"migrate": map[string]any{"on": "preDeploy", "hookWeight": 1},
						"smoke":   map[string]any{"on": "postDeploy", "hookWeight": 2},
					},
				},
			},
		},
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
	cluster := readyCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolved, nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	require.Len(t, p.Tasks, 2)
	byName := make(map[string]semantic.TaskPlan, len(p.Tasks))
	for _, task := range p.Tasks {
		byName[task.Name] = task
		assert.Equal(t, semantic.TaskUnchanged, task.Action)
		assert.False(t, task.WillRun)
	}
	assert.Equal(t, semantic.TaskPreDeploy, byName["migrate"].Phase)
	assert.Equal(t, semantic.TaskPostDeploy, byName["smoke"].Phase)
}

func TestBuildSemanticPlan_OmittedLiveCRDIsNotDeleted(t *testing.T) {
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
	cluster := readyCluster()
	cluster.store(ownedConfigMap("app", "prod", "web", "same"))
	cluster.store(storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil))
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Empty(t, p.Changes)
	assert.Empty(t, cluster.deletes)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
}
