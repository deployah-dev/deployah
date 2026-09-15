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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
)

func TestWriteHuman_CreateUpdateDeleteReplace(t *testing.T) {
	t.Parallel()
	p := allActionsPlan(t)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assertGolden(t, "human_all_actions", text) // full-output contract; Contains below are invariants only
	assertHumanLayout(t, text)
	assertHumanHeaderMetadata(t, text, "production-eu", 12)
	assert.Contains(t, text, "Resources")
	assert.Contains(t, text, `+ create v1/ConfigMap "app"`)
	assert.Contains(t, text, "+     example.com/keep: yes")
	assert.Contains(t, text, "+     app: web")
	assert.Contains(t, text, "+   key: v1")
	assert.Contains(t, text, `~ update v1/ConfigMap "web"`)
	assert.Contains(t, text, `- delete v1/ConfigMap "old"`)
	assert.Contains(t, text, "-     example.com/keep: yes")
	assert.Contains(t, text, "-     app: web")
	assert.Contains(t, text, "-   key: v1")
	assert.Contains(t, text, `-/+ replace v1/ConfigMap "rs"`)
	assert.Contains(t, text, "Tasks")
	assert.Contains(t, text, "  preDeploy")
	assert.Contains(t, text, "~ migrate  changed, will run")
	assert.Contains(t, text, `~ update v1/ConfigMap "web-migrate-env"`)
	assert.Contains(t, text, "    seed  will run")
	assert.Contains(t, text, "  postDeploy")
	assert.Contains(t, text, "+ smoke  new, will run")
	assert.Contains(t, text, `+ create batch/v1/Job "web-smoke"`)
	assert.Contains(t, text, "deployah.dev/task: smoke")
	assert.Contains(t, text, "helm.sh/hook: post-install,post-upgrade")
	assert.Contains(t, text, `helm.sh/hook-weight: "0"`)
	assert.Contains(t, text, "backoffLimit: 1")
	assert.Contains(t, text, "image: ghcr.io/example/web:1.2.3")
	assert.Contains(t, text, "- ./smoke")
	assert.Contains(t, text, "- old-check  removed")
	assert.Contains(t, text, `- delete batch/v1/Job "web-old-check"`)
	assert.Contains(t, text, "deployah.dev/task: old-check")
	assert.Contains(t, text, `helm.sh/hook-weight: "1"`)
	assert.Contains(t, text, "- ./old-check")
	assert.Contains(t, text, "  schedule")
	assert.Contains(t, text, "~ cleanup  changed")
	assert.NotContains(t, text, "~ cleanup  changed, will run")
	assert.Contains(t, text, `~ update batch/v1/CronJob "web-cleanup"`)
	assert.Contains(t, text, "-   schedule: 0 2 * * *")
	assert.Contains(t, text, "+   schedule: 0 3 * * *")
	assert.NotContains(t, text, "jobTemplate:")
	assert.Equal(t, 1, strings.Count(text, `batch/v1/CronJob "web-cleanup"`))
	assert.Contains(t, text, "  Resources: 1 create, 2 update, 1 delete, 1 replace")
	assert.Contains(t, text, "  Tasks: 3 to run, 1 schedule changed")
	assert.NotContains(t, text, "~ ConfigMap/prod/rs")
	assert.NotContains(t, text, "-/+ ConfigMap/prod/web")
	assert.NotContains(t, strings.ToLower(text), "recreate")
	assert.NotContains(t, text, "-/+ apiVersion")
	assert.NotContains(t, text, "Actions:")
	assert.NotContains(t, text, "create helm")
	assert.NotContains(t, text, "completeness:")
	assert.NotContains(t, text, "write=")
	assert.NotContains(t, text, "field_manager=")
	assert.NotContains(t, text, "force_conflicts=")
	assert.NotContains(t, text, "delete=")
	assert.Contains(t, text, "+ apiVersion: v1")
	assert.Contains(t, text, "- apiVersion: v1")
	assert.Contains(t, text, "-   key: v1")
	assert.Contains(t, text, "+   key: v2")
	assert.Contains(t, text, "  kind: ConfigMap")
	assert.Contains(t, text, "  apiVersion: v1")
	assert.NotContains(t, text, "before:")
	assert.NotContains(t, text, "after:")
	assert.NotContains(t, text, "--- before")
	assert.NotContains(t, text, "+++ after")
	assert.NotContains(t, text, "@@")
}

