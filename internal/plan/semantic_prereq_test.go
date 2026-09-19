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
	"errors"
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func widgetManifest(name, generateName string) string {
	meta := "  name: " + name
	if generateName != "" {
		meta = "  generateName: " + generateName
	}
	return "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n" + meta + "\n  namespace: prod\nspec:\n  color: blue\n"
}

func widgetCRDObject(t *testing.T, versions []any) extras.RawFile {
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
	return extras.RawFile{Path: o.Path, Raw: raw}
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

func extraCRD(t *testing.T, name, kind, plural string) extras.RawFile {
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
	return extras.RawFile{Path: o.Path, Raw: raw}
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
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.Len(t, p.Changes, 2)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Create, p.Changes[0].Action)
	assert.Equal(t, semantic.WriteCreate, p.Changes[0].Apply.Write.Method)
	assert.Equal(t, semantic.OriginHelm, p.Changes[1].Origin.Kind)
	assert.Equal(t, semantic.Create, p.Changes[1].Action)
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
	require.Len(t, p.Diagnostics, 1)
	assert.Contains(t, p.Diagnostics[0].Message, "becomes available after CRD widgets.example.com")
	require.Len(t, cluster.creates, 1)
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
	}
	assert.Empty(t, cluster.creates)
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(p.Changes), 1)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.Update, p.Changes[0].Action)
	assert.True(t, p.Changes[0].Apply.Write.ForceConflicts)
	assert.True(t, p.HasEffects())
	var sawCRD bool
	for _, a := range cluster.applies {
		if a.Obj.GetKind() == "CustomResourceDefinition" {
			sawCRD = true
			assert.True(t, a.Opts.ForceConflicts)
			assert.Equal(t, extras.CRDFieldManager, a.Opts.FieldManager)
		}
	}
	assert.True(t, sawCRD)
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
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
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

func TestBuildSemanticPlan_GenerateNameMissingCRD(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(widgetManifest("", "job-")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := readyCluster()
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	cluster.mappingErr[gvk] = &meta.NoKindMatchError{GroupKind: gvk.GroupKind()}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	var gens []string
	for _, d := range p.Diagnostics {
		if d.Resource != nil {
			gens = append(gens, d.Resource.GenerateName)
			assert.Contains(t, d.Message, "becomes available after CRD widgets.example.com")
		}
	}
	assert.Contains(t, gens, "job-")
	require.Len(t, cluster.creates, 1)
	assert.Equal(t, "CustomResourceDefinition", cluster.creates[0].GetKind())
	for _, a := range cluster.applies {
		assert.Equal(t, "Namespace", a.Obj.GetKind())
	}
}

func TestBuildSemanticPlan_GenerateNameMissingNamespace(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(widgetManifest("", "app-") + "---\n" + widgetManifest("", "job-")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	cluster := newFakeCluster()
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	cluster.mappingErr[gvk] = &meta.NoKindMatchError{GroupKind: gvk.GroupKind()}
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
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
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
		assert.NotEqual(t, "old", c.Resource.Name)
	}
	for _, d := range p.Diagnostics {
		if d.Resource != nil {
			assert.NotEqual(t, "old", d.Resource.Name)
		}
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
	cluster.applyErrGVK[gvk] = apierrors.NewForbidden(schema.GroupResource{Group: "example.com", Resource: "widgets"}, "app", errors.New("denied"))
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	_, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "predict resources")
	assert.ErrorContains(t, err, "CRD spec changes")
	assert.ErrorContains(t, err, "denied")
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
			in.CRDs = []extras.RawFile{widgetCRDObject(t, nil), extraCRD(t, tt.crdName, tt.kind, tt.plural)}
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, []any{
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
	gadgetObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": "gadgets.other.com"},
		"spec": map[string]any{
			"group": "other.com",
			"scope": "Namespaced",
			"names": map[string]any{"kind": "Gadget", "plural": "gadgets"},
			"versions": []any{map[string]any{
				"name": "v1", "served": true, "storage": true,
				"schema": map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}},
			}},
		},
	}}
	gadgetFile := extras.Object{Path: "gadgets.other.com.yaml", Obj: gadgetObj}
	gadgetRaw, err := gadgetFile.MarshalYAML()
	require.NoError(t, err)
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil), {Path: gadgetFile.Path, Raw: gadgetRaw}}
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

