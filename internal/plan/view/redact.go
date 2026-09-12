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
	"slices"
	"strings"

	"deployah.dev/deployah/internal/plan/semantic"
)

const redactedToken = "(redacted)"

func copyPlan(p semantic.Plan) semantic.Plan {
	out := p
	out.Changes = slices.Clone(p.Changes)
	for i := range out.Changes {
		c := &out.Changes[i]
		c.Before = copySnapshot(c.Before)
		c.After = copySnapshot(c.After)
		c.Fields = copyFields(c.Fields)
		if c.Origin.Helm != nil {
			h := *c.Origin.Helm
			c.Origin.Helm = &h
		}
		if c.Apply.Write != nil {
			w := *c.Apply.Write
			c.Apply.Write = &w
		}
		if c.Apply.Delete != nil {
			d := *c.Apply.Delete
			c.Apply.Delete = &d
		}
	}
	out.Diagnostics = slices.Clone(p.Diagnostics)
	for i := range out.Diagnostics {
		if out.Diagnostics[i].Resource != nil {
			r := *out.Diagnostics[i].Resource
			out.Diagnostics[i].Resource = &r
		}
	}
	if p.Executions == nil {
		out.Executions = []semantic.Execution{}
	} else {
		out.Executions = slices.Clone(p.Executions)
	}
	return out
}

func copySnapshot(s *semantic.ResourceSnapshot) *semantic.ResourceSnapshot {
	if s == nil {
		return nil
	}
	if s.Object == nil {
		return &semantic.ResourceSnapshot{}
	}
	return &semantic.ResourceSnapshot{Object: copyJSONMap(s.Object)}
}

func copyFields(fields []semantic.FieldChange) []semantic.FieldChange {
	if fields == nil {
		return nil
	}
	out := slices.Clone(fields)
	for i := range out {
		out[i].Before = copyJSONValue(out[i].Before)
		out[i].After = copyJSONValue(out[i].After)
	}
	return out
}

func copyJSONValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return copyJSONMap(val)
	case []any:
		out := make([]any, 0, len(val))
		for i := range val {
			out = append(out, copyJSONValue(val[i]))
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(val))
		for k, x := range val {
			out[k] = x
		}
		return out
	case []string:
		out := make([]string, 0, len(val))
		return append(out, val...)
	default:
		return v
	}
}

func copyJSONMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, x := range m {
		out[k] = copyJSONValue(x)
	}
	return out
}

func redactPlan(p *semantic.Plan) {
	for i := range p.Changes {
		c := &p.Changes[i]
		if !isCoreSecret(c.Resource) {
			continue
		}
		redactSnapshot(c.Before)
		redactSnapshot(c.After)
		for j := range c.Fields {
			if !isSecretDataPath(c.Fields[j].Path) {
				continue
			}
			if c.Fields[j].Before != nil {
				c.Fields[j].Before = redactedToken
			}
			if c.Fields[j].After != nil {
				c.Fields[j].After = redactedToken
			}
		}
	}
}

func isCoreSecret(ref semantic.ResourceRef) bool {
	if ref.Kind != "Secret" {
		return false
	}
	return ref.APIVersion == "v1" || ref.APIVersion == "core/v1"
}

func isSecretDataPath(path string) bool {
	return hasPointerPrefix(path, "/data") || hasPointerPrefix(path, "/stringData")
}

func hasPointerPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func redactSnapshot(s *semantic.ResourceSnapshot) {
	if s == nil || s.Object == nil {
		return
	}
	redactSecretMap(s.Object, "data")
	redactSecretMap(s.Object, "stringData")
}

func redactSecretMap(obj map[string]any, key string) {
	raw, ok := obj[key]
	if !ok {
		return
	}
	switch m := raw.(type) {
	case map[string]any:
		for k := range m {
			m[k] = redactedToken
		}
	case map[string]string:
		out := make(map[string]any, len(m))
		for k := range m {
			out[k] = redactedToken
		}
		obj[key] = out
	}
}