func TestWriteHuman_NoOpLimitationDiagnosticsOnly(t *testing.T) {
	t.Parallel()
	widget := semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "app"}
	p := mustPlanWithHeader(t, humanHeader(), semantic.HelmUpgrade, nil, nil, []semantic.Diagnostic{{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: prediction used the currently installed CRD, but that CRD's spec changes before Helm executes",
		Resource: &widget,
	}})
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
	assert.False(t, p.IsNoOp())
	text := writeHuman(t, p)
	assertGolden(t, "human_noop_limitation", text)
	assert.Contains(t, text, "Warning: prediction is incomplete")
	assert.Contains(t, text, "Diagnostics")
	assert.Contains(t, text, `example.com/v1/Widget "app"`)
	assert.NotContains(t, text, "\nResources\n")
}

func TestWriteHuman_Prerequisites(t *testing.T) {
	t.Parallel()
	widgetRef := semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "app"}
	p := mustPlanWithHeader(t, semantic.Header{
		Project:      "web",
		Environment:  "prod",
		Release:      "web",
		Namespace:    "prod",
		Context:      "kind-dev",
		Revision:     1,
		FreshInstall: true,
	}, semantic.HelmInstall, []semantic.ResourceChange{
		{
			Resource: semantic.ResourceRef{
				APIVersion: "apiextensions.k8s.io/v1",
				Kind:       "CustomResourceDefinition",
				Name:       "widgets.example.com",
			},
			Origin: semantic.ResourceOrigin{Kind: semantic.OriginCRD},
			Action: semantic.Create,
			After: snap(map[string]any{
				"apiVersion": "apiextensions.k8s.io/v1",
				"kind":       "CustomResourceDefinition",
				"metadata":   map[string]any{"name": "widgets.example.com"},
			}),
			Apply:      writeCreate(),
			ApplyOrder: 1,
		},
		{
			Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: "prod"},
			Origin:   semantic.ResourceOrigin{Kind: semantic.OriginNamespace},
			Action:   semantic.Create,
			After: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata": map[string]any{
					"name":   "prod",
					"labels": map[string]any{"name": "prod"},
				},
			}),
			Apply:      writeApply(),
			ApplyOrder: 1,
		},
		{
			Resource:   widgetRef,
			Origin:     helmOrigin(),
			Action:     semantic.Create,
			After:      snap(widget("app", "blue")),
			Apply:      writeApply(),
			ApplyOrder: 1,
		},
	}, nil, []semantic.Diagnostic{{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: API example.com/v1/Widget becomes available after CRD widgets.example.com is created earlier in this deployment",
		Resource: &widgetRef,
	}})
	text := writeHuman(t, p)
	assertGolden(t, "human_prerequisites", text)
	assert.Contains(t, text, `+ create apiextensions.k8s.io/v1/CustomResourceDefinition "widgets.example.com"`)
	assert.Contains(t, text, `+ create v1/Namespace "prod"`)
	assert.Contains(t, text, `+ create example.com/v1/Widget "app"`)
	assert.Contains(t, text, "Diagnostics")
}

func TestWriteHuman_DiagnosticPartial(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "web")
	p := mustPlanWithHeader(t, humanHeader(), semantic.HelmUpgrade, []semantic.ResourceChange{
		knownConfigMapUpdate(res),
	}, nil, []semantic.Diagnostic{predictionLimitation(res)})
	assert.Equal(t, semantic.CompletenessPartial, p.Completeness)
	text := writeHuman(t, p)
	assertGolden(t, "human_partial", text) // full-output contract; Contains below are invariants only
	assert.Contains(t, text, "Warning: prediction is incomplete")
	assert.Contains(t, text, `~ update v1/ConfigMap "web"`)
	assert.Contains(t, text, "-   key: v1")
	assert.Contains(t, text, "+   key: v2")
	assert.Contains(t, text, "prediction_limitation")
	assert.NotContains(t, text, "completeness:")
	assert.NotContains(t, text, "Actions:")
}

func TestWriteHuman_PartialPreservesKnownDetails(t *testing.T) {
	t.Parallel()
	header, changes, tasks := allActionsInputs()
	complete := mustPlanWithHeader(t, header, semantic.HelmUpgrade, changes, tasks, nil)
	partial := mustPlanWithHeader(t, header, semantic.HelmUpgrade, changes, tasks, []semantic.Diagnostic{
		predictionLimitation(ref("ConfigMap", "web")),
	})
	assert.Equal(t, semantic.CompletenessComplete, complete.Completeness)
	assert.Equal(t, semantic.CompletenessPartial, partial.Completeness)
	assert.Equal(t, complete.Summary, partial.Summary)

	completeText := writeHuman(t, complete)
	partialText := writeHuman(t, partial)
	assert.Contains(t, partialText, "Warning: prediction is incomplete")
	assert.Contains(t, partialText, "Diagnostics")
	assert.NotContains(t, completeText, "Warning: prediction is incomplete")
	assert.NotContains(t, completeText, "Diagnostics")
	require.Equal(t, completeText, stripPartialPresentation(t, partialText))
}

