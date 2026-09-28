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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"
)

func TestBuildSemanticPlan_FreshInstallHasNoDrift(t *testing.T) {
	t.Parallel()
	manifest := configMapYAML("app", "prod", "v1")
	p := mustBuild(t, installClient(manifest), newMapper())
	require.NotNil(t, p.Drift)
	assert.Empty(t, p.Drift)

	live := &fakeLive{}
	seedLive(t, live, manifest, "prod", func(obj *unstructured.Unstructured) {
		stampRelease(obj, "web", "prod")
	})
	got := buildDrift(t, installClient(manifest), newMapper(), live)
	require.NotNil(t, got.Drift)
	assert.Empty(t, got.Drift)
	assert.Empty(t, live.Calls())
}

func TestBuildSemanticPlan_UpgradeRequiresLiveReader(t *testing.T) {
	t.Parallel()
	client := upgradeClient(configMapYAML("app", "prod", "v"), configMapYAML("app", "prod", "v"), 4)
	p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), nil, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.Error(t, err)
	assert.ErrorContains(t, err, "requires a live reader")
	assert.Zero(t, p)
	assert.Nil(t, result)
	assert.Equal(t, 1, client.calls)
}

func TestBuildSemanticPlan_DriftAgainstLive(t *testing.T) {
	t.Parallel()
	unchanged := []struct {
		name     string
		manifest string
		live     string
	}{
		{name: "equal", manifest: configMapYAML("app", "prod", "same"), live: configMapYAML("app", "prod", "same")},
		{
			name:     "undeclared live field",
			manifest: configMapYAML("app", "prod", "same"),
			live: `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: prod
data:
  key: same
  other: extra
spec:
  clusterDomain: cluster.local
`,
		},
		{
			name:     "bookkeeping only",
			manifest: configMapYAML("app", "prod", "same"),
			live: `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: prod
  uid: abc
  resourceVersion: "9"
  generation: 3
  creationTimestamp: "2020-01-01T00:00:00Z"
  managedFields:
    - manager: helm
  annotations:
    meta.helm.sh/release-name: web
    meta.helm.sh/release-namespace: prod
data:
  key: same
status:
  observed: true
`,
		},
	}
	for _, tt := range unchanged {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			live := &fakeLive{}
			seedLive(t, live, tt.live, "prod", nil)
			p := buildDrift(t, upgradeClient(tt.manifest, tt.manifest, 4), newMapper(), live)
			assert.Equal(t, semantic.HelmNone, p.HelmAction)
			assert.Empty(t, p.Drift)
		})
	}

	fields := []struct {
		name     string
		manifest string
		live     string
		path     string
		op       semantic.FieldOp
		before   any
		after    any
	}{
		{
			name: "declared field differs", manifest: configMapYAML("app", "prod", "old"),
			live: configMapYAML("app", "prod", "new"), path: "/data/key", op: semantic.FieldReplace,
			before: "old", after: "new",
		},
		{
			name: "declared field missing", manifest: configMapYAML("app", "prod", "old"),
			live: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\n",
			path: "/data", op: semantic.FieldRemove, before: map[string]any{"key": "old"},
		},
	}
	for _, tt := range fields {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			live := &fakeLive{}
			seedLive(t, live, tt.live, "prod", nil)
			p := buildDrift(t, upgradeClient(tt.manifest, tt.manifest, 4), newMapper(), live)
			assert.Equal(t, semantic.HelmNone, p.HelmAction)
			d := onlyDrift(t, p)
			assert.Equal(t, semantic.DriftModified, d.Action)
			require.Len(t, d.Fields, 1)
			assert.Equal(t, tt.path, d.Fields[0].Path)
			assert.Equal(t, tt.op, d.Fields[0].Op)
			assert.Equal(t, tt.before, d.Fields[0].Before)
			assert.Equal(t, tt.after, d.Fields[0].After)
			for _, field := range d.Fields {
				assert.NotEqual(t, "/apiVersion", field.Path)
			}
		})
	}

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		manifest := configMapYAML("app", "prod", "old")
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), &fakeLive{})
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftMissing, d.Action)
		assert.Equal(t, semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}, d.Resource)
		assert.Nil(t, d.Live)
		assert.Empty(t, d.Fields)
		data, ok := d.Previous.Object["data"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "old", data["key"])
	})
	t.Run("apiVersion alone", func(t *testing.T) {
		t.Parallel()
		manifest := hpaManifestNamed("v2beta2", "app", 2)
		live := &fakeLive{}
		seedLive(t, live, manifest, "prod", func(obj *unstructured.Unstructured) {
			obj.SetAPIVersion("autoscaling/v2")
		})
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		assert.Empty(t, p.Drift)
		require.Len(t, gets(live), 1)
		assert.Equal(t, "v2beta2", gets(live)[0].version)
		assert.Equal(t, "prod", gets(live)[0].namespace)
	})
	t.Run("apiVersion with a real field change keeps previous ref", func(t *testing.T) {
		t.Parallel()
		manifest := hpaManifestNamed("v2beta2", "app", 2)
		live := &fakeLive{}
		seedLive(t, live, manifest, "prod", func(obj *unstructured.Unstructured) {
			obj.SetAPIVersion("autoscaling/v2")
			require.NoError(t, unstructured.SetNestedField(obj.Object, int64(3), "spec", "maxReplicas"))
		})
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
		assert.Equal(t, "autoscaling/v2beta2", d.Resource.APIVersion)
		require.Len(t, d.Fields, 1)
		assert.Equal(t, "/spec/maxReplicas", d.Fields[0].Path)
		assert.NotEqual(t, "/apiVersion", d.Fields[0].Path)
	})
	t.Run("omitted namespace", func(t *testing.T) {
		t.Parallel()
		manifest := configMapYAML("app", "", "same")
		live := &fakeLive{}
		seedLive(t, live, configMapYAML("app", "prod", "same"), "prod", nil)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		assert.Empty(t, p.Drift)
		require.Len(t, gets(live), 1)
		assert.Equal(t, "v1", gets(live)[0].version)
		assert.Equal(t, "prod", gets(live)[0].namespace)
	})
	t.Run("cluster scoped declared namespace", func(t *testing.T) {
		t.Parallel()
		manifest := kindYAML("rbac.authorization.k8s.io/v1", "ClusterRole", "app", "prod", "rules: []")
		live := &fakeLive{}
		seedLive(t, live, kindYAML("rbac.authorization.k8s.io/v1", "ClusterRole", "app", "", "rules: []"), "", nil)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
		require.Len(t, d.Fields, 1)
		assert.Equal(t, "/metadata/namespace", d.Fields[0].Path)
		assert.Equal(t, semantic.FieldRemove, d.Fields[0].Op)
		require.Len(t, gets(live), 1)
		assert.Equal(t, "v1", gets(live)[0].version)
		assert.Empty(t, gets(live)[0].namespace)
	})
}

