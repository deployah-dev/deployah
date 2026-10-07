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

func TestWriteHuman_NoChartCRDsHasNoSection(t *testing.T) {
	t.Parallel()
	text := writeHuman(t, mustPlanWithHeader(t, humanHeader(), semantic.HelmNone, nil, nil))
	assert.NotContains(t, text, "Chart CRDs")
}

func TestWriteHuman_ChartCRDLifecycle(t *testing.T) {
	t.Parallel()
	fresh := humanHeader()
	fresh.FreshInstall = true
	upgrade := humanHeader()
	tests := []struct {
		name   string
		header semantic.Header
		action semantic.HelmAction
		lc     semantic.ChartCRDLifecycle
		golden string
	}{
		{
			name:   "process",
			header: fresh,
			action: semantic.HelmInstall,
			lc:     semantic.ChartCRDProcess,
			golden: "human_chart_crds_process",
		},
		{
			name:   "skip",
			header: fresh,
			action: semantic.HelmInstall,
			lc:     semantic.ChartCRDSkip,
			golden: "human_chart_crds_skip",
		},
		{
			name:   "upgrade",
			header: upgrade,
			action: semantic.HelmUpgrade,
			lc:     semantic.ChartCRDUpgrade,
			golden: "human_chart_crds_upgrade",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := planWithChartCRDs(t, tt.header, tt.action, []semantic.ChartCRD{
				chartCRD(".deployah/crds/widget.yaml", "widgets.example.com", 0, tt.lc),
			})
			text := writeHuman(t, p)
			assertGolden(t, tt.golden, text)
			assertChartCRDSectionHasNoDiff(t, chartCRDSection(t, text))
		})
	}
}

func TestWriteHuman_ChartCRDSourceLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		crds     []semantic.ChartCRD
		contains []string
		absent   []string
	}{
		{
			name: "empty source",
			crds: []semantic.ChartCRD{
				chartCRD("", "widgets.example.com", 0, semantic.ChartCRDUpgrade),
			},
			contains: []string{"lifecycle: upgrade (Helm upgrade does not process chart CRDs)"},
			absent:   []string{"source:"},
		},
		{
			name: "one document",
			crds: []semantic.ChartCRD{
				chartCRD(".deployah/crds/widget.yaml", "widgets.example.com", 0, semantic.ChartCRDUpgrade),
			},
			contains: []string{".deployah/crds/widget.yaml"},
			absent:   []string{"(index "},
		},
		{
			name: "shared source",
			crds: []semantic.ChartCRD{
				chartCRD(".deployah/crds/bundle.yaml", "zeta.example.com", 0, semantic.ChartCRDUpgrade),
				chartCRD(".deployah/crds/bundle.yaml", "alpha.example.com", 1, semantic.ChartCRDUpgrade),
				chartCRD(".deployah/crds/a.yaml", "mu.example.com", 0, semantic.ChartCRDUpgrade),
			},
			contains: []string{
				".deployah/crds/bundle.yaml (index 0)",
				".deployah/crds/bundle.yaml (index 1)",
				".deployah/crds/a.yaml",
			},
			absent: []string{"a.yaml (index"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			section := chartCRDSection(t, writeHuman(t, planWithChartCRDs(t, humanHeader(), semantic.HelmUpgrade, tt.crds)))
			for _, s := range tt.contains {
				assert.Contains(t, section, s)
			}
			for _, s := range tt.absent {
				assert.NotContains(t, section, s)
			}
		})
	}
}

func TestWriteHuman_ChartCRDOrder(t *testing.T) {
	t.Parallel()
	p := planWithChartCRDs(t, humanHeader(), semantic.HelmUpgrade, []semantic.ChartCRD{
		chartCRD(".deployah/crds/bundle.yaml", "zeta.example.com", 0, semantic.ChartCRDUpgrade),
		chartCRD(".deployah/crds/bundle.yaml", "alpha.example.com", 1, semantic.ChartCRDUpgrade),
		chartCRD(".deployah/crds/a.yaml", "mu.example.com", 0, semantic.ChartCRDUpgrade),
	})
	text := writeHuman(t, p)
	assertGolden(t, "human_chart_crds_multiple", text)
	assert.Equal(t, text, writeHuman(t, p))
	assertTextOrder(t, text, "zeta.example.com", "alpha.example.com", "mu.example.com")
}

