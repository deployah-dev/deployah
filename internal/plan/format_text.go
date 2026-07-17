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

package plan

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"nabat.dev/theme"
)

// Mode selects how [RenderText] displays field paths and values.
type Mode int

const (
	// ModeCompact maps common Kubernetes paths to Deployah's own spec
	// vocabulary (e.g. "spec.template.spec.containers.web.image" becomes
	// "image"), falling back to the raw dyff path for anything unmapped.
	// This is the default.
	ModeCompact Mode = iota
	// ModeRaw always shows the raw dyff dot-style path, e.g.
	// "spec.template.spec.containers.web.image", bypassing ModeCompact's
	// vocabulary mapping. This is `deployah plan --raw`.
	ModeRaw
	// ModeYAML shows every changed field as a YAML block (path on its own
	// line, old/new values indented underneath) instead of a single
	// flattened "path: old -> new" line, for both scalar and nested
	// map/list values. This is `deployah plan --yaml`.
	ModeYAML
)

// TextOptions controls how [RenderText] formats a [Plan].
type TextOptions struct {
	Mode Mode
	// ShowSecrets reveals the real value of fields [ApplyMasking] flagged.
	// The caller must refuse this on a non-interactive terminal and must
	// never set it together with JSON output; RenderText itself applies it
	// unconditionally once given.
	ShowSecrets bool
	// Theme colors the +/~/- resource lines, header labels, and warning/
	// note text. The zero value renders every style call as the terminal
	// default, so callers that don't set this keep plain, uncolored output.
	Theme theme.ResolvedTheme
}

// actionSymbol is the leading per-resource marker, e.g. "~ Deployment/web",
// "+ ConfigMap/web-config", "- Service/legacy-sidecar".
func actionSymbol(a Action) string {
	switch a {
	case ActionAdd:
		return "+"
	case ActionDestroy:
		return "-"
	default:
		return "~"
	}
}

// actionToken maps a resource [Action] to the semantic status token whose
// color represents it, matching the convention most diff-style CLIs use:
// green for additions, yellow for in-place changes, red for removals.
func actionToken(a Action) theme.Token {
	switch a {
	case ActionAdd:
		return theme.StatusSuccess
	case ActionDestroy:
		return theme.StatusError
	default:
		return theme.StatusWarning
	}
}

// fieldToken is [actionToken]'s field-level counterpart: it colors an
// individual field line by its own [FieldChangeKind], independent of the
// resource's overall Action (e.g. a field added inside an otherwise
// "changed" resource still renders green, not yellow).
func fieldToken(k FieldChangeKind) theme.Token {
	switch k {
	case FieldAdded:
		return theme.StatusSuccess
	case FieldRemoved:
		return theme.StatusError
	default:
		return theme.StatusWarning
	}
}

// RenderText writes a human-readable rendering of p to w: a header block
// describing the target release, one line per changed resource with its
// field-level changes indented underneath, and a trailing summary line.
//
// RenderText calls [ApplyMasking] itself (safe to repeat) so a caller can
// never forget it and leak a secret; opts.ShowSecrets is the only way to
// see a masked value in text output.
func RenderText(w io.Writer, p *Plan, opts TextOptions) error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}
	ApplyMasking(p)

	if err := writeHeader(w, p.Header, opts); err != nil {
		return err
	}

	if len(p.Changes) == 0 {
		if _, err := fmt.Fprintln(w, opts.Theme.Style(theme.StatusSuccess).Render("No changes.")); err != nil {
			return err
		}
		if err := writeHookNote(w, p, opts); err != nil {
			return err
		}
		return writeDrift(w, p, opts)
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	for _, c := range p.Changes {
		if err := writeChange(w, c, opts); err != nil {
			return err
		}
	}

	if err := writeHookNote(w, p, opts); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "\nPlan: %s.\n", p.Summary.String()); err != nil {
		return err
	}

	return writeDrift(w, p, opts)
}