func TestWriteHuman_DeterministicMapOrder(t *testing.T) {
	t.Parallel()
	first := map[string]any{"z": "1", "a": "1", "m": "1"}
	second := map[string]any{"a": "1", "m": "1", "z": "1"}
	p1 := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(first),
		Apply:    writeApply(),
	}}, nil)
	p2 := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(second),
		Apply:    writeApply(),
	}}, nil)
	var b1, b2 bytes.Buffer
	require.NoError(t, view.WriteHuman(&b1, p1, view.Options{}))
	require.NoError(t, view.WriteHuman(&b2, p2, view.Options{}))
	assert.Equal(t, b1.String(), b2.String())
}

func TestWriteHuman_DoesNotHTMLEscape(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(map[string]any{"note": "plain"}),
		After:    snap(map[string]any{"note": map[string]any{"html": "a < b & c"}}),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, `a < b & c`)
	assert.NotContains(t, text, `\u003c`)
	assert.NotContains(t, text, `\u0026`)
}

func TestWriteHuman_InvalidZero(t *testing.T) {
	t.Parallel()
	err := view.WriteHuman(&bytes.Buffer{}, semantic.Plan{}, view.Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid completeness")
}

func TestWriteHuman_DoesNotMutatePlan(t *testing.T) {
	t.Parallel()
	obj := secretObj("s", "old", "tok")
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(obj),
		After:    snap(secretObj("s", "new", "tok2")),
		Apply:    writeApply(),
	}}, nil)
	before := objectString(t, p.Changes[0].Before.Object, "stringData", "password")
	require.NoError(t, view.WriteHuman(&bytes.Buffer{}, p, view.Options{}))
	assert.Equal(t, before, objectString(t, p.Changes[0].Before.Object, "stringData", "password"))
}

func TestWriteHuman_KubernetesKeyOrder(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"spec":       map[string]any{"replicas": 1, "paused": false},
		"kind":       "Deployment",
		"apiVersion": "apps/v1",
		"metadata": map[string]any{
			"labels":    map[string]any{"z": "1", "a": "1"},
			"namespace": "prod",
			"name":      "web",
			"uid":       "u1",
		},
	}
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Deployment", "web"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(obj),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	api := strings.Index(text, "+ apiVersion: apps/v1")
	kind := strings.Index(text, "+ kind: Deployment")
	meta := strings.Index(text, "+ metadata:")
	spec := strings.Index(text, "+ spec:")
	require.Greater(t, api, -1)
	assert.Greater(t, kind, api)
	assert.Greater(t, meta, kind)
	assert.Greater(t, spec, meta)
	name := strings.Index(text, "+   name: web")
	ns := strings.Index(text, "+   namespace: prod")
	labels := strings.Index(text, "+   labels:")
	assert.Greater(t, ns, name)
	assert.Greater(t, labels, ns)
	assert.NotContains(t, text, "uid:")
	a := strings.Index(text, "a: \"1\"")
	if a < 0 {
		a = strings.Index(text, "+     a: 1")
	}
	z := strings.Index(text, "z: \"1\"")
	if z < 0 {
		z = strings.Index(text, "+     z: 1")
	}
	require.Greater(t, a, -1)
	require.Greater(t, z, -1)
	assert.Greater(t, z, a)
}

func TestWriteHuman_GenerateNameBeforeNamespace(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"kind":       "ConfigMap",
		"apiVersion": "v1",
		"metadata": map[string]any{
			"namespace":    "prod",
			"generateName": "app-",
		},
	}
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", GenerateName: "app-"},
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(obj),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	gen := strings.Index(text, "+   generateName: app-")
	ns := strings.Index(text, "+   namespace: prod")
	require.Greater(t, gen, -1)
	assert.Greater(t, ns, gen)
	assert.NotContains(t, text, "+   name:")
}

func TestWriteHuman_ArrayOrderPreserved(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "app", "namespace": "prod"},
		"data":       map[string]any{"items": []any{"zeta", "alpha", "mu"}},
	}
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(obj),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	zeta := strings.Index(text, "zeta")
	alpha := strings.Index(text, "alpha")
	mu := strings.Index(text, "mu")
	require.Greater(t, zeta, -1)
	assert.Greater(t, alpha, zeta)
	assert.Greater(t, mu, alpha)
}