func TestWriteHuman_ChartCRDPlacement(t *testing.T) {
	t.Parallel()
	p := mustPlanInput(t, semantic.Input{
		Header:     humanHeader(),
		HelmAction: semantic.HelmUpgrade,
		Changes: []semantic.ResourceChange{{
			Resource: ref("ConfigMap", "app"),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
		}},
		Tasks: []semantic.TaskPlan{{
			Name:    "migrate",
			Phase:   semantic.TaskPreDeploy,
			Action:  semantic.TaskUnchanged,
			WillRun: true,
		}},
		ChartCRDs: []semantic.ChartCRD{
			chartCRD(".deployah/crds/widget.yaml", "widgets.example.com", 0, semantic.ChartCRDUpgrade),
		},
		Drift: []semantic.DriftChange{{
			Resource: ref("ConfigMap", "extra"),
			Action:   semantic.DriftUnexpected,
			Live:     snap(cm("extra", "live")),
		}},
	})

	text := writeHuman(t, p)
	assertHumanLayout(t, text)
	assertTextOrder(t, text,
		"\nResources\n",
		"\nChart CRDs\n",
		"\nTasks\n",
		"\nDrift\n",
		"\nSummary\n",
	)
}

func TestWriteHuman_ChartCRDSectionUsesNoDiffRole(t *testing.T) {
	t.Parallel()
	fresh := humanHeader()
	fresh.FreshInstall = true
	upgrade := humanHeader()
	tests := []struct {
		name   string
		header semantic.Header
		action semantic.HelmAction
		lc     semantic.ChartCRDLifecycle
	}{
		{name: "process", header: fresh, action: semantic.HelmInstall, lc: semantic.ChartCRDProcess},
		{name: "skip", header: fresh, action: semantic.HelmInstall, lc: semantic.ChartCRDSkip},
		{name: "upgrade", header: upgrade, action: semantic.HelmUpgrade, lc: semantic.ChartCRDUpgrade},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := planWithChartCRDs(t, tt.header, tt.action, []semantic.ChartCRD{
				chartCRD(".deployah/crds/widget.yaml", "widgets.example.com", 0, tt.lc),
			})
			assertChartCRDSectionRoles(t, p)
		})
	}
}

func TestWriteHuman_ChartCRDWriteError(t *testing.T) {
	t.Parallel()
	p := mustPlanInput(t, semantic.Input{
		Header:     humanHeader(),
		HelmAction: semantic.HelmUpgrade,
		Changes: []semantic.ResourceChange{{
			Resource: ref("ConfigMap", "app"),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
		}},
		ChartCRDs: []semantic.ChartCRD{
			chartCRD(".deployah/crds/widget.yaml", "widgets.example.com", 0, semantic.ChartCRDUpgrade),
			chartCRD(".deployah/crds/widget.yaml", "gadgets.example.com", 1, semantic.ChartCRDUpgrade),
		},
	})

	var plain captureWriter
	require.NoError(t, view.WriteHuman(&plain, p, view.Options{}))
	from, to := chartCRDWriteSpan(t, plain.chunks)

	tests := make([]struct {
		name string
		at   int
	}, 0, to-from+1)
	for at := from; at <= to; at++ {
		label := strings.TrimSpace(plain.chunks[at])
		if label == "" {
			label = "blank"
		}
		tests = append(tests, struct {
			name string
			at   int
		}{name: fmt.Sprintf("%d %s", at, label), at: at})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			writeErr := view.WriteHuman(&failAtWriter{at: tt.at}, p, view.Options{})
			require.Error(t, writeErr)
			assert.ErrorContains(t, writeErr, "write failed")
		})
	}
}