// writeDrift renders the drift section when p.DriftChecked is true (i.e.
// `--drift` was requested and ran), under a "Drift (cluster changed outside
// deployah):" heading with a trailing note. It is a no-op when DriftChecked
// is false, so a plan rendered without --drift never grows this section.
func writeDrift(w io.Writer, p *Plan, opts TextOptions) error {
	// FreshInstall also short-circuits here: checkDrift never sets
	// DriftChecked on a fresh install (no live release to compare against),
	// so this is a defensive second guard for callers that bypass it. Why
	// --drift had no effect is commentary for stderr, not diff content here.
	if !p.DriftChecked || p.Header.FreshInstall {
		return nil
	}

	heading := opts.Theme.Style(theme.TextTitle).Render("Drift (cluster changed outside deployah):")
	if _, err := fmt.Fprintln(w, "\n"+heading); err != nil {
		return err
	}

	for _, c := range p.Drift {
		if err := writeDriftChange(w, c, opts); err != nil {
			return err
		}
	}
	if len(p.Drift) == 0 {
		if _, err := fmt.Fprintln(w, opts.Theme.Style(theme.StatusSuccess).Render("No drift detected.")); err != nil {
			return err
		}
	}

	if len(p.DriftIncomplete) > 0 {
		warning := opts.Theme.Style(theme.StatusWarning).Render("Warning: drift is incomplete; could not check:")
		if _, err := fmt.Fprintln(w, "\n"+warning); err != nil {
			return err
		}
		for _, reason := range p.DriftIncomplete {
			if _, err := fmt.Fprintf(w, "  - %s\n", reason); err != nil {
				return err
			}
		}
	}

	note := opts.Theme.Style(theme.TextMuted).Render("Note: deploy does not revert drift. Drifted fields keep their live values\nunless the spec changes them.")
	_, err := fmt.Fprintln(w, "\n"+note)
	return err
}

// writeDriftChange is [writeChange]'s drift counterpart: it uses "expected
// X, live Y" wording instead of "X -> Y" so a drift line is never confused
// with an ordinary spec-edit change.
func writeDriftChange(w io.Writer, c Change, opts TextOptions) error {
	line := fmt.Sprintf("%s %s/%s", actionSymbol(c.Action), c.Kind, c.Name)
	if _, err := fmt.Fprintln(w, opts.Theme.Style(actionToken(c.Action)).Render(line)); err != nil {
		return err
	}
	if opts.Mode == ModeYAML {
		return writeYAMLTree(w, c.Fields, opts, yamlLeafStyleDrift)
	}
	for _, f := range c.Fields {
		if err := writeDriftField(w, f, opts); err != nil {
			return err
		}
	}
	return nil
}

// writeDriftField renders one drift field. Old is the predicted (expected)
// value and New is the resource's live value; see [driftOnlyChange] in
// deployah.dev/deployah/internal/drift. Every drift line styles as
// [theme.StatusWarning] regardless of FieldChangeKind: unlike a spec-edit
// change, any drift is the same kind of surprise, so it skips the 3-way
// add/change/remove color split [writeField] uses.
func writeDriftField(w io.Writer, f FieldDiff, opts TextOptions) error {
	path := f.Path
	if opts.Mode != ModeRaw {
		if mapped, ok := mapCompactPath(f.Path); ok {
			path = mapped
		}
	}

	if f.Masked && !opts.ShowSecrets {
		return writeMaskedField(w, path, f.ChangeKind, opts)
	}

	style := opts.Theme.Style(theme.StatusWarning)
	switch f.ChangeKind {
	case FieldAdded:
		_, err := fmt.Fprintln(w, style.Render(fmt.Sprintf("    %s: (only on cluster) %s", path, f.New)))
		return err
	case FieldRemoved:
		_, err := fmt.Fprintln(w, style.Render(fmt.Sprintf("    %s: (missing on cluster, expected %s)", path, f.Old)))
		return err
	default:
		_, err := fmt.Fprintln(w, style.Render(fmt.Sprintf("    %s: expected %s, live %s", path, f.Old, f.New)))
		return err
	}
}

func writeHeader(w io.Writer, h Header, opts TextOptions) error {
	type line struct {
		label, value string
	}
	var lines []line
	if h.Project != "" {
		lines = append(lines, line{"Project", h.Project})
	}
	if h.Environment != "" {
		lines = append(lines, line{"Environment", h.Environment})
	}
	if h.Release != "" {
		release := h.Release
		switch {
		case h.FreshInstall:
			release += " (fresh install)"
		case h.Revision > 0:
			release += fmt.Sprintf(" (revision %d)", h.Revision)
		}
		lines = append(lines, line{"Release", release})
	}
	if h.Namespace != "" {
		lines = append(lines, line{"Namespace", h.Namespace})
	}
	if h.Context != "" {
		lines = append(lines, line{"Context", h.Context})
	}

	labelStyle := opts.Theme.Style(theme.AccentPrimary)
	for _, l := range lines {
		// Pad the plain label before styling it: ANSI escape codes must not
		// count toward the %-12s width, or the values stop lining up.
		padded := fmt.Sprintf("%-12s", l.label+":")
		if _, err := fmt.Fprintf(w, "%s %s\n", labelStyle.Render(padded), l.value); err != nil {
			return err
		}
	}

	if h.Warning != "" {
		warning := opts.Theme.Style(theme.StatusWarning).Render("Warning: " + h.Warning)
		if _, err := fmt.Fprintf(w, "\n%s\n", warning); err != nil {
			return err
		}
	}

	return nil
}

