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
	"deployah.dev/deployah/internal/render"
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

func widgetCRDObject(t *testing.T, versions []any) extras.Object {
	t.Helper()
	if versions == nil {
		versions = []any{map[string]any{
			"name":    "v1",
			"served":  true,
			"storage": true,
			"schema":  map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}},
		}}
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": "widgets.example.com"},
		"spec": map[string]any{
			"group": "example.com",
			"scope": "Namespaced",
			"names": map[string]any{
				"kind":   "Widget",
				"plural": "widgets",
			},
			"versions": versions,
		},
	}}
	o := extras.Object{Path: "widgets.yaml", Obj: obj}
	raw, err := o.MarshalYAML()
	require.NoError(t, err)
	o.Raw = raw
	return o
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

func extraCRD(t *testing.T, name, kind, plural string) extras.Object {
	t.Helper()
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": name},
		"spec": map[string]any{
			"group": "example.com",
			"scope": "Namespaced",
			"names": map[string]any{"kind": kind, "plural": plural},
			"versions": []any{map[string]any{
				"name": "v1", "served": true, "storage": true,
				"schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}},
			}},
		},
	}}
	o := extras.Object{Path: name + ".yaml", Obj: obj}
	raw, err := o.MarshalYAML()
	require.NoError(t, err)
	o.Raw = raw
	return o
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
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newFakeCluster(), buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.HelmUpgrade, p.HelmAction)
	require.NotEmpty(t, p.Changes)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, "app", p.Changes[0].Resource.Name)
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
			cluster := readyCluster()
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
			t.Cleanup(cleanup)
			require.NoError(t, err)
			require.Len(t, p.Changes, 1)
			assert.Equal(t, semantic.OriginHelm, p.Changes[0].Origin.Kind)
			assert.Equal(t, "Namespace", p.Changes[0].Resource.Kind)
			assert.Equal(t, "prod", p.Changes[0].Resource.Name)
			require.NotNil(t, p.Changes[0].After)
			for _, c := range p.Changes {
				assert.NotEqual(t, semantic.OriginNamespace, c.Origin.Kind)
			}
			meta, ok := p.Changes[0].After.Object["metadata"].(map[string]any)
			require.True(t, ok)
			labels, hasLabels := meta["labels"].(map[string]any)
			if hasLabels {
				_, hasName := labels["name"]
				assert.False(t, hasName, "After must be the chart Namespace, not an implicit+chart union")
			}
			assertReadOnly(t, cluster)
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

func TestBuildSemanticPlan_CRDCreateAndWidget(t *testing.T) {
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
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.Len(t, p.Changes, 2)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, semantic.WriteCreate, p.Changes[0].Apply.Write.Method)
	assert.Equal(t, semantic.OriginHelm, p.Changes[1].Origin.Kind)
	assert.Equal(t, semantic.Create, p.Changes[1].Action)
	assertNoGetKind(t, cluster, "Widget")
	assertReadOnly(t, cluster)
}

func TestBuildSemanticPlan_CRDCreateExistingIgnored(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := readyCluster()
	cluster.store(storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil))
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
	}
}

func TestBuildSemanticPlan_CRDCreateReplaceExisting(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := readyCluster()
	cluster.store(storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil))
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(p.Changes), 1)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	assert.True(t, p.Changes[0].Apply.Write.ForceConflicts)
	assert.True(t, p.HasEffects())
	require.NotNil(t, p.Changes[0].After)
	body, err := extras.ApplyObject(in.CRDs[0])
	require.NoError(t, err)
	assert.Equal(t, body.Object, p.Changes[0].After.Object)
	assertReadOnly(t, cluster)
}