func TestWriteHuman_HeaderMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		header   semantic.Header
		context  string
		revision int
	}{
		{
			name:     "upgrade next revision",
			header:   humanHeader(),
			context:  "production-eu",
			revision: 12,
		},
		{
			name: "fresh install revision 1",
			header: semantic.Header{
				Project:      "web",
				Environment:  "prod",
				Release:      "web",
				Namespace:    "prod",
				Context:      "kind-dev",
				Revision:     1,
				FreshInstall: true,
			},
			context:  "kind-dev",
			revision: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			helmAction := semantic.HelmNone
			if tt.header.FreshInstall {
				helmAction = semantic.HelmInstall
			}
			p := mustPlanWithHeader(t, tt.header, helmAction, nil, nil, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			assertHumanLayout(t, text)
			assertHumanHeaderMetadata(t, text, tt.context, tt.revision)
			assert.Equal(t, tt.context, p.Header.Context)
			assert.Equal(t, tt.revision, p.Header.Revision)
			assert.NotContains(t, text, "fresh_install")
			assert.NotContains(t, text, "FreshInstall")
			assert.NotContains(t, text, "completeness:")
			assert.NotContains(t, text, "Executions:")
		})
	}
}

func TestWriteHuman_DiagnosticWithoutResource(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{Release: "web"}, semantic.HelmNone, nil, nil, []semantic.Diagnostic{{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: cluster-scoped",
	}})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "warning prediction_limitation: prediction is not exact: cluster-scoped")
	assert.NotContains(t, text, "ConfigMap/")
}

func TestWriteHuman_BookkeepingOnlyUpdateKeepsResource(t *testing.T) {
	t.Parallel()
	before := noisyCM("web", "same", "11", "u-live")
	after := noisyCM("web", "same", "22", "u-pred")
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "web"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(before),
		After:    snap(after),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, `~ update v1/ConfigMap "web"`)
	assert.NotContains(t, text, "-   key:")
	assert.NotContains(t, text, "+   key:")
	assertNoBookkeeping(t, text)
}

func TestWriteHuman_EmptyPlan(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmNone, nil, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, `Plan for project "web" on environment "prod"`)
	assert.NotContains(t, text, "completeness:")
	assert.NotContains(t, text, "Executions:")
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  Resources: 0 create, 0 update, 0 delete, 0 replace")
	assert.NotContains(t, text, "  Tasks:")
	assert.NotContains(t, text, "create helm")
	assert.NotContains(t, text, "Actions:")
}

func TestWriteHuman_WriterError(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()}, nil)
	err := view.WriteHuman(errWriter{}, p, view.Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "write failed")
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteHuman_ZeroThemeIsPlainText(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	assert.NotContains(t, buf.String(), "\x1b[")
	assert.Contains(t, buf.String(), "+ apiVersion: v1")
	assert.Contains(t, buf.String(), "+ kind: ConfigMap")
}

func TestWriteHuman_UnknownGVKActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		change   semantic.ResourceChange
		contains []string
		omits    []string
	}{
		{
			name: "create dumps full object",
			change: semantic.ResourceChange{
				Resource: semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "w"},
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(widget("w", "blue")),
				Apply:    writeApply(),
			},
			contains: []string{`+ create example.com/v1/Widget "w"`, "+ apiVersion: example.com/v1", "size: large"},
		},
		{
			name: "update projects changed leaves",
			change: semantic.ResourceChange{
				Resource: semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "u"},
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   snap(widget("u", "blue")),
				After:    snap(widget("u", "red")),
				Apply:    writeApply(),
			},
			contains: []string{`~ update example.com/v1/Widget "u"`, "color:"},
			omits:    []string{"size"},
		},
		{
			name: "delete dumps full object",
			change: semantic.ResourceChange{
				Resource: semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "d"},
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   snap(widget("d", "blue")),
				Apply:    deleteApply(),
			},
			contains: []string{`- delete example.com/v1/Widget "d"`, "- apiVersion: example.com/v1", "size: large"},
		},
		{
			name: "replace identity then projected rest",
			change: semantic.ResourceChange{
				Resource: semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "w"},
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
				Before: snap(map[string]any{
					"apiVersion": "example.com/v1",
					"kind":       "Widget",
					"metadata":   map[string]any{"name": "w", "namespace": "prod"},
					"spec":       map[string]any{"color": "blue", "keep": "yes"},
					"status":     map[string]any{"ready": true},
				}),
				After: snap(map[string]any{
					"apiVersion": "example.com/v1",
					"kind":       "Widget",
					"metadata":   map[string]any{"name": "w", "namespace": "prod"},
					"spec":       map[string]any{"color": "red", "keep": "yes"},
					"status":     map[string]any{"ready": true},
				}),
				Apply: bothApply(),
			},
			contains: []string{
				`-/+ replace example.com/v1/Widget "w"`,
				"apiVersion: example.com/v1",
				"kind: Widget",
				"metadata:",
				"color: blue",
				"color: red",
			},
			omits: []string{"keep: yes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change}, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			for _, want := range tt.contains {
				assert.Contains(t, text, want)
			}
			for _, omit := range tt.omits {
				assert.NotContains(t, text, omit)
			}
		})
	}
}

