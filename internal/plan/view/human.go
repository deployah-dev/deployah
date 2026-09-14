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
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/plan/semantic"
)

// yamlDiffContextLines is small because Update and Replace diffs are
// ancestor-projected, not whole-object dumps.
const yamlDiffContextLines = 3

const headerLabelWidth = 10

// WriteHuman writes a deterministic YAML-oriented rendering of p. It
// does not mutate p. Resource headings use +, ~, -, and -/+ markers.
func WriteHuman(w io.Writer, p semantic.Plan, opts Options) error {
	prepared, err := prepareRender(p, opts)
	if err != nil {
		return err
	}
	if herr := writeHumanHeader(w, prepared.Header, opts); herr != nil {
		return herr
	}
	if prepared.Completeness == semantic.CompletenessPartial {
		if werr := writeln(w, opts, theme.StatusWarning, "Warning: prediction is incomplete"); werr != nil {
			return werr
		}
		if _, ferr := fmt.Fprintln(w); ferr != nil {
			return ferr
		}
	}
	owned := ownedResourceKeys(prepared.Tasks)
	indexed := indexChanges(prepared.Changes)
	if rerr := writeHumanResources(w, prepared.Changes, owned, prepared.Header.Namespace, opts); rerr != nil {
		return rerr
	}
	if terr := writeHumanTasks(w, prepared, indexed, opts); terr != nil {
		return terr
	}
	if derr := writeHumanDiagnostics(w, prepared.Diagnostics, prepared.Header.Namespace, opts); derr != nil {
		return derr
	}
	return writeHumanFooter(w, prepared, opts)
}

