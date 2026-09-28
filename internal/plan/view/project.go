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
			projectInto(afterRoot, after, before, tokens, f.After)
		case semantic.FieldRemove:
			projectInto(beforeRoot, before, after, tokens, f.Before)
		case semantic.FieldReplace:
			projectInto(beforeRoot, before, after, tokens, f.Before)
			projectInto(afterRoot, after, before, tokens, f.After)
		}
	}
	return asObject(beforeRoot.toValue()), asObject(afterRoot.toValue())
}

// projectInto puts leaf at tokens even when src has no such path.
// src supplies list or map shape and any name sibling. other supplies
// that shape when src does not. Otherwise the rest are map keys.
func projectInto(n *projNode, src, other any, tokens []string, leaf any) {
	if n == nil {
		return
	}
	if len(tokens) == 0 {
		n.leaf = copyJSONValue(leaf)
		n.hasLeaf = true
		return
	}
	if asMap, ok := stringMap(src); ok {
		projectInto(n, asMap, other, tokens, leaf)
		return
	}
	token := tokens[0]
	if projectionIsList(src, other) {
		idx, err := strconv.Atoi(token)
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
			if !copyNameSibling(child, listItem(src, idx)) {
				copyNameSibling(child, listItem(other, idx))
			}
		}
		projectInto(child, listItem(src, idx), listItem(other, idx), tokens[1:], leaf)
		return
	}
	if n.m == nil {
		n.m = map[string]*projNode{}
	}
	child, ok := n.m[token]
	if !ok {
		child = &projNode{}
		n.m[token] = child
	}
	projectInto(child, mapItem(src, token), mapItem(other, token), tokens[1:], leaf)
}

func projectionIsList(src, other any) bool {
	if _, ok := src.([]any); ok {
		return true
	}
	if _, ok := src.(map[string]any); ok {
		return false
	}
	_, ok := other.([]any)
	return ok
}

func stringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]string)
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		out[k] = val
	}
	return out, true
}

func mapItem(v any, key string) any {
	switch node := v.(type) {
	case map[string]any:
		return node[key]
	case map[string]string:
		if val, ok := node[key]; ok {
			return val
		}
	}
	return nil
}

func listItem(v any, idx int) any {
	list, ok := v.([]any)
	if !ok || idx < 0 || idx >= len(list) {
		return nil
	}
	return list[idx]
}

func copyNameSibling(n *projNode, item any) bool {
	m, ok := item.(map[string]any)
	if !ok {
		return false
	}
	name, ok := m["name"]
	if !ok {
		return false
	}
	switch name.(type) {
	case string, int, int32, int64, float32, float64, bool:
		n.hasName = true
		n.name = copyJSONValue(name)
		return true
	default:
		return false
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