func TestWriteHuman_GVKAndNamespaceHeadings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ref     semantic.ResourceRef
		heading string
		omits   []string
	}{
		{
			name:    "core group omits core prefix and plan namespace",
			ref:     semantic.ResourceRef{APIVersion: "v1", Kind: "Service", Namespace: "prod", Name: "api"},
			heading: `+ create v1/Service "api"`,
			omits:   []string{"core/v1/Service", "in namespace"},
		},
		{
			name:    "core/v1 apiVersion still renders as v1",
			ref:     semantic.ResourceRef{APIVersion: "core/v1", Kind: "Service", Namespace: "prod", Name: "legacy"},
			heading: `+ create v1/Service "legacy"`,
			omits:   []string{"core/v1/Service"},
		},
		{
			name:    "custom group includes group",
			ref:     semantic.ResourceRef{APIVersion: "monitoring.coreos.com/v1", Kind: "ServiceMonitor", Namespace: "prod", Name: "api"},
			heading: `+ create monitoring.coreos.com/v1/ServiceMonitor "api"`,
			omits:   []string{"in namespace"},
		},
		{
			name:    "other namespace",
			ref:     semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "kube-system", Name: "foo"},
			heading: `+ create v1/ConfigMap "foo" in namespace "kube-system"`,
		},
		{
			name:    "cluster scoped omits namespace clause",
			ref:     semantic.ResourceRef{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "read"},
			heading: `+ create rbac.authorization.k8s.io/v1/ClusterRole "read"`,
			omits:   []string{"in namespace"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
				Resource: tt.ref,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(k8sObj(tt.ref.APIVersion, tt.ref.Kind, tt.ref.Namespace, tt.ref.Name)),
				Apply:    writeApply(),
			}}, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			assert.Contains(t, text, tt.heading)
			for _, omit := range tt.omits {
				assert.NotContains(t, text, omit)
			}
		})
	}
}

func TestWriteHuman_UpdateProjection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ref      semantic.ResourceRef
		before   map[string]any
		after    map[string]any
		contains []string
		omits    []string
	}{
		{
			name: "deployment container image keeps name omits siblings",
			ref:  semantic.ResourceRef{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "prod", Name: "api"},
			before: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]any{"name": "api", "namespace": "prod"},
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{
									"name":  "api",
									"image": "old",
									"ports": []any{map[string]any{"containerPort": 8080}},
									"env":   []any{map[string]any{"name": "X", "value": "1"}},
								},
							},
						},
					},
				},
			},
			after: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   map[string]any{"name": "api", "namespace": "prod"},
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{
									"name":  "api",
									"image": "new",
									"ports": []any{map[string]any{"containerPort": 8080}},
									"env":   []any{map[string]any{"name": "X", "value": "1"}},
								},
							},
						},
					},
				},
			},
			contains: []string{`~ update apps/v1/Deployment "api"`, "name: api", "image: old", "image: new"},
			omits:    []string{"ports", "containerPort", "env:"},
		},
		{
			name: "custom resource keeps list name omits extra",
			ref:  semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "w"},
			before: map[string]any{
				"apiVersion": "example.com/v1",
				"kind":       "Widget",
				"metadata":   map[string]any{"name": "w", "namespace": "prod"},
				"spec": map[string]any{
					"items": []any{
						map[string]any{"name": "alpha", "value": "1", "extra": "no"},
					},
				},
			},
			after: map[string]any{
				"apiVersion": "example.com/v1",
				"kind":       "Widget",
				"metadata":   map[string]any{"name": "w", "namespace": "prod"},
				"spec": map[string]any{
					"items": []any{
						map[string]any{"name": "alpha", "value": "2", "extra": "no"},
					},
				},
			},
			contains: []string{"name: alpha", "value:"},
			omits:    []string{"extra"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
				Resource: tt.ref,
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   snap(tt.before),
				After:    snap(tt.after),
				Apply:    writeApply(),
			}}, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			for _, want := range tt.contains {
				assert.Contains(t, text, want)
			}
			for _, omit := range tt.omits {
				assert.NotContains(t, text, omit)
			}
		})
	}
}

