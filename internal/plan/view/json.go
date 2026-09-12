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
	"encoding/json"
	"fmt"
	"io"

	"deployah.dev/deployah/internal/plan/semantic"
)

const jsonSchema = "deployah.semantic_plan.v1"

type document struct {
	Schema       string         `json:"schema"`
	Header       headerDTO      `json:"header"`
	Changes      []changeDTO    `json:"changes"`
	Executions   []executionDTO `json:"executions"`
	Diagnostics  []diagDTO      `json:"diagnostics"`
	Summary      summaryDTO     `json:"summary"`
	Completeness string         `json:"completeness"`
}

type headerDTO struct {
	Project      string `json:"project,omitempty"`
	Environment  string `json:"environment,omitempty"`
	Release      string `json:"release,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Context      string `json:"context,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	FreshInstall bool   `json:"fresh_install,omitempty"`
}

type changeDTO struct {
	Resource resourceDTO `json:"resource"`
	Origin   originDTO   `json:"origin"`
	Action   string      `json:"action"`
	Before   any         `json:"before"`
	After    any         `json:"after"`
	Fields   []fieldDTO  `json:"fields"`
	Apply    applyDTO    `json:"apply"`
}

type resourceDTO struct {
	APIVersion   string `json:"api_version"`
	Kind         string `json:"kind"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	GenerateName string `json:"generate_name,omitempty"`
}

type originDTO struct {
	Kind string   `json:"kind"`
	Helm *helmDTO `json:"helm,omitempty"`
}

type helmDTO struct {
	Release   string `json:"release"`
	Namespace string `json:"namespace"`
}

type applyDTO struct {
	Write  *writeDTO  `json:"write,omitempty"`
	Delete *deleteDTO `json:"delete,omitempty"`
}

type writeDTO struct {
	Method         string `json:"method"`
	FieldManager   string `json:"field_manager"`
	ForceConflicts bool   `json:"force_conflicts"`
}

type deleteDTO struct {
	Propagation string `json:"propagation"`
}

type fieldDTO struct {
	Path   string `json:"path"`
	Op     string `json:"op"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

type diagDTO struct {
	Severity string       `json:"severity"`
	Category string       `json:"category"`
	Message  string       `json:"message"`
	Resource *resourceDTO `json:"resource,omitempty"`
}

type summaryDTO struct {
	Create   int `json:"create"`
	Update   int `json:"update"`
	Delete   int `json:"delete"`
	Recreate int `json:"recreate"`
	Total    int `json:"total"`
}

type executionDTO struct{}

// WriteJSON writes the Stage C machine-readable plan document. It does
// not mutate p and does not marshal semantic types directly.
func WriteJSON(w io.Writer, p semantic.Plan, opts Options) error {
	doc, err := newDocument(p, opts)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err = enc.Encode(doc); err != nil {
		return fmt.Errorf("encode plan document: %w", err)
	}
	return nil
}

func newDocument(p semantic.Plan, opts Options) (document, error) {
	prepared, err := prepareRender(p, opts)
	if err != nil {
		return document{}, err
	}
	changes := make([]changeDTO, 0, len(prepared.Changes))
	for _, c := range prepared.Changes {
		changes = append(changes, toChangeDTO(c))
	}
	diags := make([]diagDTO, 0, len(prepared.Diagnostics))
	for _, d := range prepared.Diagnostics {
		diags = append(diags, toDiagDTO(d))
	}
	return document{
		Schema:       jsonSchema,
		Header:       toHeaderDTO(prepared.Header),
		Changes:      changes,
		Executions:   []executionDTO{},
		Diagnostics:  diags,
		Summary:      toSummaryDTO(prepared.Summary),
		Completeness: prepared.Completeness.String(),
	}, nil
}

func toHeaderDTO(h semantic.Header) headerDTO {
	return headerDTO{
		Project:      h.Project,
		Environment:  h.Environment,
		Release:      h.Release,
		Namespace:    h.Namespace,
		Context:      h.Context,
		Revision:     h.Revision,
		FreshInstall: h.FreshInstall,
	}
}

func toChangeDTO(c semantic.ResourceChange) changeDTO {
	fields := make([]fieldDTO, 0, len(c.Fields))
	for _, f := range c.Fields {
		fields = append(fields, fieldDTO{
			Path:   f.Path,
			Op:     f.Op.String(),
			Before: f.Before,
			After:  f.After,
		})
	}
	origin := originDTO{Kind: c.Origin.Kind.String()}
	if c.Origin.Helm != nil {
		origin.Helm = &helmDTO{
			Release:   c.Origin.Helm.Release,
			Namespace: c.Origin.Helm.Namespace,
		}
	}
	apply := applyDTO{}
	if c.Apply.Write != nil {
		apply.Write = &writeDTO{
			Method:         c.Apply.Write.Method.String(),
			FieldManager:   c.Apply.Write.FieldManager,
			ForceConflicts: c.Apply.Write.ForceConflicts,
		}
	}
	if c.Apply.Delete != nil {
		apply.Delete = &deleteDTO{Propagation: c.Apply.Delete.Propagation.String()}
	}
	return changeDTO{
		Resource: toResourceDTO(c.Resource),
		Origin:   origin,
		Action:   c.Action.String(),
		Before:   snapshotJSON(c.Before),
		After:    snapshotJSON(c.After),
		Fields:   fields,
		Apply:    apply,
	}
}

func toResourceDTO(r semantic.ResourceRef) resourceDTO {
	return resourceDTO{
		APIVersion:   r.APIVersion,
		Kind:         r.Kind,
		Namespace:    r.Namespace,
		Name:         r.Name,
		GenerateName: r.GenerateName,
	}
}

func toDiagDTO(d semantic.Diagnostic) diagDTO {
	dto := diagDTO{
		Severity: d.Severity.String(),
		Category: d.Category.String(),
		Message:  d.Message,
	}
	if d.Resource != nil {
		r := toResourceDTO(*d.Resource)
		dto.Resource = &r
	}
	return dto
}

func toSummaryDTO(s semantic.Summary) summaryDTO {
	return summaryDTO{
		Create:   s.Create,
		Update:   s.Update,
		Delete:   s.Delete,
		Recreate: s.Recreate,
		Total:    s.Total(),
	}
}

func snapshotJSON(s *semantic.ResourceSnapshot) any {
	if s == nil {
		return nil
	}
	if s.Object == nil {
		return map[string]any{}
	}
	return s.Object
}
