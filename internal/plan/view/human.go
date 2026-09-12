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

package view

import (
	"fmt"
	"io"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/plan/semantic"
)

// yamlDiffContextLines is large enough that small Kubernetes objects stay
// visible around a field change without printing unified-diff headers.
const yamlDiffContextLines = 8

// WriteHuman writes a deterministic YAML-oriented rendering of p. It
// does not mutate p.
func WriteHuman(w io.Writer, p semantic.Plan, opts Options) error {
	prepared, err := prepareRender(p, opts)
	if err != nil {
		return err
	}
	if herr := writeHumanHeader(w, prepared.Header, prepared.Completeness, opts); herr != nil {
		return herr
	}
	for i := range prepared.Changes {
		if cerr := writeHumanChange(w, prepared.Changes[i], opts); cerr != nil {
			return cerr
		}
	}
	if derr := writeHumanDiagnostics(w, prepared.Diagnostics, opts); derr != nil {
		return derr
	}
	if _, werr := fmt.Fprintln(w, style(opts, theme.TextTitle, "Executions: none")); werr != nil {
		return werr
	}
	return writeHumanSummary(w, prepared.Summary, opts)
}

func writeHumanHeader(w io.Writer, h semantic.Header, completeness semantic.Completeness, opts Options) error {
	pairs := [][2]string{
		{"project", h.Project},
		{"environment", h.Environment},
		{"release", h.Release},
		{"namespace", h.Namespace},
		{"context", h.Context},
	}
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		if err := writeln(w, opts, theme.TextPrimary, pair[0]+": "+pair[1]); err != nil {
			return err
		}
	}
	if h.Revision > 0 {
		if err := writeln(w, opts, theme.TextPrimary, fmt.Sprintf("revision: %d", h.Revision)); err != nil {
			return err
		}
	}
	if h.FreshInstall {
		if err := writeln(w, opts, theme.TextPrimary, "fresh_install: true"); err != nil {
			return err
		}
	}
	if err := writeln(w, opts, theme.TextPrimary, "completeness: "+completeness.String()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeHumanChange(w io.Writer, c semantic.ResourceChange, opts Options) error {
	line := c.Resource.String() + " " + c.Action.String() + " " + c.Origin.Kind.String()
	if err := writeln(w, opts, actionHeadingToken(c.Action), line); err != nil {
		return err
	}
	before := humanObject(c.Before)
	after := humanObject(c.After)
	if !opts.ShowSecrets && isCoreSecret(c.Resource) {
		markSecretDiffs(before, after, c.Fields)
	}
	switch c.Action {
	case semantic.Create:
		text, err := marshalOrderedYAML(after)
		if err != nil {
			return err
		}
		return writePrefixedYAML(w, text, "+ ", theme.StatusSuccess, opts)
	case semantic.Delete:
		text, err := marshalOrderedYAML(before)
		if err != nil {
			return err
		}
		return writePrefixedYAML(w, text, "- ", theme.StatusError, opts)
	case semantic.Update, semantic.Recreate:
		if c.After == nil {
			return nil
		}
		beforeYAML, err := marshalOrderedYAML(before)
		if err != nil {
			return err
		}
		afterYAML, err := marshalOrderedYAML(after)
		if err != nil {
			return err
		}
		return writeYAMLDiff(w, beforeYAML, afterYAML, opts)
	default:
		return nil
	}
}

func writePrefixedYAML(w io.Writer, text, prefix string, token theme.Token, opts Options) error {
	for line := range strings.SplitSeq(strings.TrimRight(revealSecretSentinels(text), "\n"), "\n") {
		if err := writeln(w, opts, token, prefix+line); err != nil {
			return err
		}
	}
	return nil
}

func writeYAMLDiff(w io.Writer, beforeYAML, afterYAML string, opts Options) error {
	edits := udiff.Strings(beforeYAML, afterYAML)
	diff, err := udiff.ToUnifiedDiff("before", "after", beforeYAML, edits, yamlDiffContextLines)
	if err != nil {
		return fmt.Errorf("diff yaml: %w", err)
	}
	if len(diff.Hunks) == 0 {
		// Bookkeeping-only updates can leave Before and After YAML identical
		// after strip. Keep the resource shape visible.
		return writePrefixedYAML(w, afterYAML, "  ", theme.TextMuted, opts)
	}
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			content := revealSecretSentinels(strings.TrimRight(line.Content, "\n"))
			prefix, token := "  ", theme.TextMuted
			switch line.Kind {
			case udiff.Delete:
				prefix, token = "- ", theme.StatusError
			case udiff.Insert:
				prefix, token = "+ ", theme.StatusSuccess
			}
			if werr := writeln(w, opts, token, prefix+content); werr != nil {
				return werr
			}
		}
	}
	return nil
}

func writeHumanDiagnostics(w io.Writer, diags []semantic.Diagnostic, opts Options) error {
	if len(diags) == 0 {
		return nil
	}
	if err := writeln(w, opts, theme.TextTitle, "Diagnostics"); err != nil {
		return err
	}
	for _, d := range diags {
		ref := ""
		if d.Resource != nil {
			ref = " " + d.Resource.String()
		}
		line := fmt.Sprintf("  %s %s%s: %s", d.Severity.String(), d.Category.String(), ref, d.Message)
		if err := writeln(w, opts, theme.StatusWarning, line); err != nil {
			return err
		}
	}
	return nil
}

func writeHumanSummary(w io.Writer, s semantic.Summary, opts Options) error {
	if err := writeln(w, opts, theme.TextTitle, "Summary"); err != nil {
		return err
	}
	for _, line := range []string{
		fmt.Sprintf("  create: %d", s.Create),
		fmt.Sprintf("  update: %d", s.Update),
		fmt.Sprintf("  delete: %d", s.Delete),
		fmt.Sprintf("  recreate: %d", s.Recreate),
		fmt.Sprintf("  total: %d", s.Total()),
	} {
		if err := writeln(w, opts, theme.TextPrimary, line); err != nil {
			return err
		}
	}
	return nil
}

func snapshotMap(s *semantic.ResourceSnapshot) map[string]any {
	if s == nil || s.Object == nil {
		return map[string]any{}
	}
	return s.Object
}

func actionHeadingToken(action semantic.Action) theme.Token {
	switch action {
	case semantic.Create:
		return theme.StatusSuccess
	case semantic.Delete:
		return theme.StatusError
	default:
		return theme.StatusWarning
	}
}

func style(opts Options, token theme.Token, s string) string {
	return opts.Theme.Style(token).Render(s)
}

func writeln(w io.Writer, opts Options, token theme.Token, s string) error {
	_, err := fmt.Fprintln(w, style(opts, token, s))
	return err
}
