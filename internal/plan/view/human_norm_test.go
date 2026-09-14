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

func TestWriteHuman_SecretDiffs(t *testing.T) {
	t.Parallel()
	addAfter := secretObj("s", "same-pass", "same-tok")
	secretData(t, addAfter)["extra"] = "added-secret"
	removeBefore := secretObj("s", "same-pass", "same-tok")
	secretData(t, removeBefore)["extra"] = "removed-secret"
	emptySecret := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "s", "namespace": "prod"},
	}
	withData := secretObj("s", "added-pass", "added-tok")
	escaped := func(slash, tilde, keep string) map[string]any {
		return map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]any{"name": "s", "namespace": "prod"},
			"data":       map[string]any{"foo/bar": slash, "tilde~x": tilde, "keep": keep},
		}
	}

	tests := []struct {
		name        string
		before      map[string]any
		after       map[string]any
		showSecrets bool
		contains    []string
		omits       []string
	}{
		{
			name:   "replace hides secrets",
			before: secretObj("s", "old-pass", "same-tok"),
			after:  secretObj("s", "new-pass", "same-tok"),
			contains: []string{
				"-   password: (redacted)",
				"+   password: (redacted)",
			},
			omits: []string{
				"token:",
				"old-pass", "new-pass", "same-tok", "deployah-secret-",
			},
		},
		{
			name:        "replace shows secrets",
			before:      secretObj("s", "old-pass", "same-tok"),
			after:       secretObj("s", "new-pass", "same-tok"),
			showSecrets: true,
			contains: []string{
				"-   password: old-pass",
				"+   password: new-pass",
			},
			omits: []string{"token:", "same-tok"},
		},
		{
			name:   "add key",
			before: secretObj("s", "same-pass", "same-tok"),
			after:  addAfter,
			contains: []string{
				"+   extra: (redacted)",
			},
			omits: []string{
				"-   extra:", "-   password:", "+   password:",
				"-   token:", "+   token:",
				"added-secret", "same-pass", "deployah-secret-",
			},
		},
		{
			name:   "remove key",
			before: removeBefore,
			after:  secretObj("s", "same-pass", "same-tok"),
			contains: []string{
				"-   extra: (redacted)",
			},
			omits: []string{
				"+   extra:", "-   password:", "+   password:",
				"removed-secret", "deployah-secret-",
			},
		},
		{
			name:   "escaped keys",
			before: escaped("old-slash", "old-tilde", "same"),
			after:  escaped("new-slash", "new-tilde", "same"),
			contains: []string{
				"-   foo/bar: (redacted)",
				"+   foo/bar: (redacted)",
				"-   tilde~x: (redacted)",
				"+   tilde~x: (redacted)",
			},
			omits: []string{
				"keep:",
				"old-slash", "new-slash", "old-tilde", "new-tilde", "deployah-secret-",
			},
		},
		{
			name:     "add whole data map",
			before:   emptySecret,
			after:    withData,
			contains: []string{"+ data:", "(redacted)"},
			omits:    []string{"added-pass", "added-tok"},
		},
		{
			name:     "remove whole data map",
			before:   withData,
			after:    emptySecret,
			contains: []string{"- data:", "(redacted)"},
			omits:    []string{"added-pass"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPlan(t, []semantic.ResourceChange{{
				Resource: ref("Secret", "s"),
				Origin:   helmOrigin(),
				Action:   semantic.Update,
				Before:   snap(tt.before),
				After:    snap(tt.after),
				Apply:    writeApply(),
			}}, nil)
			var buf bytes.Buffer
			require.NoError(t, view.WriteHuman(&buf, p, view.Options{ShowSecrets: tt.showSecrets}))
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
	assert.NotContains(t, text, "example.com/keep")
	assert.Contains(t, jsonBuf.String(), `"resourceVersion"`)
	assert.Contains(t, jsonBuf.String(), `"managedFields"`)
	assert.Contains(t, jsonBuf.String(), `"status"`)
	assert.Equal(t, "11", objectString(t, p.Changes[0].Before.Object, "metadata", "resourceVersion"))
}

func TestWriteHuman_OmitsBookkeepingOnCreateDeleteReplace(t *testing.T) {
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
			name: "replace",
			changes: []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "web"),
				Origin:   helmOrigin(),
				Action:   semantic.Replace,
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
	m, ok := obj["data"].(map[string]any)
	require.True(t, ok)
	return m
}
