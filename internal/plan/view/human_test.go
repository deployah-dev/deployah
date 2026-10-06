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

func TestWriteHuman_CreateUpdateDelete(t *testing.T) {
	t.Parallel()
	text := writeHuman(t, allActionsPlan(t))
	assertGolden(t, "human_all_actions", text)
	assert.NotContains(t, text, "~ ConfigMap/prod/rs")
}

func TestWriteHuman_DeterministicMapOrder(t *testing.T) {
	t.Parallel()
	first := map[string]any{"z": "1", "a": "1", "m": "1"}
	second := map[string]any{"a": "1", "m": "1", "z": "1"}
	p1 := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    snap(first),
	}})
	p2 := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Create,
		After:    snap(second),
	}})
	assert.Equal(t, writeHuman(t, p1), writeHuman(t, p2))
}

func TestWriteHuman_DoesNotHTMLEscape(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Update,
		Before:   snap(map[string]any{"note": "plain"}),
		After:    snap(map[string]any{"note": map[string]any{"html": "a < b & c"}}),
	}})
	text := writeHuman(t, p)
	assert.Contains(t, text, `a < b & c`)
	assert.NotContains(t, text, `\u003c`)
	assert.NotContains(t, text, `\u0026`)
}

func TestWriteHuman_InvalidZero(t *testing.T) {
	t.Parallel()
	err := view.WriteHuman(&bytes.Buffer{}, semantic.Plan{}, view.Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid helmAction")
}

func TestWriteHuman_DoesNotMutatePlan(t *testing.T) {
	t.Parallel()
	obj := secretObj("s", "old", "tok")
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Action:   semantic.Update,
		Before:   snap(obj),
		After:    snap(secretObj("s", "new", "tok2")),
	}})
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
		Action:   semantic.Create,
		After:    snap(obj),
	}})
	text := writeHuman(t, p)
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
		Action:   semantic.Create,
		After:    snap(obj),
	}})
	text := writeHuman(t, p)
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
		Action:   semantic.Create,
		After:    snap(obj),
	}})
	text := writeHuman(t, p)
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
			p := mustPlanWithHeader(t, tt.header, helmAction, nil, nil)
			text := writeHuman(t, p)
			assertHumanLayout(t, text)
			assertHumanHeaderMetadata(t, text, tt.context, tt.revision)
			assert.Equal(t, tt.context, p.Header.Context)
			assert.Equal(t, tt.revision, p.Header.Revision)
		})
	}
}

func TestWriteHuman_EmptyPlan(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmNone, nil)
	text := writeHuman(t, p)
	assert.Contains(t, text, `Plan for project "web" on environment "prod"`)
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  Resources: 0 create, 0 update, 0 delete")
	assert.NotContains(t, text, "  Tasks:")
}

func TestWriteHuman_WriterError(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()})
	err := view.WriteHuman(errWriter{}, p, view.Options{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "write failed")
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteHuman_NilStylerIsPlainText(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()})
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
				Action:   semantic.Create,
				After:    snap(widget("w", "blue")),
			},
			contains: []string{`+ create example.com/v1/Widget "w"`, "+ apiVersion: example.com/v1", "size: large"},
		},
		{
			name: "update projects changed leaves",
			change: semantic.ResourceChange{
				Resource: semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "u"},
				Action:   semantic.Update,
				Before:   snap(widget("u", "blue")),
				After:    snap(widget("u", "red")),
			},
			contains: []string{`~ update example.com/v1/Widget "u"`, "color:"},
			omits:    []string{"size"},
		},
		{
			name: "delete dumps full object",
			change: semantic.ResourceChange{
				Resource: semantic.ResourceRef{APIVersion: "example.com/v1", Kind: "Widget", Namespace: "prod", Name: "d"},
				Action:   semantic.Delete,
				Before:   snap(widget("d", "blue")),
			},
			contains: []string{`- delete example.com/v1/Widget "d"`, "- apiVersion: example.com/v1", "size: large"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change})
			text := writeHuman(t, p)
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
				Action:   semantic.Create,
				After:    snap(k8sObj(tt.ref.APIVersion, tt.ref.Kind, tt.ref.Namespace, tt.ref.Name)),
			}})
			text := writeHuman(t, p)
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
				Action:   semantic.Update,
				Before:   snap(tt.before),
				After:    snap(tt.after),
			}})
			text := writeHuman(t, p)
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
			Action:   semantic.Create,
			After:    snap(cm("api", "v1")),
		},
		{
			Resource: cron,
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
		},
	}, []semantic.TaskPlan{{
		Name:      "cleanup",
		Phase:     semantic.TaskSchedule,
		Action:    semantic.TaskUpdate,
		Resources: []semantic.ResourceRef{cron},
	}})
	text := writeHuman(t, p)
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
	assert.Contains(t, text, "  Resources: 1 create, 1 update, 0 delete")
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
				Action:   semantic.Create,
				After:    snap(cronJob("web-cleanup", "cleanup", "0 3 * * *")),
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
				"  Resources: 1 create, 0 update, 0 delete",
			}, body...),
			omits: []string{"will run", "\nResources\n"},
		},
		{
			name: "delete",
			change: semantic.ResourceChange{
				Resource: cron,
				Action:   semantic.Delete,
				Before:   snap(cronJob("web-cleanup", "cleanup", "0 3 * * *")),
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
				"  Resources: 0 create, 0 update, 1 delete",
			}, body...),
			omits: []string{"will run", "\nResources\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlanWithTasks(t, semantic.HelmUpgrade, []semantic.ResourceChange{tt.change}, []semantic.TaskPlan{tt.task})
			text := writeHuman(t, p)
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
	}})
	text := writeHuman(t, p)
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
			p := mustPlanWithTasks(t, semantic.HelmUpgrade, nil, tt.tasks)
			text := writeHuman(t, p)
			assertHumanLayout(t, text)
			assert.Contains(t, text, "  Resources: 0 create, 0 update, 0 delete")
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
					Action:   semantic.Create,
					After:    snap(cm("app", "v1")),
				},
				{
					Resource: ref("ConfigMap", "old"),
					Action:   semantic.Create,
					After:    snap(cm("old", "v1")),
				},
			}),
			contains: []string{
				"+   key: v1\n\n  + create v1/ConfigMap \"old\"",
				"  Resources: 2 create, 0 update, 0 delete",
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
			}),
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
			}}),
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
			}),
			contains:        []string{"    seed  will run\n\n  postDeploy\n"},
			wantTasksFooter: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text := writeHuman(t, tt.plan)
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
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()})
	text := writeHuman(t, p)
	assertHumanLayout(t, text)
	assert.Contains(t, text, "  Resources: 1 create, 0 update, 0 delete")
	assert.NotContains(t, text, "  Tasks:")
}