func TestBuildSemanticPlan_GenerateNameMissingConfigMap(t *testing.T) {
	t.Parallel()
	cluster := newFakeCluster()
	client := &fakeBuildClient{
		result: installResult(`apiVersion: v1
kind: ConfigMap
metadata:
  generateName: app-
  namespace: prod
data:
  key: v1
`),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var gens []string
	for _, c := range p.Changes {
		if c.Resource.GenerateName == "" {
			continue
		}
		gens = append(gens, c.Resource.GenerateName)
		assert.Equal(t, semantic.Create, c.Action)
	}
	assert.Equal(t, []string{"app-"}, gens)
}

func TestBuildSemanticPlan_GenerateNameMissingWidget(t *testing.T) {
	t.Parallel()
	widgetGVK := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	tests := []struct {
		name     string
		manifest string
		wantGens []string
	}{
		{
			name:     "missing crd",
			manifest: widgetManifest("", "job-"),
			wantGens: []string{"job-"},
		},
		{
			name:     "missing namespace",
			manifest: widgetManifest("", "app-") + "---\n" + widgetManifest("", "job-"),
			wantGens: []string{"app-", "job-"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cluster := newFakeCluster()
			cluster.mappingErr[widgetGVK] = &meta.NoKindMatchError{GroupKind: widgetGVK.GroupKind()}
			in := buildInput("ctx", resolvedSpec(), nil)
			in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
			client := &fakeBuildClient{
				result:  installResult(tt.manifest),
				prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
				cleanup: func() {},
			}
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
			t.Cleanup(cleanup)
			require.NoError(t, err)
			var gens []string
			for _, c := range p.Changes {
				if c.Resource.GenerateName == "" {
					continue
				}
				gens = append(gens, c.Resource.GenerateName)
				assert.Equal(t, semantic.Create, c.Action)
			}
			assert.ElementsMatch(t, tt.wantGens, gens)
			assertNoGetKind(t, cluster, "Widget")
		})
	}
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
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	cluster.mappingErr[gvk] = &meta.NoKindMatchError{GroupKind: gvk.GroupKind()}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	for _, c := range p.Changes {
		assert.NotEqual(t, "old", c.Resource.Name)
	}
}

func TestBuildSemanticPlan_CreateReplaceCurrentAPIFailure(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(widgetManifest("app", "")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := readyCluster()
	cluster.store(storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", map[string]any{"conversion": map[string]any{"strategy": "None"}}))
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	cluster.mappingErr[gvk] = &meta.NoKindMatchError{GroupKind: gvk.GroupKind()}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "not discoverable")
}

func TestBuildSemanticPlan_CRDCollision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		crdName string
		kind    string
		plural  string
		wantErr string
	}{
		{
			name:    "same GVK with different GVR",
			crdName: "objects.example.com",
			kind:    "Widget",
			plural:  "objects",
			wantErr: "conflicting CRD API",
		},
		{
			name:    "same GVR with different GVK",
			crdName: "gadgets.example.com",
			kind:    "Gadget",
			plural:  "widgets",
			wantErr: "conflicting CRD resource",
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
			in := buildInput("ctx", resolvedSpec(), nil)
			in.CRDs = []extras.Object{widgetCRDObject(t, nil), extraCRD(t, tt.crdName, tt.kind, tt.plural)}
			_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), in)
			t.Cleanup(cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestBuildSemanticPlan_MultiVersionCRD(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, []any{
		map[string]any{"name": "v1", "served": true, "storage": true, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
		map[string]any{"name": "v2", "served": true, "storage": false, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
	})}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
}

func TestBuildSemanticPlan_DistinctCRDs(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	gadget := extraCRD(t, "gadgets.other.com", "Gadget", "gadgets")
	require.NotNil(t, gadget.Obj)
	spec, ok := gadget.Obj.Object["spec"].(map[string]any)
	require.True(t, ok)
	spec["group"] = "other.com"
	raw, err := gadget.MarshalYAML()
	require.NoError(t, err)
	gadget.Raw = raw
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil), gadget}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var crdCreates int
	for _, c := range p.Changes {
		if c.Origin.Kind == semantic.OriginCRD {
			crdCreates++
		}
	}
	assert.Equal(t, 2, crdCreates)
}