func TestWriteHuman_ScheduledTaskNotDuplicated(t *testing.T) {
	t.Parallel()
	cron := semantic.ResourceRef{APIVersion: "batch/v1", Kind: "CronJob", Namespace: "prod", Name: "cleanup"}
	api := ref("ConfigMap", "api")
	p := mustPlanWithTasks(t, semantic.HelmUpgrade, []semantic.ResourceChange{
		{
			Resource: api,
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    snap(cm("api", "v1")),
			Apply:    writeApply(),
		},
		{
			Resource: cron,
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before: snap(map[string]any{
				"apiVersion": "batch/v1",
				"kind":       "CronJob",
				"metadata":   map[string]any{"name": "cleanup", "namespace": "prod"},
				"spec":       map[string]any{"schedule": "0 2 * * *"},
			}),
			After: snap(map[string]any{
				"apiVersion": "batch/v1",
				"kind":       "CronJob",
				"metadata":   map[string]any{"name": "cleanup", "namespace": "prod"},
				"spec":       map[string]any{"schedule": "0 3 * * *"},
			}),
			Apply: writeApply(),
		},
	}, []semantic.TaskPlan{{
		Name:      "cleanup",
		Phase:     semantic.TaskSchedule,
		Action:    semantic.TaskUpdate,
		Resources: []semantic.ResourceRef{cron},
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "Resources")
	assert.Contains(t, text, `+ create v1/ConfigMap "api"`)
	assert.Contains(t, text, "Tasks")
	assert.Contains(t, text, "schedule")
	assert.Contains(t, text, "~ cleanup  changed")
	assert.Contains(t, text, `~ update batch/v1/CronJob "cleanup"`)
	assert.Equal(t, 1, strings.Count(text, `batch/v1/CronJob "cleanup"`))
	resourcesIdx := strings.Index(text, "Resources")
	tasksIdx := strings.Index(text, "Tasks")
	require.Greater(t, tasksIdx, resourcesIdx)
	assert.NotContains(t, text[resourcesIdx:tasksIdx], `CronJob "cleanup"`)
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  Resources: 1 create, 1 update, 0 delete, 0 replace")
	assert.Contains(t, text, "  Tasks: 0 to run, 1 schedule changed")
}

func TestWriteHuman_ScheduleCreateDeleteRendersFullObject(t *testing.T) {
	t.Parallel()
	cron := batchRef("CronJob", "web-cleanup")
	body := []string{
		"deployah.dev/task: cleanup",
		"jobTemplate:",
		"backoffLimit: 1",
		"image: ghcr.io/example/web:1.2.3",
		"./cleanup",
		"schedule: 0 3 * * *",
	}
	tests := []struct {
		name     string
		change   semantic.ResourceChange
		task     semantic.TaskPlan
		contains []string
		omits    []string
	}{
		{
			name: "create",
			change: semantic.ResourceChange{
				Resource: cron,
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(cronJob("web-cleanup", "cleanup", "0 3 * * *")),
				Apply:    writeApply(),
			},
			task: semantic.TaskPlan{
				Name:      "cleanup",
				Phase:     semantic.TaskSchedule,
				Action:    semantic.TaskCreate,
				Resources: []semantic.ResourceRef{cron},
			},
			contains: append([]string{
				"+ cleanup  new",
				`+ create batch/v1/CronJob "web-cleanup"`,
				"  Resources: 1 create, 0 update, 0 delete, 0 replace",
			}, body...),
			omits: []string{"will run", "\nResources\n"},
		},
		{
			name: "delete",
			change: semantic.ResourceChange{
				Resource: cron,
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   snap(cronJob("web-cleanup", "cleanup", "0 3 * * *")),
				Apply:    deleteApply(),
			},
			task: semantic.TaskPlan{
				Name:      "cleanup",
				Phase:     semantic.TaskSchedule,
				Action:    semantic.TaskDelete,
				Resources: []semantic.ResourceRef{cron},
			},
			contains: append([]string{
				"- cleanup  removed",
				`- delete batch/v1/CronJob "web-cleanup"`,
				"  Resources: 0 create, 0 update, 1 delete, 0 replace",
			}, body...),
			omits: []string{"will run", "\nResources\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlanWithTasks(t, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change}, []semantic.TaskPlan{tt.task}, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			assertHumanLayout(t, text)
			assert.Contains(t, text, "Tasks")
			assert.Contains(t, text, "  schedule")
			assert.Contains(t, text, "  Tasks: 0 to run, 1 schedule changed")
			for _, want := range tt.contains {
				assert.Contains(t, text, want)
			}
			for _, omit := range tt.omits {
				assert.NotContains(t, text, omit)
			}
		})
	}
}

func TestWriteHuman_UnchangedTaskWillRun(t *testing.T) {
	t.Parallel()
	p := mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
		Name:    "seed",
		Phase:   semantic.TaskPreDeploy,
		Action:  semantic.TaskUnchanged,
		WillRun: true,
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "seed  will run")
	assert.NotContains(t, text, "apiVersion")
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  Tasks: 1 to run")
	assert.NotContains(t, text, "schedule changed")
}

func TestWriteHuman_TaskFooter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		tasks           []semantic.TaskPlan
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "unchanged schedule omitted",
			tasks: []semantic.TaskPlan{{
				Name:   "cleanup",
				Phase:  semantic.TaskSchedule,
				Action: semantic.TaskUnchanged,
			}},
			wantContains:    []string{"Tasks: 0 to run"},
			wantNotContains: []string{"schedule changed"},
		},
		{
			name: "changed schedule counted",
			tasks: []semantic.TaskPlan{{
				Name:   "cleanup",
				Phase:  semantic.TaskSchedule,
				Action: semantic.TaskUpdate,
			}},
			wantContains: []string{"Tasks: 0 to run, 1 schedule changed"},
		},
		{
			name: "unchanged hook to run",
			tasks: []semantic.TaskPlan{{
				Name:    "seed",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUnchanged,
				WillRun: true,
			}},
			wantContains:    []string{"Tasks: 1 to run"},
			wantNotContains: []string{"schedule changed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlanWithTasks(t, semantic.HelmUpgrade, nil, tt.tasks, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			assertHumanLayout(t, text)
			assert.Contains(t, text, "  Resources: 0 create, 0 update, 0 delete, 0 replace")
			for _, s := range tt.wantContains {
				assert.Contains(t, text, s)
			}
			for _, s := range tt.wantNotContains {
				assert.NotContains(t, text, s)
			}
		})
	}
}