func writeHumanHeader(w io.Writer, h semantic.Header, opts Options) error {
	if h.Project != "" || h.Environment != "" {
		line := fmt.Sprintf("Plan for project %q on environment %q", h.Project, h.Environment)
		if err := writeln(w, opts, theme.TextTitle, line); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	type pair struct {
		label, value string
	}
	pairs := []pair{
		{"Context:", h.Context},
		{"Namespace:", h.Namespace},
		{"Release:", h.Release},
	}
	for _, p := range pairs {
		if p.value == "" {
			continue
		}
		if err := writeln(w, opts, theme.TextPrimary, padLabel(p.label)+" "+p.value); err != nil {
			return err
		}
	}
	if h.Revision > 0 {
		if err := writeln(w, opts, theme.TextPrimary, padLabel("Revision:")+" "+strconv.Itoa(h.Revision)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func padLabel(label string) string {
	if len(label) >= headerLabelWidth {
		return label
	}
	return label + strings.Repeat(" ", headerLabelWidth-len(label))
}

func writeHumanResources(w io.Writer, changes []semantic.ResourceChange, owned map[string]struct{}, planNS string, opts Options) error {
	var visible []semantic.ResourceChange
	for _, c := range changes {
		if _, ok := owned[refKey(c.Resource)]; ok {
			continue
		}
		visible = append(visible, c)
	}
	if len(visible) == 0 {
		return nil
	}
	if err := writeln(w, opts, theme.TextTitle, "Resources"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for i := range visible {
		if err := writeHumanChange(w, visible[i], planNS, "  ", opts); err != nil {
			return err
		}
	}
	return nil
}

func writeHumanTasks(w io.Writer, p semantic.Plan, indexed map[string]semantic.ResourceChange, opts Options) error {
	if len(p.Tasks) == 0 {
		return nil
	}
	if err := writeln(w, opts, theme.TextTitle, "Tasks"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	var current semantic.TaskPhase
	for i := range p.Tasks {
		t := p.Tasks[i]
		if t.Phase != current {
			current = t.Phase
			if err := writeln(w, opts, theme.TextTitle, "  "+t.Phase.String()); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := writeHumanTask(w, t, indexed, p.Header.Namespace, opts); err != nil {
			return err
		}
	}
	return nil
}

func writeHumanTask(w io.Writer, t semantic.TaskPlan, indexed map[string]semantic.ResourceChange, planNS string, opts Options) error {
	if err := writeln(w, opts, taskHeadingToken(t), "    "+taskTitle(t)); err != nil {
		return err
	}
	for i := range t.Definitions {
		if err := writeHumanDefinition(w, t.Definitions[i], planNS, opts); err != nil {
			return err
		}
	}
	for _, ref := range t.Resources {
		c, ok := indexed[refKey(ref)]
		if !ok {
			return fmt.Errorf("task %s: missing resource %s", t.Name, ref)
		}
		if err := writeHumanChange(w, c, planNS, "      ", opts); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func taskTitle(t semantic.TaskPlan) string {
	switch t.Action {
	case semantic.TaskCreate:
		s := "+ " + t.Name + "  new"
		if t.WillRun {
			s += ", will run"
		}
		return s
	case semantic.TaskUpdate:
		s := "~ " + t.Name + "  changed"
		if t.WillRun {
			s += ", will run"
		}
		return s
	case semantic.TaskDelete:
		return "- " + t.Name + "  removed"
	case semantic.TaskUnchanged:
		if t.WillRun {
			return t.Name + "  will run"
		}
		return t.Name
	default:
		return t.Name
	}
}

func writeHumanDefinition(w io.Writer, d semantic.HookDefinition, planNS string, opts Options) error {
	line := resourceHeading(d.Action, d.Resource, planNS)
	if err := writeln(w, opts, actionHeadingToken(d.Action), "      "+line); err != nil {
		return err
	}
	return writeActionBody(w, d.Action, d.Before, d.After, d.Fields, d.Resource, "      ", opts)
}

func writeHumanChange(w io.Writer, c semantic.ResourceChange, planNS, indent string, opts Options) error {
	line := resourceHeading(c.Action, c.Resource, planNS)
	if err := writeln(w, opts, actionHeadingToken(c.Action), indent+line); err != nil {
		return err
	}
	return writeActionBody(w, c.Action, c.Before, c.After, c.Fields, c.Resource, indent, opts)
}

func writeActionBody(
	w io.Writer,
	action semantic.Action,
	before, after *semantic.ResourceSnapshot,
	fields []semantic.FieldChange,
	ref semantic.ResourceRef,
	indent string,
	opts Options,
) error {
	beforeObj := humanObject(before)
	afterObj := humanObject(after)
	renderFields := fields
	if !opts.ShowSecrets && isCoreSecret(ref) {
		markSecretDiffs(beforeObj, afterObj, fields)
		renderFields = secretSentinelFields(fields)
	}
	switch action {
	case semantic.Create:
		text, err := marshalOrderedYAML(afterObj)
		if err != nil {
			return err
		}
		return writePrefixedYAML(w, text, indent+"+ ", theme.StatusSuccess, opts)
	case semantic.Delete:
		text, err := marshalOrderedYAML(beforeObj)
		if err != nil {
			return err
		}
		return writePrefixedYAML(w, text, indent+"- ", theme.StatusError, opts)
	case semantic.Update:
		if after == nil {
			return nil
		}
		beforeProj, afterProj := projectFields(beforeObj, afterObj, renderFields)
		return writeProjectedDiff(w, beforeProj, afterProj, indent, opts)
	case semantic.Replace:
		if err := writeReplaceIdentity(w, afterObj, indent, opts); err != nil {
			return err
		}
		beforeProj, afterProj := projectFields(beforeObj, afterObj, restFields(renderFields))
		return writeProjectedDiff(w, beforeProj, afterProj, indent, opts)
	default:
		return nil
	}
}

func restFields(fields []semantic.FieldChange) []semantic.FieldChange {
	out := make([]semantic.FieldChange, 0, len(fields))
	for _, f := range fields {
		if f.Path == "/apiVersion" || f.Path == "/kind" || f.Path == "/metadata" || strings.HasPrefix(f.Path, "/metadata/") {
			continue
		}
		out = append(out, f)
	}
	return out
}

func writeReplaceIdentity(w io.Writer, after map[string]any, indent string, opts Options) error {
	id := map[string]any{}
	if v, ok := after["apiVersion"]; ok {
		id["apiVersion"] = v
	}
	if v, ok := after["kind"]; ok {
		id["kind"] = v
	}
	if v, ok := after["metadata"]; ok {
		id["metadata"] = v
	}
	text, err := marshalOrderedYAML(id)
	if err != nil {
		return err
	}
	return writePrefixedYAML(w, text, indent+"  ", theme.TextMuted, opts)
}

func writeProjectedDiff(w io.Writer, before, after map[string]any, indent string, opts Options) error {
	beforeYAML, err := marshalOrderedYAML(before)
	if err != nil {
		return err
	}
	afterYAML, err := marshalOrderedYAML(after)
	if err != nil {
		return err
	}
	if strings.TrimSpace(beforeYAML) == "{}" {
		beforeYAML = ""
	}
	if strings.TrimSpace(afterYAML) == "{}" {
		afterYAML = ""
	}
	return writeYAMLDiff(w, beforeYAML, afterYAML, indent, opts)
}

func writePrefixedYAML(w io.Writer, text, prefix string, token theme.Token, opts Options) error {
	for line := range strings.SplitSeq(strings.TrimRight(revealSecretSentinels(text), "\n"), "\n") {
		if err := writeln(w, opts, token, prefix+line); err != nil {
			return err
		}
	}
	return nil
}

func writeYAMLDiff(w io.Writer, beforeYAML, afterYAML, indent string, opts Options) error {
	edits := udiff.Strings(beforeYAML, afterYAML)
	diff, err := udiff.ToUnifiedDiff("before", "after", beforeYAML, edits, yamlDiffContextLines)
	if err != nil {
		return fmt.Errorf("diff yaml: %w", err)
	}
	if len(diff.Hunks) == 0 {
		return writePrefixedYAML(w, afterYAML, indent+"  ", theme.TextMuted, opts)
	}
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			content := revealSecretSentinels(strings.TrimRight(line.Content, "\n"))
			prefix, token := indent+"  ", theme.TextMuted
			switch line.Kind {
			case udiff.Delete:
				prefix, token = indent+"- ", theme.StatusError
			case udiff.Insert:
				prefix, token = indent+"+ ", theme.StatusSuccess
			}
			if werr := writeln(w, opts, token, prefix+content); werr != nil {
				return werr
			}
		}
	}
	return nil
}

func writeHumanDiagnostics(w io.Writer, diags []semantic.Diagnostic, planNS string, opts Options) error {
	if len(diags) == 0 {
		return nil
	}
	if err := writeln(w, opts, theme.TextTitle, "Diagnostics"); err != nil {
		return err
	}
	for _, d := range diags {
		ref := ""
		if d.Resource != nil {
			ref = " " + formatGVK(*d.Resource) + " " + quotedName(*d.Resource)
			if d.Resource.Namespace != "" && d.Resource.Namespace != planNS {
				ref += fmt.Sprintf(" in namespace %q", d.Resource.Namespace)
			}
		}
		line := fmt.Sprintf("  %s %s%s: %s", d.Severity.String(), d.Category.String(), ref, d.Message)
		if err := writeln(w, opts, theme.StatusWarning, line); err != nil {
			return err
		}
	}
	return nil
}

func writeHumanFooter(w io.Writer, p semantic.Plan, opts Options) error {
	s := p.Summary
	planLine := fmt.Sprintf("Plan: %d create, %d update, %d delete, %d replace", s.Create, s.Update, s.Delete, s.Replace)
	if err := writeln(w, opts, theme.TextPrimary, planLine); err != nil {
		return err
	}
	if len(p.Tasks) == 0 {
		return nil
	}
	toRun := 0
	schedChanged := 0
	for _, t := range p.Tasks {
		if t.WillRun {
			toRun++
		}
		if t.Phase == semantic.TaskSchedule && t.Action != semantic.TaskUnchanged {
			schedChanged++
		}
	}
	taskLine := fmt.Sprintf("Tasks: %d to run", toRun)
	if schedChanged > 0 {
		taskLine += fmt.Sprintf(", %d schedule changed", schedChanged)
	}
	return writeln(w, opts, theme.TextPrimary, taskLine)
}

func resourceHeading(action semantic.Action, ref semantic.ResourceRef, planNS string) string {
	line := actionMarker(action) + " " + action.String() + " " + formatGVK(ref) + " " + quotedName(ref)
	if ref.Namespace != "" && ref.Namespace != planNS {
		line += fmt.Sprintf(" in namespace %q", ref.Namespace)
	}
	return line
}

func quotedName(ref semantic.ResourceRef) string {
	name := ref.Name
	if name == "" {
		name = ref.GenerateName
	}
	return strconv.Quote(name)
}

func formatGVK(ref semantic.ResourceRef) string {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		if ref.APIVersion == "" {
			return ref.Kind
		}
		return ref.APIVersion + "/" + ref.Kind
	}
	if gv.Group == "" || gv.Group == "core" {
		return gv.Version + "/" + ref.Kind
	}
	return gv.Group + "/" + gv.Version + "/" + ref.Kind
}

func actionMarker(action semantic.Action) string {
	switch action {
	case semantic.Create:
		return "+"
	case semantic.Update:
		return "~"
	case semantic.Delete:
		return "-"
	case semantic.Replace:
		return "-/+"
	default:
		return ""
	}
}

func actionHeadingToken(action semantic.Action) theme.Token {
	switch action {
	case semantic.Create:
		return theme.StatusSuccess
	case semantic.Delete:
		return theme.StatusError
	case semantic.Update:
		return theme.StatusWarning
	case semantic.Replace:
		return theme.AccentPrimary
	default:
		return theme.StatusWarning
	}
}

func taskHeadingToken(t semantic.TaskPlan) theme.Token {
	switch t.Action {
	case semantic.TaskCreate:
		return theme.StatusSuccess
	case semantic.TaskDelete:
		return theme.StatusError
	case semantic.TaskUpdate:
		return theme.StatusWarning
	default:
		return theme.TextPrimary
	}
}

func ownedResourceKeys(tasks []semantic.TaskPlan) map[string]struct{} {
	out := make(map[string]struct{})
	for _, t := range tasks {
		for _, ref := range t.Resources {
			out[refKey(ref)] = struct{}{}
		}
	}
	return out
}

func indexChanges(changes []semantic.ResourceChange) map[string]semantic.ResourceChange {
	out := make(map[string]semantic.ResourceChange, len(changes))
	for _, c := range changes {
		out[refKey(c.Resource)] = c
	}
	return out
}

func refKey(r semantic.ResourceRef) string {
	return r.APIVersion + "\x00" + r.Kind + "\x00" + r.Namespace + "\x00" + r.Name + "\x00" + r.GenerateName
}

func snapshotMap(s *semantic.ResourceSnapshot) map[string]any {
	if s == nil || s.Object == nil {
		return map[string]any{}
	}
	return s.Object
}

func style(opts Options, token theme.Token, s string) string {
	return opts.Theme.Style(token).Render(s)
}

func writeln(w io.Writer, opts Options, token theme.Token, s string) error {
	_, err := fmt.Fprintln(w, style(opts, token, s))
	return err
}
