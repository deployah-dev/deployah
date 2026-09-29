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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
)

type styledLine struct {
	role view.Role
	line string
}

type recordingStyler struct {
	lines []styledLine
}

func (s *recordingStyler) Style(role view.Role, line string) string {
	s.lines = append(s.lines, styledLine{role: role, line: line})
	return line
}

// markingStyler returns a different string from the one it receives, so
// tests can see that WriteHuman writes the result of Style.
type markingStyler struct{}

func (markingStyler) Style(_ view.Role, line string) string {
	return "<" + line + ">"
}

func TestWriteHuman_StylerPreservesLayout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		plan   semantic.Plan
		golden string
	}{
		{name: "all actions", plan: allActionsPlan(t), golden: "human_all_actions"},
		{name: "drift", plan: humanDriftPlan(t), golden: "human_drift"},
		{
			name:   "semantic",
			plan:   semanticUpgradePlan(t, previousProductSpec(), currentProductSpec()),
			golden: "human_semantic_plan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plain := assertRoleTrace(t, tt.golden+".roles", tt.plan)
			assertGolden(t, tt.golden, plain)
		})
	}
}

func TestWriteHuman_StyleRoles(t *testing.T) {
	t.Parallel()
	header := humanHeader()
	unexpected, err := semantic.AttachDrift(mustPlanWithHeader(t, header, semantic.HelmNone, nil, nil), []semantic.DriftChange{{
		Resource: ref("ConfigMap", "extra"),
		Action:   semantic.DriftUnexpected,
		Live:     snap(cm("extra", "live")),
	}})
	require.NoError(t, err)
	missing, err := semantic.AttachDrift(mustPlanWithHeader(t, header, semantic.HelmNone, nil, nil), []semantic.DriftChange{{
		Resource: ref("ConfigMap", "other"),
		Action:   semantic.DriftMissing,
		Previous: snap(cm("other", "gone")),
	}})
	require.NoError(t, err)
	modified, err := semantic.AttachDrift(mustPlanWithHeader(t, header, semantic.HelmNone, nil, nil), []semantic.DriftChange{{
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
	}})
	require.NoError(t, err)
	tasks := mustPlanWithHeader(t, header, semantic.HelmUpgrade, nil, []semantic.TaskPlan{
		{
			Name:    "migrate",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskUpdate,
			WillRun: true,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("ConfigMap", "env"),
				Action:   semantic.Update,
				Before:   snap(cm("env", "v1")),
				After:    snap(cm("env", "v2")),
			}},
		},
		{
			Name:       "seed",
			Phase:      semantic.TaskPreDeploy,
			Action:     semantic.TaskUnchanged,
			WillRun:    true,
			HookWeight: 1,
		},
		{
			Name:       "smoke",
			Phase:      semantic.TaskPostDeploy,
			Action:     semantic.TaskCreate,
			WillRun:    true,
			HookWeight: 0,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("ConfigMap", "hook"),
				Action:   semantic.Create,
				After:    snap(cm("hook", "v1")),
			}},
		},
		{
			Name:       "old-check",
			Phase:      semantic.TaskPostDeploy,
			Action:     semantic.TaskDelete,
			HookWeight: 1,
			Definitions: []semantic.HookDefinition{{
				Resource: ref("ConfigMap", "gone"),
				Action:   semantic.Delete,
				Before:   snap(cm("gone", "v1")),
			}},
		},
	})
	tests := []struct {
		name   string
		golden string
		plan   semantic.Plan
	}{
		{
			name:   "create",
			golden: "style_create",
			plan: mustPlanWithHeader(t, header, semantic.HelmUpgrade, []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "app"),
				Action:   semantic.Create,
				After:    snap(cm("app", "v1")),
			}}, nil),
		},
		{
			name:   "delete",
			golden: "style_delete",
			plan: mustPlanWithHeader(t, header, semantic.HelmUpgrade, []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "old"),
				Action:   semantic.Delete,
				Before:   snap(cm("old", "v1")),
			}}, nil),
		},
		{
			name:   "update",
			golden: "style_update",
			plan: mustPlanWithHeader(t, header, semantic.HelmUpgrade, []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "web"),
				Action:   semantic.Update,
				Before:   snap(cm("web", "v1")),
				After:    snap(cm("web", "v2")),
			}}, nil),
		},
		{name: "drift unexpected", golden: "style_drift_unexpected", plan: unexpected},
		{name: "drift missing", golden: "style_drift_missing", plan: missing},
		{name: "drift modified", golden: "style_drift_modified", plan: modified},
		{name: "tasks", golden: "style_tasks", plan: tasks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertRoleTrace(t, tt.golden, tt.plan)
		})
	}
}