func TestBuildSemanticPlan_SpecChangeNoOpLimitation(t *testing.T) {
	t.Parallel()
	live := &unstructured.Unstructured{Object: map[string]any{
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
	}}
	client := &fakeBuildClient{
		result: upgradeResult(widgetManifest("app", ""), 2),
		prep: helm.ReleasePrep{
			Operation:    helm.OperationUpgrade,
			Current:      &v1.Release{Version: 1, Manifest: widgetManifest("app", "")},
			NextRevision: 2,
		},
		cleanup: func() {},
	}
	cluster := readyCluster()
	cluster.store(storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", map[string]any{"conversion": map[string]any{"strategy": "None"}}))
	cluster.store(live)
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var widgetChange bool
	for _, c := range p.Changes {
		if c.Resource.Kind == "Widget" {
			widgetChange = true
		}
	}
	assert.False(t, widgetChange)
}

func unservedWidgetVersions() []any {
	return []any{map[string]any{
		"name":    "v1",
		"served":  false,
		"storage": true,
		"schema":  map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}},
	}}
}

func TestBuildSemanticPlan_UnservedOnlyCRDChange(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "same")
	tests := []struct {
		name       string
		policy     extras.Policy
		store      []*unstructured.Unstructured
		wantAction semantic.Action
		wantMethod semantic.WriteMethod
		wantForce  bool
	}{
		{
			name:       "missing create",
			policy:     extras.PolicyCreate,
			wantAction: semantic.Create,
			wantMethod: semantic.WriteCreate,
		},
		{
			name:       "existing create-replace",
			policy:     extras.PolicyCreateReplace,
			store:      []*unstructured.Unstructured{storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil)},
			wantAction: semantic.Update,
			wantMethod: semantic.WriteServerSide,
			wantForce:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
			for _, obj := range tt.store {
				cluster.store(obj)
			}
			in := buildInput("ctx", resolvedSpec(), nil)
			in.CRDs = []extras.Object{widgetCRDObject(t, unservedWidgetVersions())}
			in.CRDPolicy = tt.policy
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
			t.Cleanup(cleanup)
			require.NoError(t, err)
			require.Len(t, p.Changes, 1)
			c := p.Changes[0]
			assert.Equal(t, semantic.OriginCRD, c.Origin.Kind)
			assert.Equal(t, tt.wantAction, c.Action)
			assert.Equal(t, tt.wantMethod, c.Apply.Write.Method)
			assert.Equal(t, tt.wantForce, c.Apply.Write.ForceConflicts)
			assert.NotNil(t, c.After)
			assert.Equal(t, semantic.HelmNone, p.HelmAction)
		})
	}
}