func writeChange(w io.Writer, c Change, opts TextOptions) error {
	line := fmt.Sprintf("%s %s/%s", actionSymbol(c.Action), c.Kind, c.Name)
	if _, err := fmt.Fprintln(w, opts.Theme.Style(actionToken(c.Action)).Render(line)); err != nil {
		return err
	}
	if opts.Mode == ModeYAML {
		return writeYAMLTree(w, c.Fields, opts, yamlLeafStyleChange)
	}
	for _, f := range c.Fields {
		if err := writeField(w, f, opts); err != nil {
			return err
		}
	}
	return nil
}

func writeField(w io.Writer, f FieldDiff, opts TextOptions) error {
	path := f.Path
	if opts.Mode != ModeRaw {
		if mapped, ok := mapCompactPath(f.Path); ok {
			path = mapped
		}
	}

	if f.Masked && !opts.ShowSecrets {
		return writeMaskedField(w, path, f.ChangeKind, opts)
	}

	style := opts.Theme.Style(fieldToken(f.ChangeKind))
	switch f.ChangeKind {
	case FieldAdded:
		_, err := fmt.Fprintln(w, style.Render(fmt.Sprintf("    %s: (added) %s", path, f.New)))
		return err
	case FieldRemoved:
		_, err := fmt.Fprintln(w, style.Render(fmt.Sprintf("    %s: (removed) %s", path, f.Old)))
		return err
	default:
		_, err := fmt.Fprintln(w, style.Render(fmt.Sprintf("    %s: %s -> %s", path, f.Old, f.New)))
		return err
	}
}

// writeMaskedField prints a masked field's change kind without exposing
// its value, e.g. "(masked) changed" / "(masked) added" / "(masked)
// removed". Styled muted rather than by change kind: the value is hidden,
// so there's nothing to draw the eye to the way a real added/changed/
// removed value would.
func writeMaskedField(w io.Writer, path string, kind FieldChangeKind, opts TextOptions) error {
	line := fmt.Sprintf("    %s: (masked) %s", path, kind)
	_, err := fmt.Fprintln(w, opts.Theme.Style(theme.TextMuted).Render(line))
	return err
}

// indentBlock prefixes the first line of value with prefix and every
// subsequent line with matching whitespace, so multi-line YAML values stay
// legible under a field path.
func indentBlock(value, prefix string) string {
	pad := strings.Repeat(" ", len(prefix))
	lines := strings.Split(value, "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = prefix + l
		} else {
			lines[i] = pad + l
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " ")
}

func writeHookNote(w io.Writer, p *Plan, opts TextOptions) error {
	if !p.HooksChanged {
		return nil
	}
	note := opts.Theme.Style(theme.TextMuted).Render("Note: Helm hooks changed for this release (not shown above).")
	_, err := fmt.Fprintln(w, "\n"+note)
	return err
}

// String renders the summary trailer, e.g.
// "1 to add, 1 to change, 1 to destroy".
func (s Summary) String() string {
	return fmt.Sprintf("%s to add, %s to change, %s to destroy",
		strconv.Itoa(s.Add), strconv.Itoa(s.Change), strconv.Itoa(s.Destroy))
}

// compactPathMappings maps common Kubernetes field paths, in dyff's
// dot-style notation, to Deployah's own spec vocabulary.
var compactPathMappings = []struct {
	pattern *regexp.Regexp
	display string // may reference regexp capture groups, e.g. "$1"
}{
	{regexp.MustCompile(`^spec\.replicas$`), "replicas"},
	{regexp.MustCompile(`^spec\.template\.spec\.containers\.[^.]+\.image$`), "image"},
	{regexp.MustCompile(`^spec\.template\.spec\.containers\.[^.]+\.ports\.[^.]+\.containerPort$`), "port"},
	{regexp.MustCompile(`^spec\.template\.spec\.containers\.[^.]+\.env\.([^.]+)\.value$`), "env.$1"},
}

// mapCompactPath maps a raw dyff path to Deployah's compact vocabulary. It
// returns ok=false for any path with no mapping, so the caller can fall
// back to the raw path unchanged.
func mapCompactPath(path string) (display string, ok bool) {
	for _, m := range compactPathMappings {
		if m.pattern.MatchString(path) {
			return m.pattern.ReplaceAllString(path, m.display), true
		}
	}
	return "", false
}