func TestWriteHuman_ExpandsTabs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		plan  semantic.Plan
		role  view.Role
		lines []string
	}{
		{
			name: "yaml literal",
			plan: mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{{
				Resource: ref("ConfigMap", "app"),
				Action:   semantic.Create,
				After:    snap(cm("app", "line1\nline2\twith tab")),
			}}),
			role: view.RoleDiffAdded,
			lines: []string{
				"  +   key: |-",
				"  +     line2    with tab",
			},
		},
		{
			name: "header context",
			plan: mustPlanWithHeader(t, semantic.Header{
				Project:     "web",
				Environment: "prod",
				Release:     "web",
				Namespace:   "prod",
				Context:     "prod\tuction-eu",
			}, semantic.HelmNone, nil, nil),
			role:  view.RolePrimary,
			lines: []string{"Context:   prod    uction-eu"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var plainBuf, styledBuf bytes.Buffer
			require.NoError(t, view.WriteHuman(&plainBuf, tt.plan, view.Options{}))
			rec := &recordingStyler{}
			require.NoError(t, view.WriteHuman(&styledBuf, tt.plan, view.Options{Styler: rec}))
			plain := plainBuf.String()
			styled := styledBuf.String()
			assert.Equal(t, plain, styled)
			assert.NotContains(t, plain, "\t")
			recorded := make(map[string]view.Role, len(rec.lines))
			for _, line := range rec.lines {
				assert.NotContains(t, line.line, "\t")
				recorded[line.line] = line.role
			}
			for _, line := range tt.lines {
				assert.Contains(t, "\n"+plain, "\n"+line+"\n")
				got, ok := recorded[line]
				require.Truef(t, ok, "styled lines omit %q", line)
				assert.Equal(t, tt.role, got)
			}
		})
	}
}

func TestWriteHuman_WritesStylerResult(t *testing.T) {
	t.Parallel()
	p := mustPlan(t, semantic.HelmUpgrade, []semantic.ResourceChange{createChangeForHuman()})
	var plainBuf, styledBuf bytes.Buffer
	require.NoError(t, view.WriteHuman(&plainBuf, p, view.Options{}))
	require.NoError(t, view.WriteHuman(&styledBuf, p, view.Options{Styler: markingStyler{}}))
	var want strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(plainBuf.String(), "\n"), "\n") {
		if line == "" {
			want.WriteByte('\n')
			continue
		}
		want.WriteString("<" + line + ">\n")
	}
	assert.Equal(t, want.String(), styledBuf.String())
}

func assertRoleTrace(t *testing.T, golden string, plan semantic.Plan) string {
	t.Helper()
	var plainBuf, styledBuf bytes.Buffer
	require.NoError(t, view.WriteHuman(&plainBuf, plan, view.Options{}))
	rec := &recordingStyler{}
	require.NoError(t, view.WriteHuman(&styledBuf, plan, view.Options{Styler: rec}))
	plain := plainBuf.String()
	assert.Equal(t, plain, styledBuf.String())
	for _, line := range rec.lines {
		assert.NotContains(t, line.line, "\t")
	}
	trace := roleTrace(rec.lines)
	assertGolden(t, golden, trace)
	var want, got []string
	for line := range strings.SplitSeq(strings.TrimRight(plain, "\n"), "\n") {
		if line != "" {
			want = append(want, line)
		}
	}
	for line := range strings.SplitSeq(strings.TrimRight(trace, "\n"), "\n") {
		role, text, ok := strings.Cut(line, "\t")
		require.Truef(t, ok, "role row %q has no tab", line)
		require.NotEmpty(t, role)
		require.NotContains(t, text, "\t")
		got = append(got, text)
	}
	assert.Equal(t, want, got)
	return plain
}

// roleTrace renders one golden row per styled line: the role name, a
// tab, and the Human line. Blank lines are absent because WriteHuman
// does not style them. The text after the tab is the plain Human line.
func roleTrace(lines []styledLine) string {
	var b strings.Builder
	for _, line := range lines {
		var name string
		switch line.role {
		case view.RoleTitle:
			name = "Title"
		case view.RolePrimary:
			name = "Primary"
		case view.RoleDiffAdded:
			name = "DiffAdded"
		case view.RoleDiffRemoved:
			name = "DiffRemoved"
		case view.RoleDiffModified:
			name = "DiffModified"
		case view.RoleDiffContext:
			name = "DiffContext"
		default:
			name = fmt.Sprintf("Role(%d)", int(line.role))
		}
		b.WriteString(name)
		b.WriteByte('\t')
		b.WriteString(line.line)
		b.WriteByte('\n')
	}
	return b.String()
}