func TestBuildSemanticPlan_UnavailableAPIHardError(t *testing.T) {
	t.Parallel()
	previous := &v1.Release{Version: 1, Manifest: widgetManifest("old", "")}
	v2 := schema.GroupVersionKind{Group: "example.com", Version: "v2", Kind: "Widget"}
	liveV1V2 := storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", map[string]any{
		"versions": []any{
			map[string]any{"name": "v1", "served": true, "storage": true, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
			map[string]any{"name": "v2", "served": true, "storage": false, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
		},
	})
	tests := []struct {
		name       string
		result     *render.RenderResult
		prep       helm.ReleasePrep
		versions   []any
		store      []*unstructured.Unstructured
		mappingErr map[schema.GroupVersionKind]error
		policy     extras.Policy
		wantErr    string
	}{
		{
			name:     "desired all unserved",
			result:   installResult(widgetManifest("app", "")),
			prep:     helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			versions: unservedWidgetVersions(),
			policy:   extras.PolicyCreate,
			wantErr:  "API example.com/v1/Widget is not served by CRD widgets.example.com",
		},
		{
			name:   "previous becomes unserved",
			result: upgradeResult(configMapYAML("app", "prod", "v1"), 2),
			prep: helm.ReleasePrep{
				Operation:    helm.OperationUpgrade,
				Current:      previous,
				Newest:       previous,
				NextRevision: 2,
			},
			versions: unservedWidgetVersions(),
			store:    []*unstructured.Unstructured{storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil)},
			policy:   extras.PolicyCreateReplace,
			wantErr:  "API example.com/v1/Widget is not served by CRD widgets.example.com",
		},
		{
			name: "desired unserved version while another remains served",
			result: installResult(`apiVersion: example.com/v2
kind: Widget
metadata:
  name: app
  namespace: prod
`),
			prep: helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			versions: []any{
				map[string]any{"name": "v1", "served": true, "storage": true, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
				map[string]any{"name": "v2", "served": false, "storage": false, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
			},
			policy:  extras.PolicyCreate,
			wantErr: "is not served by CRD widgets.example.com",
		},
		{
			name:   "removed served version still in previous release",
			result: upgradeResult(configMapYAML("app", "prod", "v1"), 2),
			prep: helm.ReleasePrep{
				Operation: helm.OperationUpgrade,
				Current: &v1.Release{Version: 1, Manifest: `apiVersion: example.com/v2
kind: Widget
metadata:
  name: old
  namespace: prod
`},
				NextRevision: 2,
			},
			store:   []*unstructured.Unstructured{liveV1V2},
			policy:  extras.PolicyCreateReplace,
			wantErr: "API example.com/v2/Widget is not served by CRD widgets.example.com",
		},
		{
			name: "new version on existing CRD is not discoverable",
			result: installResult(`apiVersion: example.com/v2
kind: Widget
metadata:
  name: app
  namespace: prod
`),
			prep: helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
			versions: []any{
				map[string]any{"name": "v1", "served": true, "storage": false, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
				map[string]any{"name": "v2", "served": true, "storage": true, "schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}}},
			},
			store: []*unstructured.Unstructured{storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil)},
			mappingErr: map[schema.GroupVersionKind]error{
				v2: &meta.NoKindMatchError{GroupKind: v2.GroupKind()},
			},
			policy:  extras.PolicyCreateReplace,
			wantErr: "would add this API but it is not discoverable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeBuildClient{result: tt.result, prep: tt.prep, cleanup: func() {}}
			cluster := readyCluster()
			for _, obj := range tt.store {
				cluster.store(obj)
			}
			maps.Copy(cluster.mappingErr, tt.mappingErr)
			in := buildInput("ctx", resolvedSpec(), nil)
			in.CRDs = []extras.Object{widgetCRDObject(t, tt.versions)}
			in.CRDPolicy = tt.policy
			_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
			t.Cleanup(cleanup)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestBuildSemanticPlan_CRDCreateReplaceMissing(t *testing.T) {
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
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	c := p.Changes[0]
	assert.Equal(t, semantic.OriginCRD, c.Origin.Kind)
	assert.Equal(t, semantic.Create, c.Action)
	assert.Equal(t, semantic.WriteServerSide, c.Apply.Write.Method)
	assert.Equal(t, extras.CRDFieldManager, c.Apply.Write.FieldManager)
	assert.True(t, c.Apply.Write.ForceConflicts)
	assert.Nil(t, c.Before)
	require.NotNil(t, c.After)
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	for _, ch := range p.Changes {
		assert.NotEqual(t, semantic.OriginHelm, ch.Origin.Kind)
	}
	assertReadOnly(t, cluster)
}

func TestBuildSemanticPlan_CRDOnlyDoesNotRunHooks(t *testing.T) {
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
	cluster.store(storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil))
	in := buildInput("ctx", resolved, nil)
	in.CRDs = []extras.Object{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.Len(t, p.Changes, 1)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
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
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
}
