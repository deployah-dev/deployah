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

func TestSecretRedaction_DefaultAndShowSecrets(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "old-tok")),
		After:    snap(secretObj("s", "new-pass", "new-tok")),
		Apply:    writeApply(),
	}}, nil)

	var hidden bytes.Buffer
	require.NoError(t, view.WriteHuman(&hidden, p, view.Options{}))
	text := hidden.String()
	assert.NotContains(t, text, "old-pass")
	assert.NotContains(t, text, "new-pass")
	assert.NotContains(t, text, "old-tok")
	assert.NotContains(t, text, "new-tok")
	assert.Contains(t, text, "/data/token")
	assert.Contains(t, text, "/stringData/password")
	assert.Contains(t, text, "(redacted)")
	assert.Contains(t, text, "s")

	var shown bytes.Buffer
	require.NoError(t, view.WriteHuman(&shown, p, view.Options{ShowSecrets: true}))
	assert.Contains(t, shown.String(), "old-pass")
	assert.Contains(t, shown.String(), "new-pass")
}

func TestSecretRedaction_ConfigMapNotRedacted(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(cm("app", "old")),
		After:    snap(cm("app", "new")),
		Apply:    writeApply(),
	}}, nil)
	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	assert.Contains(t, buf.String(), "old")
	assert.Contains(t, buf.String(), "new")
	assert.NotContains(t, buf.String(), "(redacted)")
}

func TestSecretRedaction_FieldChangeComputedBeforeRedaction(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "same")),
		After:    snap(secretObj("s", "new-pass", "same")),
		Apply:    writeApply(),
	}}, nil)
	require.NotEmpty(t, p.Changes[0].Fields)
	found := false
	for _, f := range p.Changes[0].Fields {
		if f.Path == "/stringData/password" {
			found = true
			assert.Equal(t, "old-pass", f.Before)
			assert.Equal(t, "new-pass", f.After)
		}
	}
	assert.True(t, found, "secret field change must exist on the unredacted plan")

	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	assert.Contains(t, buf.String(), "/stringData/password")
	assert.NotContains(t, buf.String(), "old-pass")
	assert.Equal(t, "old-pass", objectString(t, p.Changes[0].Before.Object, "stringData", "password"))
}
