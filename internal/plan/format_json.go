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
	"encoding/json"
	"fmt"
	"io"
)

// jsonFormatVersion is the schema version emitted by [NewJSONDocument]. Bump
// it, and document the change, whenever a field is added, removed, or
// changes meaning. Two deliberate omissions (no next revision, no
// spec-vocabulary path) keep 1.0 stable.
const jsonFormatVersion = "1.0"

// JSONDocument is the "--output json" wire format for a [Plan]
// (format_version "1.0"). Field names use snake_case.
type JSONDocument struct {
	FormatVersion string `json:"format_version"`
	Project       string `json:"project"`
	Environment   string `json:"environment"`
	Release       string `json:"release"`
	Namespace     string `json:"namespace"`
	Context       string `json:"context"`
	// Revision is null when FreshInstall is true: there is no current
	// revision to report yet.
	Revision     *int         `json:"revision"`
	FreshInstall bool         `json:"fresh_install"`
	Warning      string       `json:"warning,omitempty"`
	Changes      []JSONChange `json:"changes"`
	// HooksChanged is true when Helm hooks changed without a matching entry
	// in Changes, so a hook-only change still explains a non-zero
	// --detailed-exitcode against an otherwise-empty Changes/Summary.
	HooksChanged bool        `json:"hooks_changed,omitempty"`
	Summary      JSONSummary `json:"summary"`
	// Drift and DriftIncomplete are omitted when there is nothing to report
	// (either --drift wasn't requested, or it found nothing); the schema
	// does not distinguish those two cases.
	Drift           []JSONChange `json:"drift,omitempty"`
	DriftIncomplete []string     `json:"drift_incomplete,omitempty"`
}

// JSONChange is one entry in [JSONDocument.Changes].
type JSONChange struct {
	Action     Action      `json:"action"`
	Kind       string      `json:"kind"`
	APIVersion string      `json:"api_version"`
	Name       string      `json:"name"`
	Namespace  string      `json:"namespace"`
	Fields     []JSONField `json:"fields"`
}

// JSONField is one entry in [JSONChange.Fields]. A masked field omits Old
// and New and carries Change (the [FieldChangeKind] as a string) instead;
// an unmasked field carries Old/New and omits Masked and Change.
type JSONField struct {
	Path   string `json:"path"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new,omitempty"`
	Masked bool   `json:"masked,omitempty"`
	Change string `json:"change,omitempty"`
}

// JSONSummary is [JSONDocument.Summary].
type JSONSummary struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
}

// NewJSONDocument converts p into the format_version "1.0" JSON document.
// It masks secret field values unconditionally (calling [ApplyMasking] is
// safe to repeat): JSON output ignores --show-secrets by design, so a CI
// job can pipe it anywhere without a credential-leak review.
func NewJSONDocument(p *Plan) *JSONDocument {
	ApplyMasking(p)

	doc := &JSONDocument{
		FormatVersion: jsonFormatVersion,
		Project:       p.Header.Project,
		Environment:   p.Header.Environment,
		Release:       p.Header.Release,
		Namespace:     p.Header.Namespace,
		Context:       p.Header.Context,
		FreshInstall:  p.Header.FreshInstall,
		Warning:       p.Header.Warning,
		Changes:       make([]JSONChange, 0, len(p.Changes)),
		HooksChanged:  p.HooksChanged,
		Summary: JSONSummary{
			Add:     p.Summary.Add,
			Change:  p.Summary.Change,
			Destroy: p.Summary.Destroy,
		},
	}
	if !p.Header.FreshInstall && p.Header.Revision > 0 {
		revision := p.Header.Revision
		doc.Revision = &revision
	}

	for _, c := range p.Changes {
		doc.Changes = append(doc.Changes, toJSONChange(c))
	}

	for _, c := range p.Drift {
		doc.Drift = append(doc.Drift, toJSONChange(c))
	}
	doc.DriftIncomplete = p.DriftIncomplete

	return doc
}

// toJSONChange converts one [Change] to its JSON wire representation,
// applying the same masking rule [NewJSONDocument] uses for ordinary
// changes.
func toJSONChange(c Change) JSONChange {
	jc := JSONChange{
		Action:     c.Action,
		Kind:       c.Kind,
		APIVersion: c.APIVersion,
		Name:       c.Name,
		Namespace:  c.Namespace,
		Fields:     make([]JSONField, 0, len(c.Fields)),
	}
	for _, f := range c.Fields {
		if f.Masked {
			jc.Fields = append(jc.Fields, JSONField{
				Path:   f.Path,
				Masked: true,
				Change: string(f.ChangeKind),
			})
			continue
		}
		jc.Fields = append(jc.Fields, JSONField{
			Path: f.Path,
			Old:  f.Old,
			New:  f.New,
		})
	}
	return jc
}

// RenderJSON writes p to w as pretty-printed format_version "1.0" JSON; see
// [NewJSONDocument].
func RenderJSON(w io.Writer, p *Plan) error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(NewJSONDocument(p))
}