func TestWriteHuman_BlockSpacing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		plan            semantic.Plan
		contains        []string
		wantTasksFooter bool
	}{
		{
			name: "resource blocks",
			plan: mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{
				{
					Resource: ref("ConfigMap", "app"),
					Origin:   helmOrigin(),
					Action:   semantic.Create,
					After:    snap(cm("app", "v1")),
					Apply:    writeApply(),
				},
				{
					Resource: ref("ConfigMap", "old"),
					Origin:   helmOrigin(),
					Action:   semantic.Create,
					After:    snap(cm("old", "v1")),
					Apply:    writeApply(),
				},
			}, nil),
			contains: []string{
				"+   key: v1\n\n  + create v1/ConfigMap \"old\"",
				"  Resources: 2 create, 0 update, 0 delete, 0 replace",
			},
		},
		{
			name: "task blocks",
			plan: mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{
				{
					Name:    "migrate",
					Phase:   semantic.TaskPreDeploy,
					Action:  semantic.TaskUpdate,
					WillRun: true,
				},
				{
					Name:    "seed",
					Phase:   semantic.TaskPreDeploy,
					Action:  semantic.TaskUnchanged,
					WillRun: true,
				},
			}, nil),
			contains:        []string{"~ migrate  changed, will run\n\n    seed  will run"},
			wantTasksFooter: true,
		},
		{
			name: "nested definition blocks",
			plan: mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{{
				Name:    "migrate",
				Phase:   semantic.TaskPreDeploy,
				Action:  semantic.TaskUpdate,
				WillRun: true,
				Definitions: []semantic.HookDefinition{
					{
						Resource: ref("ConfigMap", "a-env"),
						Action:   semantic.Create,
						After:    snap(cm("a-env", "v1")),
					},
					{
						Resource: ref("ConfigMap", "z-env"),
						Action:   semantic.Create,
						After:    snap(cm("z-env", "v1")),
					},
				},
			}}, nil),
			contains:        []string{"+   key: v1\n\n      + create v1/ConfigMap \"z-env\""},
			wantTasksFooter: true,
		},
		{
			name: "task phases",
			plan: mustPlanWithTasks(t, semantic.HelmUpgrade, nil, []semantic.TaskPlan{
				{
					Name:    "seed",
					Phase:   semantic.TaskPreDeploy,
					Action:  semantic.TaskUnchanged,
					WillRun: true,
				},
				{
					Name:    "smoke",
					Phase:   semantic.TaskPostDeploy,
					Action:  semantic.TaskCreate,
					WillRun: true,
				},
			}, nil),
			contains:        []string{"    seed  will run\n\n  postDeploy\n"},
			wantTasksFooter: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, tt.plan, view.Options{}))
			text := buf.String()
			assertHumanLayout(t, text)
			for _, want := range tt.contains {
				assert.Contains(t, text, want)
			}
			assert.Equal(t, tt.wantTasksFooter, strings.Contains(text, "  Tasks:"))
		})
	}
}

func TestWriteHuman_SummaryOmitsTasksWhenAbsent(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  Resources: 1 create, 0 update, 0 delete, 0 replace")
	assert.NotContains(t, text, "  Tasks:")
}

// allActionsPlan is the synthetic generic renderer fixture for
// human_all_actions.golden. It is not the semantic-pipeline contract;
// that is TestWriteHuman_SemanticPlan / human_semantic_plan.golden.
func allActionsPlan(tb testing.TB) semantic.Plan {
	tb.Helper()
	header, changes, tasks := allActionsInputs()
	return mustPlanWithHeader(tb, header, semantic.HelmUpgrade, changes, tasks, nil)
}

