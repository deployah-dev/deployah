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

func TestWriteHuman_CreateUpdateDeleteRecreate(t *testing.T) {
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
			Action:   semantic.Recreate,
			Before:   snap(cm("rs", "v1")),
			After:    snap(cm("rs", "v2")),
			Apply:    bothApply(),
		},
	}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assertGolden(t, "human_all_actions", text)
	assert.Contains(t, text, "ConfigMap/prod/app create helm")
	assert.Contains(t, text, "ConfigMap/prod/web update helm")
	assert.Contains(t, text, "ConfigMap/prod/old delete helm")
	assert.Contains(t, text, "ConfigMap/prod/rs recreate helm")
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
	assert.Contains(t, buf.String(), "completeness: partial")
	assert.Contains(t, buf.String(), "prediction_limitation")
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
	}, nil, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "context: kind-dev")
	assert.Contains(t, text, "revision: 7")
	assert.Contains(t, text, "fresh_install: true")
	assert.Contains(t, text, "completeness: complete")
	assert.Contains(t, text, "Executions: none")
}

func TestWriteHuman_DiagnosticWithoutResource(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{Release: "web"}, nil, []semantic.Diagnostic{{
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
	assert.Contains(t, text, "ConfigMap/prod/web update helm")
	assert.Contains(t, text, "  key: same")
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
	assert.Contains(t, text, "completeness: complete")
	assert.Contains(t, text, "Executions: none")
	assert.Contains(t, text, "create: 0")
	assert.NotContains(t, text, "create helm")
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
