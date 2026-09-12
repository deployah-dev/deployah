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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"deployah.dev/deployah/internal/plan/semantic"
)

// WriteHuman writes a deterministic structural rendering of p. It does
// not mutate p.
func WriteHuman(w io.Writer, p semantic.Plan, opts Options) error {
	prepared, err := prepareRender(p, opts)
	if err != nil {
		return err
	}
	if herr := writeHumanHeader(w, prepared.Header, prepared.Completeness); herr != nil {
		return herr
	}
	for i := range prepared.Changes {
		if cerr := writeHumanChange(w, prepared.Changes[i]); cerr != nil {
			return cerr
		}
	}
	if derr := writeHumanDiagnostics(w, prepared.Diagnostics); derr != nil {
		return derr
	}
	if _, werr := fmt.Fprintln(w, "Executions: none"); werr != nil {
		return werr
	}
	return writeHumanSummary(w, prepared.Summary)
}

func writeHumanHeader(w io.Writer, h semantic.Header, completeness semantic.Completeness) error {
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
		if _, err := fmt.Fprintf(w, "%s: %s\n", pair[0], pair[1]); err != nil {
			return err
		}
	}
	if h.Revision > 0 {
		if _, err := fmt.Fprintf(w, "revision: %d\n", h.Revision); err != nil {
			return err
		}
	}
	if h.FreshInstall {
		if _, err := fmt.Fprintln(w, "fresh_install: true"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "completeness: %s\n", completeness.String()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeHumanChange(w io.Writer, c semantic.ResourceChange) error {
	line := c.Resource.String() + " " + c.Action.String() + " " + c.Origin.Kind.String()
	if apply := humanApply(c.Apply); apply != "" {
		line += " " + apply
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}
	switch c.Action {
	case semantic.Update, semantic.Recreate:
		for _, f := range c.Fields {
			if err := writeHumanField(w, f); err != nil {
				return err
			}
		}
	case semantic.Create:
		if _, err := fmt.Fprintln(w, "  after:"); err != nil {
			return err
		}
		return writeIndentedJSON(w, snapshotMap(c.After))
	case semantic.Delete:
		if _, err := fmt.Fprintln(w, "  before:"); err != nil {
			return err
		}
		return writeIndentedJSON(w, snapshotMap(c.Before))
	}
	return nil
}

func humanApply(a semantic.ApplySemantics) string {
	var parts []string
	if a.Write != nil {
		parts = append(parts, fmt.Sprintf("write=%s field_manager=%s force_conflicts=%t",
			a.Write.Method.String(), a.Write.FieldManager, a.Write.ForceConflicts))
	}
	if a.Delete != nil {
		parts = append(parts, "delete="+a.Delete.Propagation.String())
	}
	return strings.Join(parts, " ")
}

func writeHumanField(w io.Writer, f semantic.FieldChange) error {
	switch f.Op {
	case semantic.FieldAdd:
		_, err := fmt.Fprintf(w, "  %s: (added) %s\n", f.Path, formatValue(f.After))
		return err
	case semantic.FieldRemove:
		_, err := fmt.Fprintf(w, "  %s: (removed) %s\n", f.Path, formatValue(f.Before))
		return err
	default:
		_, err := fmt.Fprintf(w, "  %s: %s -> %s\n", f.Path, formatValue(f.Before), formatValue(f.After))
		return err
	}
}

func formatValue(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return fmt.Sprintf("%t", x)
	case json.Number:
		return x.String()
	case int, int32, int64, float32, float64:
		return fmt.Sprint(x)
	default:
		s, err := encodeJSON(x, "")
		if err != nil {
			return fmt.Sprint(x)
		}
		return s
	}
}

func writeHumanDiagnostics(w io.Writer, diags []semantic.Diagnostic) error {
	if len(diags) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Diagnostics"); err != nil {
		return err
	}
	for _, d := range diags {
		ref := ""
		if d.Resource != nil {
			ref = " " + d.Resource.String()
		}
		if _, err := fmt.Fprintf(w, "  %s %s%s: %s\n",
			d.Severity.String(), d.Category.String(), ref, d.Message); err != nil {
			return err
		}
	}
	return nil
}

func writeHumanSummary(w io.Writer, s semantic.Summary) error {
	_, err := fmt.Fprintf(w, "Summary\n  create: %d\n  update: %d\n  delete: %d\n  recreate: %d\n  total: %d\n",
		s.Create, s.Update, s.Delete, s.Recreate, s.Total())
	return err
}

func snapshotMap(s *semantic.ResourceSnapshot) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	if s.Object == nil {
		return map[string]any{}
	}
	return s.Object
}

func writeIndentedJSON(w io.Writer, obj map[string]any) error {
	text, err := encodeJSON(obj, "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	for line := range strings.SplitSeq(text, "\n") {
		if _, werr := fmt.Fprintf(w, "  %s\n", line); werr != nil {
			return werr
		}
	}
	return nil
}

func encodeJSON(v any, indent string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}