// allActionsPlan is the synthetic generic renderer fixture for
// human_all_actions.golden. It is not the semantic-pipeline contract;
// that is TestWriteHuman_SemanticPlan / human_semantic_plan.golden.
func allActionsPlan(tb testing.TB) semantic.Plan {
	tb.Helper()
	header, changes, tasks := allActionsInputs()
	return mustPlanWithHeader(tb, header, semantic.HelmUpgrade, changes, tasks)
}

func allActionsInputs() (semantic.Header, []semantic.ResourceChange, []semantic.TaskPlan) {
	cron := batchRef("CronJob", "web-cleanup")
	changes := []semantic.ResourceChange{
		{
			Resource: ref("ConfigMap", "app"),
			Action:   semantic.Create,
			After:    snap(cmWithMeta("app", "v1")),
		},
		{
			Resource: ref("ConfigMap", "web"),
			Action:   semantic.Update,
			Before:   snap(cm("web", "v1")),
			After:    snap(cm("web", "v2")),
		},
		{
			Resource: ref("ConfigMap", "old"),
			Action:   semantic.Delete,
			Before:   snap(cmWithMeta("old", "v1")),
		},
		{
			Resource: cron,
			Action:   semantic.Update,
			Before:   snap(cronJob("web-cleanup", "cleanup", "0 2 * * *")),
			After:    snap(cronJob("web-cleanup", "cleanup", "0 3 * * *")),
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

func humanDriftPlan(t *testing.T) semantic.Plan {
	t.Helper()
	return mustPlanInput(t, semantic.Input{
		Header:     humanHeader(),
		HelmAction: semantic.HelmNone,
		Drift: []semantic.DriftChange{
			{
				Resource: ref("ConfigMap", "app"),
				Action:   semantic.DriftModified,
				Previous: snap(cm("app", "old")),
				Live:     snap(cm("app", "new")),
				Fields: []semantic.FieldChange{{
					Path:   "/data/key",
					Op:     semantic.FieldReplace,
					Before: "old",
					After:  "new",
				}},
			},
			{
				Resource: ref("ConfigMap", "other"),
				Action:   semantic.DriftMissing,
				Previous: snap(cm("other", "gone")),
			},
			{
				Resource: ref("ConfigMap", "extra"),
				Action:   semantic.DriftUnexpected,
				Live:     snap(cm("extra", "live")),
			},
		},
	})
}

func TestWriteHuman_DriftSection(t *testing.T) {
	t.Parallel()
	p := humanDriftPlan(t)
	text := writeHuman(t, p)
	assertGolden(t, "human_drift", text)
	assert.Less(t, strings.Index(text, "\nDrift\n"), strings.Index(text, "\nSummary\n"))
	assert.NotContains(t, text, "Resources\n")
}

func TestWriteHuman_EmptyDriftHasNoSection(t *testing.T) {
	t.Parallel()
	p := mustPlanWithHeader(t, humanHeader(), semantic.HelmNone, nil, nil)
	assert.NotContains(t, writeHuman(t, p), "Drift")
}

func TestWriteHuman_DriftSecretNormalizedPath(t *testing.T) {
	t.Parallel()
	p := mustPlanInput(t, semantic.Input{
		Header: semantic.Header{
			Project:     "web",
			Environment: "prod",
			Release:     "web",
			Namespace:   "prod",
		},
		HelmAction: semantic.HelmNone,
		Drift: []semantic.DriftChange{{
			Resource: ref("Secret", "db"),
			Action:   semantic.DriftModified,
			Previous: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]any{"name": "db", "namespace": "prod"},
				"stringData": map[string]any{"password": "old"},
			}),
			Live: snap(map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]any{"name": "db", "namespace": "prod"},
				"data":       map[string]any{"password": "bmV3"},
			}),
			Fields: []semantic.FieldChange{{
				Path:   "/data/password",
				Op:     semantic.FieldReplace,
				Before: "b2xk",
				After:  "bmV3",
			}},
		}},
	})

	tests := []struct {
		name string
		opts view.Options
		has  []string
		omit []string
	}{
		{
			name: "redacted",
			has:  []string{"data:", "- ", "+ ", "password: (redacted)"},
			omit: []string{"old", "b2xk", "bmV3"},
		},
		{
			name: "shown",
			opts: view.Options{ShowSecrets: true},
			has:  []string{"password: b2xk", "password: bmV3"},
			omit: []string{"old", "(redacted)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, tt.opts))
			text := buf.String()
			for _, want := range tt.has {
				assert.Contains(t, text, want)
			}
			for _, omit := range tt.omit {
				assert.NotContains(t, text, omit)
			}
		})
	}
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