// chartCRDWriteSpan returns the write indexes for the blank line before
// Chart CRDs through the last lifecycle line. Failing each of those
// writes checks that a section write error reaches WriteHuman.
func chartCRDWriteSpan(t *testing.T, chunks []string) (from, to int) {
	t.Helper()
	title, last := -1, -1
	for i, chunk := range chunks {
		if strings.Contains(chunk, "Chart CRDs") {
			title = i
		}
		if title >= 0 && strings.Contains(chunk, "lifecycle:") {
			last = i
		}
	}
	require.GreaterOrEqual(t, title, 1, "Chart CRDs write missing")
	require.GreaterOrEqual(t, last, title, "lifecycle write missing")
	require.Equal(t, "\n", chunks[title-1], "write before Chart CRDs should be the separating blank line")
	return title - 1, last
}

type captureWriter struct {
	chunks []string
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.chunks = append(w.chunks, string(p))
	return len(p), nil
}

// failAtWriter accepts writes until index at, then fails.
type failAtWriter struct {
	at int
	n  int
}

func (w *failAtWriter) Write(p []byte) (int, error) {
	if w.n == w.at {
		return 0, errors.New("write failed")
	}
	w.n++
	return len(p), nil
}

func chartCRD(source, name string, index int, lc semantic.ChartCRDLifecycle) semantic.ChartCRD {
	return semantic.ChartCRD{
		Source:      source,
		Index:       index,
		Kind:        "CustomResourceDefinition",
		Name:        name,
		Lifecycle:   lc,
		WillProcess: lc == semantic.ChartCRDProcess,
	}
}

func planWithChartCRDs(tb testing.TB, header semantic.Header, action semantic.HelmAction, crds []semantic.ChartCRD) semantic.Plan {
	tb.Helper()
	return mustPlanInput(tb, semantic.Input{
		Header:     header,
		HelmAction: action,
		ChartCRDs:  crds,
	})
}

// chartCRDSection returns the Chart CRDs block, from its title through
// the last entry line. It stops before the blank line that opens the
// next column-0 section.
func chartCRDSection(t *testing.T, text string) string {
	t.Helper()
	const title = "Chart CRDs\n"
	start := strings.Index(text, title)
	require.GreaterOrEqual(t, start, 0, "Chart CRDs section missing")
	rest := text[start+len(title):]
	end := len(rest)
	for _, next := range []string{"\n\nTasks\n", "\n\nDrift\n", "\n\nSummary\n"} {
		if i := strings.Index(rest, next); i >= 0 && i < end {
			end = i
		}
	}
	return text[start : start+len(title)+end]
}

func assertTextOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	prev := -1
	for _, part := range parts {
		idx := strings.Index(text, part)
		require.Greaterf(t, idx, prev, "%q", part)
		prev = idx
	}
}

func assertChartCRDSectionRoles(t *testing.T, p semantic.Plan) {
	t.Helper()
	var buf bytes.Buffer
	rec := &recordingStyler{}
	require.NoError(t, view.WriteHuman(&buf, p, view.Options{Styler: rec}))

	inSection := false
	sawSection := false
	for _, line := range rec.lines {
		if line.line == "Chart CRDs" {
			inSection = true
			sawSection = true
			assert.Equal(t, view.RoleTitle, line.role)
			continue
		}
		if !inSection {
			continue
		}
		if line.role == view.RoleTitle {
			break
		}
		assert.Equal(t, view.RolePrimary, line.role, "line %q", line.line)
	}
	assert.True(t, sawSection)
}

func assertChartCRDSectionHasNoDiff(t *testing.T, section string) {
	t.Helper()
	for _, word := range []string{"create", "update", "delete", "apply"} {
		assert.NotContains(t, section, word)
	}
	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, marker := range []string{"+ ", "~ ", "- "} {
			assert.Falsef(t, strings.HasPrefix(trimmed, marker), "diff marker on %q", line)
		}
	}
}
