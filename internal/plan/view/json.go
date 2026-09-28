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

type document struct {
	Schema     string        `json:"schema"`
	Header     headerDTO     `json:"header"`
	HelmAction string        `json:"helmAction"`
	Changes    []changeDTO   `json:"changes"`
	Tasks      []taskDTO     `json:"tasks"`
	ChartCRDs  []chartCRDDTO `json:"chartCRDs"`
	Summary    summaryDTO    `json:"summary"`
}

type headerDTO struct {
	Project      string `json:"project,omitempty"`
	Environment  string `json:"environment,omitempty"`
	Release      string `json:"release,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Context      string `json:"context,omitempty"`
	Revision     int    `json:"revision,omitempty"`
	FreshInstall bool   `json:"freshInstall,omitempty"`
}

type changeDTO struct {
	Resource resourceDTO `json:"resource"`
	Action   string      `json:"action"`
	Before   any         `json:"before"`
	After    any         `json:"after"`
	Fields   []fieldDTO  `json:"fields"`
}

type resourceDTO struct {
	APIVersion   string `json:"apiVersion"`
	Kind         string `json:"kind"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	GenerateName string `json:"generateName,omitempty"`
}

type fieldDTO struct {
	Path   string `json:"path"`
	Op     string `json:"op"`
	Before *any   `json:"before,omitempty"`
	After  *any   `json:"after,omitempty"`
}

type summaryDTO struct {
	Create int `json:"create"`
	Update int `json:"update"`
	Delete int `json:"delete"`
	Total  int `json:"total"`
}

type taskDTO struct {
	Name        string        `json:"name"`
	Phase       string        `json:"phase"`
	Action      string        `json:"action"`
	WillRun     bool          `json:"willRun"`
	Definitions []hookDefDTO  `json:"definitions"`
	Resources   []resourceDTO `json:"resources"`
}

type chartCRDDTO struct {
	Source      string `json:"source"`
	Index       int    `json:"index"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Lifecycle   string `json:"lifecycle"`
	WillProcess bool   `json:"willProcess"`
}

type hookDefDTO struct {
	Resource resourceDTO `json:"resource"`
	Action   string      `json:"action"`
	Before   any         `json:"before"`
	After    any         `json:"after"`
	Fields   []fieldDTO  `json:"fields"`
}

// WriteJSON writes the machine-readable plan document. It does not
// mutate p and does not marshal semantic types directly.
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
	tasks := make([]taskDTO, 0, len(prepared.Tasks))
	for _, t := range prepared.Tasks {
		tasks = append(tasks, toTaskDTO(t))
	}
	crds := make([]chartCRDDTO, 0, len(prepared.ChartCRDs))
	for _, c := range prepared.ChartCRDs {
		crds = append(crds, toChartCRDDTO(c))
	}
	return document{
		Schema:     SchemaV1ID,
		Header:     toHeaderDTO(prepared.Header),
		HelmAction: prepared.HelmAction.String(),
		Changes:    changes,
		Tasks:      tasks,
		ChartCRDs:  crds,
		Summary:    toSummaryDTO(prepared.Summary),
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
		fields = append(fields, toFieldDTO(f))
	}
	return changeDTO{
		Resource: toResourceDTO(c.Resource),
		Action:   c.Action.String(),
		Before:   snapshotJSON(c.Before),
		After:    snapshotJSON(c.After),
		Fields:   fields,
	}
}

func toFieldDTO(f semantic.FieldChange) fieldDTO {
	dto := fieldDTO{Path: f.Path, Op: f.Op.String()}
	switch f.Op {
	case semantic.FieldAdd:
		dto.After = new(f.After)
	case semantic.FieldRemove:
		dto.Before = new(f.Before)
	case semantic.FieldReplace:
		dto.Before = new(f.Before)
		dto.After = new(f.After)
	}
	return dto
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

func toSummaryDTO(s semantic.Summary) summaryDTO {
	return summaryDTO{
		Create: s.Create,
		Update: s.Update,
		Delete: s.Delete,
		Total:  s.Total(),
	}
}

func toTaskDTO(t semantic.TaskPlan) taskDTO {
	defs := make([]hookDefDTO, 0, len(t.Definitions))
	for _, d := range t.Definitions {
		defs = append(defs, toHookDefDTO(d))
	}
	refs := make([]resourceDTO, 0, len(t.Resources))
	for _, r := range t.Resources {
		refs = append(refs, toResourceDTO(r))
	}
	return taskDTO{
		Name:        t.Name,
		Phase:       t.Phase.String(),
		Action:      t.Action.String(),
		WillRun:     t.WillRun,
		Definitions: defs,
		Resources:   refs,
	}
}

func toChartCRDDTO(c semantic.ChartCRD) chartCRDDTO {
	return chartCRDDTO{
		Source:      c.Source,
		Index:       c.Index,
		Kind:        c.Kind,
		Name:        c.Name,
		Lifecycle:   c.Lifecycle.String(),
		WillProcess: c.WillProcess,
	}
}

func toHookDefDTO(d semantic.HookDefinition) hookDefDTO {
	fields := make([]fieldDTO, 0, len(d.Fields))
	for _, f := range d.Fields {
		fields = append(fields, toFieldDTO(f))
	}
	return hookDefDTO{
		Resource: toResourceDTO(d.Resource),
		Action:   d.Action.String(),
		Before:   snapshotJSON(d.Before),
		After:    snapshotJSON(d.After),
		Fields:   fields,
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