func TestBuildSemanticPlan_MultiDocCRDFile(t *testing.T) {
	t.Parallel()
	client := &fakeBuildClient{
		result:  installResult(configMapYAML("app", "prod", "v1")),
		prep:    helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1},
		cleanup: func() {},
	}
	widget := widgetCRDObject(t, nil)
	gadget := extraCRD(t, "gadgets.other.com", "Gadget", "gadgets")
	nonCRD := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: not-a-crd\n")
	nameOnly := []byte("metadata:\n  name: ignored.example.com\n")
	combined := append([]byte{}, widget.Raw...)
	combined = append(combined, []byte("---\n")...)
	combined = append(combined, nonCRD...)
	combined = append(combined, []byte("---\n")...)
	combined = append(combined, nameOnly...)
	combined = append(combined, []byte("---\n")...)
	combined = append(combined, gadget.Raw...)
	rawCopy := append([]byte(nil), combined...)
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDs = []extras.RawFile{{Path: "both.yaml", Raw: combined}}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, readyCluster(), in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, rawCopy, in.CRDs[0].Raw)
	var names []string
	for _, c := range p.Changes {
		if c.Origin.Kind == semantic.OriginCRD {
			names = append(names, c.Resource.Name)
		}
	}
	assert.ElementsMatch(t, []string{"widgets.example.com", "gadgets.other.com"}, names)
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	assert.Equal(t, semantic.CompletenessComplete, p.Completeness)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
		assert.NotEqual(t, "Widget", c.Resource.Kind)
	}
	for _, d := range p.Diagnostics {
		assert.NotContains(t, d.Message, "currently installed CRD")
	}
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
		name   string
		policy extras.Policy
		store  []*unstructured.Unstructured
	}{
		{
			name:   "missing create",
			policy: extras.PolicyCreate,
		},
		{
			name:   "existing create-replace",
			policy: extras.PolicyCreateReplace,
			store:  []*unstructured.Unstructured{storedCRD("widgets.example.com", "example.com", "Widget", "widgets", "v1", nil)},
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
			in.CRDs = []extras.RawFile{widgetCRDObject(t, unservedWidgetVersions())}
			in.CRDPolicy = tt.policy
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
			t.Cleanup(cleanup)
			require.NoError(t, err)
			for _, c := range p.Changes {
				assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
			}
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
		wantOK     bool
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
			wantOK:   true,
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
			store:  []*unstructured.Unstructured{liveV1V2},
			policy: extras.PolicyCreateReplace,
			wantOK: true,
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
			in.CRDs = []extras.RawFile{widgetCRDObject(t, tt.versions)}
			in.CRDPolicy = tt.policy
			p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
			t.Cleanup(cleanup)
			if tt.wantOK {
				require.NoError(t, err)
				for _, c := range p.Changes {
					assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
				}
				return
			}
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	for _, ch := range p.Changes {
		assert.NotEqual(t, semantic.OriginCRD, ch.Origin.Kind)
	}
	assert.Equal(t, semantic.HelmNone, p.HelmAction)
	for _, obj := range cluster.creates {
		assert.NotEqual(t, "CustomResourceDefinition", obj.GetKind())
	}
	for _, a := range cluster.applies {
		assert.NotEqual(t, "CustomResourceDefinition", a.Obj.GetKind())
	}
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
	in.CRDs = []extras.RawFile{widgetCRDObject(t, nil)}
	in.CRDPolicy = extras.PolicyCreateReplace
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, cluster, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	for _, c := range p.Changes {
		assert.NotEqual(t, semantic.OriginCRD, c.Origin.Kind)
	}
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
