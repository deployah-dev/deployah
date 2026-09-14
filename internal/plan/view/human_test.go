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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
)

func TestWriteHuman_CreateUpdateDeleteReplace(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{
		{
			Resource: ref("ConfigMap", "app"),
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
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
			Before:   snap(cm("old", "v1")),
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
	}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assertGolden(t, "human_all_actions", text)
	assert.Contains(t, text, `+ create v1/ConfigMap "app"`)
	assert.Contains(t, text, `~ update v1/ConfigMap "web"`)
	assert.Contains(t, text, `- delete v1/ConfigMap "old"`)
	assert.Contains(t, text, `-/+ replace v1/ConfigMap "rs"`)
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

func TestWriteHuman_DiagnosticPartial(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(cm("app", "v1")),
		Apply:    writeApply(),
	}}, []semantic.Diagnostic{{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  "prediction is not exact: managed-fields-migration",
		Resource: &res,
	}})
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	assertGolden(t, "human_partial", buf.String())
	assert.Contains(t, buf.String(), "Warning: prediction is incomplete")
	assert.Contains(t, buf.String(), "prediction_limitation")
	assert.NotContains(t, buf.String(), "completeness:")
	assert.NotContains(t, buf.String(), "Actions:")
}

func TestWriteHuman_DeterministicMapOrder(t *testing.T) {
	t.Parallel()
	first := map[string]any{"z": "1", "a": "1", "m": "1"}
	second := map[string]any{"a": "1", "m": "1", "z": "1"}
	p1 := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(first),
		Apply:    writeApply(),
	}}, nil)
	p2 := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlan(t, []semantic.ResourceChange{{
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

func TestWriteHuman_HeaderOptionalFields(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{
		Project:      "web",
		Environment:  "prod",
		Release:      "web",
		Namespace:    "prod",
		Context:      "kind-dev",
		Revision:     7,
		FreshInstall: true,
	}, nil, nil, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, `Plan for project "web" on environment "prod"`)
	assert.Contains(t, text, "Context:   kind-dev")
	assert.Contains(t, text, "Revision:  7")
	assert.NotContains(t, text, "fresh_install")
	assert.NotContains(t, text, "completeness:")
	assert.NotContains(t, text, "Executions:")
}

func TestWriteHuman_DiagnosticWithoutResource(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{Release: "web"}, nil, nil, []semantic.Diagnostic{{
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
	p := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlan(t, nil, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, `Plan for project "web" on environment "prod"`)
	assert.NotContains(t, text, "completeness:")
	assert.NotContains(t, text, "Executions:")
	assert.Contains(t, text, "Plan: 0 create, 0 update, 0 delete, 0 replace")
	assert.NotContains(t, text, "create helm")
	assert.NotContains(t, text, "Actions:")
}

func TestWriteHuman_WriterError(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{createChangeForHuman()}, nil)
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
	p := mustPlan(t, []semantic.ResourceChange{createChangeForHuman()}, nil)
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
			contains: []string{`- delete example.com/v1/Widget "d"`, "- apiVersion: example.com/v1"},
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
			p := mustPlan(t, []semantic.ResourceChange{tt.change}, nil)
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
			p := mustPlan(t, []semantic.ResourceChange{{
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
			p := mustPlan(t, []semantic.ResourceChange{{
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
	p := mustPlanWithTasks(t, []semantic.ResourceChange{
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
	assert.Contains(t, text, "Tasks: 0 to run, 1 schedule changed")
}

func TestWriteHuman_UnchangedTaskWillRun(t *testing.T) {
	t.Parallel()
	p := mustPlanWithTasks(t, nil, []semantic.TaskPlan{{
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
	assert.Contains(t, text, "Tasks: 1 to run")
	assert.NotContains(t, text, "schedule changed")
}