func allActionsInputs() (semantic.Header, []semantic.ResourceChange, []semantic.TaskPlan) {
	cron := batchRef("CronJob", "web-cleanup")
	changes := []semantic.ResourceChange{
		{
			Resource: ref("ConfigMap", "app"),
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    snap(cmWithMeta("app", "v1")),
			Apply:    writeApply(),
		},
		{
			Resource: ref("ConfigMap", "web"),
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before:   snap(cm("web", "v1")),
			After:    snap(cm("web", "v2")),
			Apply:    writeApply(),
		},
		{
			Resource: ref("ConfigMap", "old"),
			Origin:   helmOrigin(),
			Action:   semantic.Delete,
			Before:   snap(cmWithMeta("old", "v1")),
			Apply:    deleteApply(),
		},
		{
			Resource: ref("ConfigMap", "rs"),
			Origin:   helmOrigin(),
			Action:   semantic.Replace,
			Before:   snap(cm("rs", "v1")),
			After:    snap(cm("rs", "v2")),
			Apply:    bothApply(),
		},
		{
			Resource: cron,
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before:   snap(cronJob("web-cleanup", "cleanup", "0 2 * * *")),
			After:    snap(cronJob("web-cleanup", "cleanup", "0 3 * * *")),
			Apply:    writeApply(),
		},
	}
	tasks := []semantic.TaskPlan{
		{
			Name:    "migrate",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskUpdate,
			WillRun: true,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("ConfigMap", "web-migrate-env"),
				Action:   semantic.Update,
				Before:   snap(cm("web-migrate-env", "v1")),
				After:    snap(cm("web-migrate-env", "v2")),
			}},
		},
		{
			Name:    "seed",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskUnchanged,
			WillRun: true,
		},
		{
			Name:       "smoke",
			Phase:      semantic.TaskPostDeploy,
			Action:     semantic.TaskCreate,
			WillRun:    true,
			HookWeight: 0,
			Definitions: []semantic.HookDefinition{{
				Resource: batchRef("Job", "web-smoke"),
				Action:   semantic.Create,
				After:    snap(jobObj("web-smoke", "smoke", "post-install,post-upgrade", "0")),
			}},
		},
		{
			Name:       "old-check",
			Phase:      semantic.TaskPostDeploy,
			Action:     semantic.TaskDelete,
			HookWeight: 1,
			Definitions: []semantic.HookDefinition{{
				Resource: batchRef("Job", "web-old-check"),
				Action:   semantic.Delete,
				Before:   snap(jobObj("web-old-check", "old-check", "post-install,post-upgrade", "1")),
			}},
		},
		{
			Name:      "cleanup",
			Phase:     semantic.TaskSchedule,
			Action:    semantic.TaskUpdate,
			Resources: []semantic.ResourceRef{cron},
		},
	}
	return humanHeader(), changes, tasks
}

func assertHumanLayout(t *testing.T, text string) {
	t.Helper()
	assert.NotContains(t, text, "\n\n\n")
	assert.True(t, strings.HasSuffix(text, "\n"))
	assert.False(t, strings.HasSuffix(text, "\n\n"))
	assert.Contains(t, text, "\n\nSummary\n")
	assert.NotContains(t, text, "Plan:")
	assert.Contains(t, text, "\n  Resources:")
}

func knownConfigMapUpdate(res semantic.ResourceRef) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(cm(res.Name, "v1")),
		After:    snap(cm(res.Name, "v2")),
		Apply:    writeApply(),
	}
}

func predictionLimitation(res semantic.ResourceRef) semantic.Diagnostic {
	return semantic.Diagnostic{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: managed-fields-migration",
		Resource: new(res),
	}
}

func stripPartialPresentation(t *testing.T, text string) string {
	t.Helper()
	const warning = "Warning: prediction is incomplete\n\n"
	require.Contains(t, text, warning)
	text = strings.Replace(text, warning, "", 1)
	diagStart := strings.Index(text, "\nDiagnostics\n")
	summaryStart := strings.Index(text, "\nSummary\n")
	require.Greater(t, diagStart, -1)
	require.Greater(t, summaryStart, diagStart)
	return text[:diagStart] + text[summaryStart:]
}

func assertHumanHeaderMetadata(t *testing.T, text, kubeContext string, revision int) {
	t.Helper()
	block := fmt.Sprintf("Context:   %s\nNamespace: prod\nRelease:   web\nRevision:  %d\n", kubeContext, revision)
	assert.Contains(t, text, block)
	assert.Contains(t, text, "\n\nContext:")
	contextIdx := strings.Index(text, "Context:")
	nsIdx := strings.Index(text, "Namespace:")
	relIdx := strings.Index(text, "Release:")
	revIdx := strings.Index(text, "Revision:")
	require.Greater(t, contextIdx, -1)
	assert.Greater(t, nsIdx, contextIdx)
	assert.Greater(t, relIdx, nsIdx)
	assert.Greater(t, revIdx, relIdx)
}
