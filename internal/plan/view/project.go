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

package view

import (
	"slices"
	"strconv"

	"deployah.dev/deployah/internal/plan/semantic"
)

type projNode struct {
	m       map[string]*projNode
	a       map[int]*projNode
	leaf    any
	hasLeaf bool
	name    any
	hasName bool
}

func projectFields(before, after map[string]any, fields []semantic.FieldChange) (map[string]any, map[string]any) {
	beforeRoot := &projNode{}
	afterRoot := &projNode{}
	for _, f := range fields {
		tokens, ok := pointerTokens(f.Path)
		if !ok || len(tokens) == 0 {
			continue
		}
		switch f.Op {
		case semantic.FieldAdd:
			projectInto(afterRoot, after, tokens, f.After, true)
		case semantic.FieldRemove:
			projectInto(beforeRoot, before, tokens, f.Before, true)
		case semantic.FieldReplace:
			projectInto(beforeRoot, before, tokens, f.Before, true)
			projectInto(afterRoot, after, tokens, f.After, true)
		}
	}
	return asObject(beforeRoot.toValue()), asObject(afterRoot.toValue())
}

func projectInto(n *projNode, src any, tokens []string, leaf any, includeLeaf bool) {
	if n == nil {
		return
	}
	if len(tokens) == 0 {
		if includeLeaf {
			n.leaf = copyJSONValue(leaf)
			n.hasLeaf = true
		}
		return
	}
	switch s := src.(type) {
	case map[string]any:
		if n.m == nil {
			n.m = map[string]*projNode{}
		}
		key := tokens[0]
		child, ok := n.m[key]
		if !ok {
			child = &projNode{}
			n.m[key] = child
		}
		projectInto(child, s[key], tokens[1:], leaf, includeLeaf)
	case map[string]string:
		asAny := make(map[string]any, len(s))
		for k, v := range s {
			asAny[k] = v
		}
		projectInto(n, asAny, tokens, leaf, includeLeaf)
	case []any:
		idx, err := strconv.Atoi(tokens[0])
		if err != nil || idx < 0 {
			return
		}
		if n.a == nil {
			n.a = map[int]*projNode{}
		}
		child, ok := n.a[idx]
		if !ok {
			child = &projNode{}
			n.a[idx] = child
			if idx < len(s) {
				copyNameSibling(child, s[idx])
			}
		}
		var next any
		if idx < len(s) {
			next = s[idx]
		}
		projectInto(child, next, tokens[1:], leaf, includeLeaf)
	default:
		if includeLeaf && len(tokens) == 0 {
			n.leaf = copyJSONValue(leaf)
			n.hasLeaf = true
		}
	}
}

func copyNameSibling(n *projNode, item any) {
	m, ok := item.(map[string]any)
	if !ok {
		return
	}
	name, ok := m["name"]
	if !ok {
		return
	}
	switch name.(type) {
	case string, int, int32, int64, float32, float64, bool:
		n.hasName = true
		n.name = copyJSONValue(name)
	}
}

func (n *projNode) toValue() any {
	if n == nil {
		return map[string]any{}
	}
	if n.hasLeaf {
		return n.leaf
	}
	if len(n.a) > 0 {
		idxs := make([]int, 0, len(n.a))
		for i := range n.a {
			idxs = append(idxs, i)
		}
		slices.Sort(idxs)
		out := make([]any, 0, len(idxs))
		for _, i := range idxs {
			out = append(out, n.a[i].toValue())
		}
		return out
	}
	out := map[string]any{}
	if n.hasName {
		out["name"] = n.name
	}
	keys := make([]string, 0, len(n.m))
	for k := range n.m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		out[k] = n.m[k].toValue()
	}
	return out
}

func asObject(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	return m
}
