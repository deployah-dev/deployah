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
	"strconv"
	"strings"

	"deployah.dev/deployah/internal/plan/semantic"
)

// Distinct only while go-udiff compares redacted Secret leaves. Replaced
// with [redactedToken] before any user-visible write.
const (
	secretSentinelBefore = "deployah-secret-before-9f2a7c4e"
	secretSentinelAfter  = "deployah-secret-after-9f2a7c4e"
)

func humanObject(s *semantic.ResourceSnapshot) map[string]any {
	// snapshotMap never returns nil, and copyJSONMap preserves that.
	obj := copyJSONMap(snapshotMap(s))
	stripBookkeeping(obj)
	return obj
}

func stripBookkeeping(obj map[string]any) {
	for _, pointer := range semantic.BookkeepingPointers() {
		deleteAtPointer(obj, pointer)
	}
}

func markSecretDiffs(before, after map[string]any, fields []semantic.FieldChange) {
	for _, f := range fields {
		if !isSecretDataPath(f.Path) {
			continue
		}
		switch f.Op {
		case semantic.FieldAdd:
			setSecretMarker(after, f.Path, secretSentinelAfter)
		case semantic.FieldRemove:
			setSecretMarker(before, f.Path, secretSentinelBefore)
		case semantic.FieldReplace:
			setSecretMarker(before, f.Path, secretSentinelBefore)
			setSecretMarker(after, f.Path, secretSentinelAfter)
		}
	}
}

func secretSentinelFields(fields []semantic.FieldChange) []semantic.FieldChange {
	out := make([]semantic.FieldChange, 0, len(fields))
	out = append(out, fields...)
	for i := range out {
		if !isSecretDataPath(out[i].Path) {
			continue
		}
		switch out[i].Op {
		case semantic.FieldAdd:
			out[i].After = secretSentinelValue(out[i].After, secretSentinelAfter)
		case semantic.FieldRemove:
			out[i].Before = secretSentinelValue(out[i].Before, secretSentinelBefore)
		case semantic.FieldReplace:
			out[i].Before = secretSentinelValue(out[i].Before, secretSentinelBefore)
			out[i].After = secretSentinelValue(out[i].After, secretSentinelAfter)
		}
	}
	return out
}

func secretSentinelValue(v any, sentinel string) any {
	switch m := v.(type) {
	case map[string]any:
		out := copyJSONMap(m)
		markLeaves(out, sentinel)
		return out
	case map[string]string:
		out := make(map[string]any, len(m))
		for k := range m {
			out[k] = sentinel
		}
		return out
	case []any:
		out, ok := copyJSONValue(m).([]any)
		if !ok {
			return sentinel
		}
		markSliceLeaves(out, sentinel)
		return out
	default:
		return sentinel
	}
}

func setSecretMarker(obj map[string]any, pointer, sentinel string) {
	cur, ok := lookupPointer(obj, pointer)
	if !ok {
		return
	}
	switch v := cur.(type) {
	case map[string]any:
		markLeaves(v, sentinel)
	case map[string]string:
		for k := range v {
			v[k] = sentinel
		}
	case []any:
		markSliceLeaves(v, sentinel)
	default:
		setAtPointer(obj, pointer, sentinel)
	}
}

func markLeaves(m map[string]any, sentinel string) {
	for k, x := range m {
		switch c := x.(type) {
		case map[string]any:
			markLeaves(c, sentinel)
		case []any:
			markSliceLeaves(c, sentinel)
		case nil:
		default:
			m[k] = sentinel
		}
	}
}

func markSliceLeaves(s []any, sentinel string) {
	for i, x := range s {
		switch c := x.(type) {
		case map[string]any:
			markLeaves(c, sentinel)
		case []any:
			markSliceLeaves(c, sentinel)
		case nil:
		default:
			s[i] = sentinel
		}
	}
}

func revealSecretSentinels(s string) string {
	s = strings.ReplaceAll(s, secretSentinelBefore, redactedToken)
	return strings.ReplaceAll(s, secretSentinelAfter, redactedToken)
}

func pointerTokens(pointer string) ([]string, bool) {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	var tokens []string
	for token := range strings.SplitSeq(pointer[1:], "/") {
		tokens = append(tokens, unescapePointerToken(token))
	}
	return tokens, true
}

func unescapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

func lookupPointer(obj map[string]any, pointer string) (any, bool) {
	if pointer == "" {
		return obj, true
	}
	tokens, ok := pointerTokens(pointer)
	if !ok {
		return nil, false
	}
	var cur any = obj
	for _, token := range tokens {
		switch node := cur.(type) {
		case map[string]any:
			next, found := node[token]
			if !found {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

func parentMap(obj map[string]any, tokens []string) (map[string]any, bool) {
	if len(tokens) == 0 {
		return obj, true
	}
	var cur any = obj
	for _, token := range tokens {
		node, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, found := node[token]
		if !found {
			return nil, false
		}
		cur = next
	}
	parent, ok := cur.(map[string]any)
	return parent, ok
}

func deleteAtPointer(obj map[string]any, pointer string) {
	tokens, ok := pointerTokens(pointer)
	if !ok || len(tokens) == 0 {
		return
	}
	if len(tokens) == 1 {
		delete(obj, tokens[0])
		return
	}
	parent, found := parentMap(obj, tokens[:len(tokens)-1])
	if found {
		delete(parent, tokens[len(tokens)-1])
	}
}

func setAtPointer(obj map[string]any, pointer string, value any) {
	tokens, ok := pointerTokens(pointer)
	if !ok || len(tokens) == 0 {
		return
	}
	if len(tokens) == 1 {
		obj[tokens[0]] = value
		return
	}
	parent, found := parentMap(obj, tokens[:len(tokens)-1])
	if found {
		parent[tokens[len(tokens)-1]] = value
	}
}
