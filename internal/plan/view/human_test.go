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
	assertGolden(t, "human_all_actions", buf.String())
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
