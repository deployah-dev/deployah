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

func TestWriteHuman_SecretReplaceShowsRedactedMarkers(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(secretObj("s", "old-pass", "same-tok")),
		After:    snap(secretObj("s", "new-pass", "same-tok")),
		Apply:    writeApply(),
	}}, nil)

	var hidden bytes.Buffer
	require.NoError(t, view.WriteHuman(&hidden, p, view.Options{}))
	text := hidden.String()
	assert.Contains(t, text, "-   password: (redacted)")
	assert.Contains(t, text, "+   password: (redacted)")
	assert.NotContains(t, text, "-   token: (redacted)")
	assert.NotContains(t, text, "+   token: (redacted)")
	assert.Contains(t, text, "    token: (redacted)")
	assert.NotContains(t, text, "old-pass")
	assert.NotContains(t, text, "new-pass")
	assert.NotContains(t, text, "same-tok")
	assert.NotContains(t, text, "deployah-secret-")

	var shown bytes.Buffer
	require.NoError(t, view.WriteHuman(&shown, p, view.Options{ShowSecrets: true}))
	shownText := shown.String()
	assert.Contains(t, shownText, "-   password: old-pass")
	assert.Contains(t, shownText, "+   password: new-pass")
	assert.NotContains(t, shownText, "-   token:")
	assert.NotContains(t, shownText, "+   token:")
	assert.Contains(t, shownText, "    token: same-tok")
}

func TestWriteHuman_SecretKeyAdd(t *testing.T) {
	t.Parallel()
	before := secretObj("s", "same-pass", "same-tok")
	after := secretObj("s", "same-pass", "same-tok")
	secretData(t, after)["extra"] = "added-secret"
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(before),
		After:    snap(after),
		Apply:    writeApply(),
	}}, nil)

	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "+   extra: (redacted)")
	assert.NotContains(t, text, "-   extra:")
	assert.NotContains(t, text, "-   password:")
	assert.NotContains(t, text, "+   password:")
	assert.NotContains(t, text, "-   token:")
	assert.NotContains(t, text, "+   token:")
	assert.NotContains(t, text, "added-secret")
	assert.NotContains(t, text, "same-pass")
	assert.NotContains(t, text, "deployah-secret-")
}

func TestWriteHuman_SecretKeyRemove(t *testing.T) {
	t.Parallel()
	before := secretObj("s", "same-pass", "same-tok")
	secretData(t, before)["extra"] = "removed-secret"
	after := secretObj("s", "same-pass", "same-tok")
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("Secret", "s"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(before),
		After:    snap(after),
		Apply:    writeApply(),
	}}, nil)

	var buf bytes.Buffer
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
	text := buf.String()
	assert.Contains(t, text, "-   extra: (redacted)")
	assert.NotContains(t, text, "+   extra:")
	assert.NotContains(t, text, "-   password:")
	assert.NotContains(t, text, "+   password:")
	assert.NotContains(t, text, "removed-secret")
	assert.NotContains(t, text, "deployah-secret-")
}

func TestWriteHuman_OmitsBookkeepingFields(t *testing.T) {
	t.Parallel()
	before := noisyCM("web", "v1", "11", "u-live")
	after := noisyCM("web", "v2", "22", "u-pred")
	p := mustPlan(t, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "web"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(before),
		After:    snap(after),
		Apply:    writeApply(),
	}}, nil)

	var human, jsonBuf bytes.Buffer
	require.NoError(t, view.WriteHuman(&human, p, view.Options{}))
	require.NoError(t, view.WriteJSON(&jsonBuf, p, view.Options{}))
	text := human.String()
	assertNoBookkeeping(t, text)
	assert.Contains(t, text, "-   key: v1")
	assert.Contains(t, text, "+   key: v2")
	assertKeepsUserMeta(t, text)
	assert.Contains(t, jsonBuf.String(), `"resourceVersion"`)
	assert.Contains(t, jsonBuf.String(), `"managedFields"`)
	assert.Contains(t, jsonBuf.String(), `"status"`)
	assert.Equal(t, "11", objectString(t, p.Changes[0].Before.Object, "metadata", "resourceVersion"))
}

func TestWriteHuman_OmitsBookkeepingOnCreateDeleteRecreate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		changes []semantic.ResourceChange
		want    []string
	}{
		{
			name: "create",
			changes: []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "web"),
				Origin:   helmOrigin(),
				Action:   semantic.Create,
				After:    snap(noisyCM("web", "v1", "11", "u-new")),
				Apply:    writeApply(),
			}},
			want: []string{"+   key: v1"},
		},
		{
			name: "delete",
			changes: []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "web"),
				Origin:   helmOrigin(),
				Action:   semantic.Delete,
				Before:   snap(noisyCM("web", "v1", "11", "u-old")),
				Apply:    deleteApply(),
			}},
			want: []string{"-   key: v1"},
		},
		{
			name: "recreate",
			changes: []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "web"),
				Origin:   helmOrigin(),
				Action:   semantic.Recreate,
				Before:   snap(noisyCM("web", "v1", "11", "u-live")),
				After:    snap(noisyCM("web", "v2", "22", "u-pred")),
				Apply:    bothApply(),
			}},
			want: []string{"-   key: v1", "+   key: v2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, tt.changes, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{}))
			text := buf.String()
			assertNoBookkeeping(t, text)
			assertKeepsUserMeta(t, text)
			for _, line := range tt.want {
				assert.Contains(t, text, line)
			}
		})
	}
}

func secretData(t *testing.T, obj map[string]any) map[string]any {
	t.Helper()
	raw, ok := obj["data"]
	require.True(t, ok)
	m, ok := raw.(map[string]any)
	require.True(t, ok)
	return m
}

func assertNoBookkeeping(t *testing.T, text string) {
	t.Helper()
	assert.NotContains(t, text, "resourceVersion")
	assert.NotContains(t, text, "managedFields")
	assert.NotContains(t, text, "creationTimestamp")
	assert.NotContains(t, text, "observedGeneration")
	assert.NotContains(t, text, "uid:")
	assert.NotContains(t, text, "generation:")
	assert.NotContains(t, text, "status:")
}

func assertKeepsUserMeta(t *testing.T, text string) {
	t.Helper()
	assert.Contains(t, text, "example.com/keep")
	assert.Contains(t, text, "cert-manager.io/issue-temporary-certificate")
	assert.Contains(t, text, "app: web")
}