func TestBuildSemanticPlan_DriftEquivalence(t *testing.T) {
	t.Parallel()
	emptyLimits := []struct {
		name     string
		manifest string
	}{
		{name: "deployment empty limits", manifest: workloadManifest("apps/v1", "Deployment")},
		{name: "statefulset empty limits", manifest: workloadManifest("apps/v1", "StatefulSet")},
		{name: "cronjob empty limits", manifest: workloadManifest("batch/v1", "CronJob")},
	}
	for _, tt := range emptyLimits {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			live := &fakeLive{}
			seedLive(t, live, tt.manifest, "prod", func(obj *unstructured.Unstructured) {
				dropLimits(t, obj)
			})
			p := buildDrift(t, upgradeClient(tt.manifest, tt.manifest, 4), newMapper(), live)
			assert.Empty(t, p.Drift)
		})
	}
	t.Run("stringData matches data", func(t *testing.T) {
		t.Parallel()
		previous := secretManifest("old")
		live := &fakeLive{}
		seedLive(t, live, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  namespace: prod\ndata:\n  password: b2xk\n", "prod", nil)
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		assert.Empty(t, p.Drift)
	})
	t.Run("stringData differs", func(t *testing.T) {
		t.Parallel()
		previous := secretManifest("old")
		live := &fakeLive{}
		seedLive(t, live, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  namespace: prod\ndata:\n  password: bmV3\n", "prod", nil)
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
		require.Len(t, d.Fields, 1)
		assert.Equal(t, "/data/password", d.Fields[0].Path)
		assert.Equal(t, semantic.FieldReplace, d.Fields[0].Op)
		_, hasData := d.Previous.Object["data"]
		assert.False(t, hasData)
		stringData, ok := d.Previous.Object["stringData"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "old", stringData["password"])
	})
	t.Run("crd empty limits is drift", func(t *testing.T) {
		t.Parallel()
		manifest := workloadManifest("example.com/v1", "Widget")
		live := &fakeLive{}
		seedLive(t, live, manifest, "prod", func(obj *unstructured.Unstructured) {
			dropLimits(t, obj)
		})
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
	})
	t.Run("empty annotations are drift", func(t *testing.T) {
		t.Parallel()
		previous := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: prod
  annotations: {}
spec: {}
`
		live := &fakeLive{}
		seedLive(t, live, previous, "prod", func(obj *unstructured.Unstructured) {
			obj.SetAnnotations(nil)
		})
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
	})
	t.Run("null omitted is drift", func(t *testing.T) {
		t.Parallel()
		previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\ndata:\n  key: null\n"
		live := &fakeLive{}
		seedLive(t, live, configMapYAML("app", "prod", "ignored"), "prod", func(obj *unstructured.Unstructured) {
			delete(obj.Object, "data")
		})
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
	})
	t.Run("empty list omitted is drift", func(t *testing.T) {
		t.Parallel()
		previous := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app\n  namespace: prod\nspec:\n  entries: []\n"
		live := &fakeLive{}
		seedLive(t, live, configMapYAML("app", "prod", "v"), "prod", func(obj *unstructured.Unstructured) {
			delete(obj.Object, "data")
		})
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
	})
	t.Run("empty limits map declares no keys", func(t *testing.T) {
		t.Parallel()
		manifest := workloadManifest("apps/v1", "Deployment")
		live := &fakeLive{}
		seedLive(t, live, manifest, "prod", func(obj *unstructured.Unstructured) {
			setLimits(t, obj, map[string]any{"cpu": "1"})
		})
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Empty(t, p.Drift)
	})
}

func TestBuildSemanticPlan_DriftListSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		prev string
		live string
		path string
		op   semantic.FieldOp
	}{
		{name: "extra live element", prev: listManifest([]string{"a"}), live: listManifest([]string{"a", "b"}), path: "/spec/entries/1", op: semantic.FieldAdd},
		{name: "missing live element", prev: listManifest([]string{"a", "b"}), live: listManifest([]string{"a"}), path: "/spec/entries/1", op: semantic.FieldRemove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			live := &fakeLive{}
			seedLive(t, live, tt.live, "prod", nil)
			p := buildDrift(t, upgradeClient(tt.prev, tt.prev, 4), newMapper(), live)
			d := onlyDrift(t, p)
			assert.Equal(t, semantic.DriftModified, d.Action)
			require.NotEmpty(t, d.Fields)
			assert.Equal(t, tt.path, d.Fields[0].Path)
			assert.Equal(t, tt.op, d.Fields[0].Op)
		})
	}
	t.Run("reordered", func(t *testing.T) {
		t.Parallel()
		prev := listManifest([]string{"a", "b"})
		live := &fakeLive{}
		seedLive(t, live, listManifest([]string{"b", "a"}), "prod", nil)
		p := buildDrift(t, upgradeClient(prev, prev, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftModified, d.Action)
	})
	t.Run("undeclared key inside element", func(t *testing.T) {
		t.Parallel()
		prev := `apiVersion: example.com/v1
kind: Widget
metadata:
  name: app
  namespace: prod
spec:
  entries:
    - name: a
      value: "1"
`
		liveYAML := `apiVersion: example.com/v1
kind: Widget
metadata:
  name: app
  namespace: prod
spec:
  entries:
    - name: a
      value: "1"
      extra: x
`
		live := &fakeLive{}
		seedLive(t, live, liveYAML, "prod", nil)
		p := buildDrift(t, upgradeClient(prev, prev, 4), newMapper(), live)
		assert.Empty(t, p.Drift)
	})
}

func TestBuildSemanticPlan_DriftIndependentOfReleaseIntent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		previous string
		desired  string
		live     string
	}{
		{name: "live differs from both", previous: "a", desired: "b", live: "c"},
		{name: "live already matches desired", previous: "a", desired: "b", live: "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			previous := configMapYAML("app", "prod", tt.previous)
			desired := configMapYAML("app", "prod", tt.desired)
			live := &fakeLive{}
			seedLive(t, live, configMapYAML("app", "prod", tt.live), "prod", nil)
			p := buildDrift(t, upgradeClient(previous, desired, 4), newMapper(), live)
			require.Len(t, p.Changes, 1)
			assert.Equal(t, semantic.Update, p.Changes[0].Action)
			before, ok := p.Changes[0].Before.Object["data"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.previous, before["key"])
			after, ok := p.Changes[0].After.Object["data"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.desired, after["key"])
			d := onlyDrift(t, p)
			assert.Equal(t, semantic.DriftModified, d.Action)
			liveData, ok := d.Live.Object["data"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.live, liveData["key"])
		})
	}
	t.Run("delete and missing", func(t *testing.T) {
		t.Parallel()
		previous := configMapYAML("app", "prod", "a")
		p := buildDrift(t, upgradeClient(previous, "", 4), newMapper(), &fakeLive{})
		require.Len(t, p.Changes, 1)
		assert.Equal(t, semantic.Delete, p.Changes[0].Action)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftMissing, d.Action)
	})
	t.Run("drift does not change helm action", func(t *testing.T) {
		t.Parallel()
		manifest := configMapYAML("app", "prod", "same")
		live := &fakeLive{}
		seedLive(t, live, configMapYAML("app", "prod", "drifted"), "prod", nil)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Equal(t, semantic.HelmNone, p.HelmAction)
		assert.Empty(t, p.Changes)
		assert.NotEmpty(t, p.Drift)

		hook := planHook("migrate", "old")
		changed := planHook("migrate", "new")
		client := upgradePrepWithHook(manifest, manifest, 3, 4, hook, changed)
		resolved := resolvedSpec()
		resolved.Tasks = map[string]spec.ResolvedTask{
			"migrate": {Task: spec.Task{On: spec.TaskOnPreDeploy}, HookWeight: 1},
		}
		hooked, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), live, buildInput("ctx", resolved, nil))
		t.Cleanup(cleanup)
		require.NoError(t, err)
		assert.Equal(t, semantic.HelmUpgrade, hooked.HelmAction)
		require.Len(t, hooked.Tasks, 1)
		assert.Equal(t, semantic.TaskUpdate, hooked.Tasks[0].Action)
		assert.True(t, hooked.Tasks[0].WillRun)
	})
	t.Run("noop with drift", func(t *testing.T) {
		t.Parallel()
		manifest := configMapYAML("app", "prod", "same")
		live := &fakeLive{}
		seedLive(t, live, configMapYAML("app", "prod", "drifted"), "prod", nil)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.False(t, p.HasEffects())
		assert.True(t, p.IsNoOp())
		assert.True(t, p.HasDrift())
		assert.Equal(t, semantic.Summary{}, p.Summary)
	})
	t.Run("schedule task stays unchanged", func(t *testing.T) {
		t.Parallel()
		previous := cronJobYAML("cleanup", "0 3 * * *")
		live := &fakeLive{}
		seedLive(t, live, previous, "prod", func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedField(obj.Object, "0 4 * * *", "spec", "schedule"))
		})
		client := upgradeClient(previous, previous, 4)
		client.prep.Current.Config = map[string]any{
			"deployah": map[string]any{
				"resolved": map[string]any{
					"tasks": map[string]any{
						"cleanup": map[string]any{"on": string(spec.TaskOnSchedule)},
					},
				},
			},
		}
		resolved := resolvedSpec()
		resolved.Tasks = map[string]spec.ResolvedTask{
			"cleanup": {Task: spec.Task{On: spec.TaskOnSchedule}},
		}
		p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), live, buildInput("ctx", resolved, nil))
		t.Cleanup(cleanup)
		require.NoError(t, err)
		for _, task := range p.Tasks {
			assert.Equal(t, semantic.TaskUnchanged, task.Action)
			assert.False(t, task.WillRun)
		}
		d := onlyDrift(t, p)
		assert.Equal(t, "cleanup", d.Resource.Name)
		assert.Equal(t, semantic.DriftModified, d.Action)
	})
}

func TestBuildSemanticPlan_UnexpectedDrift(t *testing.T) {
	t.Parallel()
	previous := configMapYAML("app", "prod", "same")
	t.Run("release owned extra", func(t *testing.T) {
		t.Parallel()
		live := &fakeLive{}
		seedLive(t, live, previous, "prod", nil)
		live.add(gvr("v1", "ConfigMap"), "prod", "extra", releaseObject("v1", "ConfigMap", "prod", "extra"))
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftUnexpected, d.Action)
		assert.Equal(t, semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "extra"}, d.Resource)
		assert.Nil(t, d.Previous)
		meta, ok := d.Live.Object["metadata"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "extra", meta["name"])
	})
	t.Run("strips last applied annotation", func(t *testing.T) {
		t.Parallel()
		manifest := secretManifest("old")
		live := &fakeLive{}
		seedLive(t, live, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  namespace: prod\ndata:\n  password: b2xk\n", "prod", nil)
		extra := releaseObject("v1", "Secret", "prod", "leaked")
		anns := extra.GetAnnotations()
		anns["kubectl.kubernetes.io/last-applied-configuration"] = `{"data":{"password":"plaintext-secret-payload"}}`
		extra.SetAnnotations(anns)
		require.NoError(t, unstructured.SetNestedMap(extra.Object, map[string]any{"password": "c2VjcmV0"}, "data"))
		live.add(gvr("v1", "Secret"), "prod", "leaked", extra)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftUnexpected, d.Action)
		raw, err := json.Marshal(d.Live.Object)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "kubectl.kubernetes.io/last-applied-configuration")
		assert.NotContains(t, string(raw), "plaintext-secret-payload")
		assert.Contains(t, string(raw), "c2VjcmV0")
	})
	unlabeled := releaseObject("v1", "ConfigMap", "prod", "extra")
	unlabeled.SetLabels(map[string]string{})
	noSource := releaseObject("v1", "ConfigMap", "prod", "nosource")
	noSourceAnns := noSource.GetAnnotations()
	delete(noSourceAnns, spec.AnnotationSource)
	noSource.SetAnnotations(noSourceAnns)
	badSource := releaseObject("v1", "ConfigMap", "prod", "badsource")
	badSourceAnns := badSource.GetAnnotations()
	badSourceAnns[spec.AnnotationSource] = "user"
	badSource.SetAnnotations(badSourceAnns)
	manual := releaseObject("batch/v1", "Job", "prod", "run")
	manualAnns := manual.GetAnnotations()
	delete(manualAnns, "meta.helm.sh/release-name")
	delete(manualAnns, "meta.helm.sh/release-namespace")
	manual.SetAnnotations(manualAnns)
	hooked := releaseObject("v1", "ConfigMap", "prod", "hooked")
	hookedAnns := hooked.GetAnnotations()
	hookedAnns["helm.sh/hook"] = "pre-install"
	hooked.SetAnnotations(hookedAnns)
	ignored := []struct {
		name string
		obj  *unstructured.Unstructured
	}{
		{name: "without instance label", obj: unlabeled},
		{name: "no source", obj: noSource},
		{name: "bad source", obj: badSource},
		{name: "missing helm metadata", obj: manual},
		{name: "helm hook", obj: hooked},
	}
	for _, tt := range ignored {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			obj := tt.obj.DeepCopy()
			live := &fakeLive{}
			seedLive(t, live, previous, "prod", nil)
			live.add(gvr(obj.GetAPIVersion(), obj.GetKind()), obj.GetNamespace(), obj.GetName(), obj)
			p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
			assert.Empty(t, p.Drift)
		})
	}
	t.Run("no global crawl", func(t *testing.T) {
		t.Parallel()
		live := &fakeLive{}
		seedLive(t, live, previous, "prod", nil)
		live.add(gvr("v1", "Secret"), "prod", "db", releaseObject("v1", "Secret", "prod", "db"))
		p := buildDrift(t, upgradeClient(previous, previous, 4), newMapper(), live)
		assert.Empty(t, p.Drift)
		for _, call := range live.Calls() {
			if call.verb == "list" {
				assert.Contains(t, call.resource, "configmaps")
			}
		}
	})
	t.Run("one list for two apiVersions", func(t *testing.T) {
		t.Parallel()
		manifest := joinManifests(hpaManifestNamed("v2beta2", "app", 2), hpaManifestNamed("v2", "other", 2))
		live := &fakeLive{}
		seedLive(t, live, hpaManifestNamed("v2beta2", "app", 2), "prod", nil)
		seedLive(t, live, hpaManifestNamed("v2", "other", 2), "prod", nil)
		extra := releaseObject("autoscaling/v2", "HorizontalPodAutoscaler", "prod", "extra")
		live.add(gvr("autoscaling/v2", "HorizontalPodAutoscaler"), "prod", "extra", extra)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		d := onlyDrift(t, p)
		assert.Equal(t, semantic.DriftUnexpected, d.Action)
		assert.Equal(t, "autoscaling/v2", d.Resource.APIVersion)
		assert.Equal(t, "extra", d.Resource.Name)
		var lists []liveCall
		for _, call := range live.Calls() {
			if call.verb == "list" {
				lists = append(lists, call)
			}
		}
		require.Len(t, lists, 1)
		assert.Equal(t, "v2", lists[0].version)
	})
	t.Run("target namespace is not drift", func(t *testing.T) {
		t.Parallel()
		manifest := kindYAML("v1", "Namespace", "other", "", "")
		live := &fakeLive{}
		seedLive(t, live, manifest, "", nil)
		target := releaseObject("v1", "Namespace", "", "prod")
		live.add(gvr("v1", "Namespace"), "", "prod", target)
		p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
		assert.Empty(t, p.Drift)
	})
}

func TestBuildSemanticPlan_GenerateNameDrift(t *testing.T) {
	t.Parallel()
	named := configMapYAML("app", "prod", "v")
	generated := `apiVersion: v1
kind: ConfigMap
metadata:
  generateName: app-
  namespace: prod
data:
  key: v
`
	manifest := joinManifests(named, generated)
	live := &fakeLive{}
	seedLive(t, live, named, "prod", nil)
	extra := releaseObject("v1", "ConfigMap", "prod", "app-xyz")
	live.add(gvr("v1", "ConfigMap"), "prod", "app-xyz", extra)
	p := buildDrift(t, upgradeClient(manifest, manifest, 4), newMapper(), live)
	assert.Empty(t, p.Drift)
	for _, call := range live.Calls() {
		assert.NotEqual(t, "list", call.verb)
		assert.NotContains(t, call.name, "app-")
	}
	require.Len(t, gets(live), 1)
	assert.Equal(t, "app", gets(live)[0].name)
}

func TestBuildSemanticPlan_DriftErrors(t *testing.T) {
	t.Parallel()
	previous := configMapYAML("app", "prod", "v")
	t.Run("get", func(t *testing.T) {
		t.Parallel()
		live := &fakeLive{}
		live.failGet("app", errors.New("boom"))
		p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), upgradeClient(previous, previous, 4), newMapper(), live, buildInput("ctx", resolvedSpec(), nil))
		t.Cleanup(cleanup)
		require.Error(t, err)
		assert.ErrorContains(t, err, "observe drift")
		assert.ErrorContains(t, err, "read live")
		assert.Zero(t, p)
		assert.Nil(t, result)
	})
	t.Run("list", func(t *testing.T) {
		t.Parallel()
		live := &fakeLive{}
		seedLive(t, live, previous, "prod", nil)
		live.failList(gvr("v1", "ConfigMap"), "prod", errors.New("list failed"))
		p, result, cleanup, err := plan.BuildSemanticPlan(t.Context(), upgradeClient(previous, previous, 4), newMapper(), live, buildInput("ctx", resolvedSpec(), nil))
		t.Cleanup(cleanup)
		require.Error(t, err)
		assert.ErrorContains(t, err, "observe drift")
		assert.ErrorContains(t, err, "list live")
		assert.Zero(t, p)
		assert.Nil(t, result)
	})
}

func TestBuildSemanticPlan_ChartCRDsDoNotReadLive(t *testing.T) {
	t.Parallel()
	live := &fakeLive{}
	client := installClient(configMapYAML("app", "prod", "v1"))
	in := buildInput("ctx", resolvedSpec(), nil)
	in.CRDDocs = []extras.CRDDoc{{
		Path: "/abs/.deployah/crds/widgets.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
	}}
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, newMapper(), live, in)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.NotNil(t, p.Drift)
	assert.Empty(t, p.Drift)
	assert.Empty(t, live.Calls())
	require.Len(t, p.ChartCRDs, 1)
	for _, d := range p.Drift {
		assert.NotEqual(t, "CustomResourceDefinition", d.Resource.Kind)
	}
}

func TestBuildSemanticPlan_DriftOrderIsStable(t *testing.T) {
	t.Parallel()
	alpha := configMapYAML("alpha", "prod", "v")
	zeta := configMapYAML("zeta", "prod", "v")
	forward := buildDrift(t, upgradeClient(joinManifests(zeta, alpha), joinManifests(zeta, alpha), 4), newMapper(), &fakeLive{})
	backward := buildDrift(t, upgradeClient(joinManifests(alpha, zeta), joinManifests(alpha, zeta), 4), newMapper(), &fakeLive{})
	require.Len(t, forward.Drift, 2)
	assert.Equal(t, []string{"alpha", "zeta"}, []string{forward.Drift[0].Resource.Name, forward.Drift[1].Resource.Name})
	assert.Equal(t, forward.Drift[0].Resource, backward.Drift[0].Resource)
	assert.Equal(t, forward.Drift[1].Resource, backward.Drift[1].Resource)
}

func buildDrift(t *testing.T, client *fakeBuildClient, mapper plan.RESTMapper, live *fakeLive) semantic.Plan {
	t.Helper()
	p, _, cleanup, err := plan.BuildSemanticPlan(t.Context(), client, mapper, live, buildInput("ctx", resolvedSpec(), nil))
	t.Cleanup(cleanup)
	require.NoError(t, err)
	return p
}

func onlyDrift(t *testing.T, p semantic.Plan) semantic.DriftChange {
	t.Helper()
	require.Len(t, p.Drift, 1)
	return p.Drift[0]
}

func gets(live *fakeLive) []liveCall {
	var out []liveCall
	for _, call := range live.Calls() {
		if call.verb == "get" {
			out = append(out, call)
		}
	}
	return out
}

func gvr(apiVersion, kind string) schema.GroupVersionResource {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		panic(err)
	}
	return schema.GroupVersionResource{Group: gv.Group, Version: gv.Version, Resource: strings.ToLower(kind) + "s"}
}

func seedLive(t *testing.T, live *fakeLive, manifest, lookupNS string, mutate func(*unstructured.Unstructured)) {
	t.Helper()
	for _, obj := range parseManifest(t, manifest) {
		stored := gvr(obj.GetAPIVersion(), obj.GetKind())
		name := obj.GetName()
		if mutate != nil {
			mutate(obj)
		}
		live.add(stored, lookupNS, name, obj)
	}
}

func parseManifest(t *testing.T, manifest string) []*unstructured.Unstructured {
	t.Helper()
	infos, err := resource.NewLocalBuilder().
		ContinueOnError().
		Flatten().
		Unstructured().
		Stream(bytes.NewBufferString(manifest), "manifest").
		Do().Infos()
	require.NoError(t, err)
	out := make([]*unstructured.Unstructured, 0, len(infos))
	for _, info := range infos {
		obj, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			m, convErr := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
			require.NoError(t, convErr)
			obj = &unstructured.Unstructured{Object: m}
		}
		out = append(out, obj.DeepCopy())
	}
	return out
}

func stampRelease(obj *unstructured.Unstructured, release, namespace string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[spec.LabelInstance] = release
	obj.SetLabels(labels)
	anns := obj.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	anns[spec.AnnotationSource] = spec.SourceSpec
	anns["meta.helm.sh/release-name"] = release
	anns["meta.helm.sh/release-namespace"] = namespace
	obj.SetAnnotations(anns)
}

func releaseObject(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	meta := map[string]any{"name": name}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   meta,
	}}
	stampRelease(obj, "web", "prod")
	return obj
}

func hpaManifestNamed(version, name string, replicas int) string {
	return fmt.Sprintf(`apiVersion: autoscaling/%s
kind: HorizontalPodAutoscaler
metadata:
  name: %s
  namespace: prod
spec:
  maxReplicas: %d
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: app
`, version, name, replicas)
}

func secretManifest(password string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: db
  namespace: prod
stringData:
  password: %s
`, password)
}

func workloadManifest(apiVersion, kind string) string {
	body := `spec:
  template:
    spec:
      containers:
      - name: app
        image: app:1
        resources:
          limits: {}
          requests:
            cpu: 100m
`
	if kind == "CronJob" {
		body = `spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: app
            image: app:1
            resources:
              limits: {}
              requests:
                cpu: 100m
`
	}
	if kind == "Widget" {
		body = `spec:
  template:
    spec:
      containers:
      - name: app
        resources:
          limits: {}
`
	}
	return fmt.Sprintf("apiVersion: %s\nkind: %s\nmetadata:\n  name: app\n  namespace: prod\n%s", apiVersion, kind, body)
}

func listManifest(items []string) string {
	var b strings.Builder
	b.WriteString("apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: app\n  namespace: prod\nspec:\n  entries:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "    - value: %s\n", item)
	}
	return b.String()
}

func dropLimits(t *testing.T, obj *unstructured.Unstructured) {
	t.Helper()
	containers := containersOf(t, obj)
	for _, item := range containers {
		container, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resources, ok := container["resources"].(map[string]any)
		if !ok {
			continue
		}
		delete(resources, "limits")
	}
}

func setLimits(t *testing.T, obj *unstructured.Unstructured, limits map[string]any) {
	t.Helper()
	containers := containersOf(t, obj)
	require.NotEmpty(t, containers)
	item, ok := containers[0].(map[string]any)
	require.True(t, ok)
	resources, ok := item["resources"].(map[string]any)
	if !ok || resources == nil {
		resources = map[string]any{}
		item["resources"] = resources
	}
	resources["limits"] = limits
}

func containersOf(t *testing.T, obj *unstructured.Unstructured) []any {
	t.Helper()
	path := []string{"spec", "template", "spec", "containers"}
	if obj.GetKind() == "CronJob" {
		path = []string{"spec", "jobTemplate", "spec", "template", "spec", "containers"}
	}
	if obj.GetKind() == "Widget" {
		path = []string{"spec", "template", "spec", "containers"}
	}
	cur := any(obj.Object)
	for _, key := range path {
		node, ok := cur.(map[string]any)
		require.True(t, ok, "missing %s", key)
		next, ok := node[key]
		require.True(t, ok, "missing %s", key)
		cur = next
	}
	list, ok := cur.([]any)
	require.True(t, ok)
	return list
}
