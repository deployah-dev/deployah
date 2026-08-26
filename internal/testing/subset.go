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

package testing

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// DiffSubset reports JSON-pointer-style paths where want is not a subset of
// got. An empty result means every value in want is present in got.
//
// Maps: only keys present in want are compared; extra keys in got are
// ignored. A nil value in want (YAML null) requires the key to be absent
// or nil in got. Scalar slices must match in length and order. An empty
// want slice matches any got slice. Object slices (elements that are
// maps) are matched by identity key "name", then "type"; extra got
// elements are ignored.
func DiffSubset(path string, want, got any) []string {
	if path == "" {
		path = "$"
	}
	if want == nil {
		if got == nil {
			return nil
		}
		return []string{fmt.Sprintf("%s: want absent/null, got %s", path, describe(got))}
	}
	if got == nil {
		return []string{fmt.Sprintf("%s: missing, want %s", path, describe(want))}
	}

	if wantMap, ok := asMap(want); ok {
		gotMap, gotOK := asMap(got)
		if !gotOK {
			return []string{fmt.Sprintf("%s: want map, got %s", path, describe(got))}
		}
		return diffMap(path, wantMap, gotMap)
	}

	if wantSlice, ok := asSlice(want); ok {
		gotSlice, gotOK := asSlice(got)
		if !gotOK {
			return []string{fmt.Sprintf("%s: want list, got %s", path, describe(got))}
		}
		return diffSlice(path, wantSlice, gotSlice)
	}

	if numbersEqual(want, got) {
		return nil
	}
	if reflect.DeepEqual(want, got) {
		return nil
	}
	return []string{fmt.Sprintf("%s: want %s, got %s", path, describe(want), describe(got))}
}

// DiffContainsByKey reports paths where each object in want is not found
// among got by identity key ("name", then "type") and recursive subset
// match. Extra got items are ignored. An empty want matches anything.
func DiffContainsByKey(path string, want, got []any) []string {
	if path == "" {
		path = "$"
	}
	if len(want) == 0 {
		return nil
	}
	var diffs []string
	for i, w := range want {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		wm, ok := asMap(w)
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: want object, got %s", itemPath, describe(w)))
			continue
		}
		key, keyName := identityKey(wm)
		matched := false
		for _, g := range got {
			gm, gotOK := asMap(g)
			if !gotOK {
				continue
			}
			if keyName != "" {
				gv, has := gm[keyName]
				if !has || !reflect.DeepEqual(gv, key) {
					continue
				}
			}
			itemDiffs := DiffSubset(itemPath, w, g)
			if len(itemDiffs) == 0 {
				matched = true
				break
			}
			// Keep subset diffs when the identity key matched.
			if keyName != "" {
				diffs = append(diffs, itemDiffs...)
				matched = true
				break
			}
		}
		if !matched {
			if keyName != "" {
				diffs = append(diffs, fmt.Sprintf("%s: no item with %s=%s", itemPath, keyName, describe(key)))
			} else {
				diffs = append(diffs, fmt.Sprintf("%s: no matching object", itemPath))
			}
		}
	}
	return diffs
}

func diffMap(path string, want, got map[string]any) []string {
	var diffs []string
	for k, wv := range want {
		child := path + "." + k
		gv, ok := got[k]
		if wv == nil {
			if ok && gv != nil {
				diffs = append(diffs, fmt.Sprintf("%s: want absent/null, got %s", child, describe(gv)))
			}
			continue
		}
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: missing, want %s", child, describe(wv)))
			continue
		}
		diffs = append(diffs, DiffSubset(child, wv, gv)...)
	}
	return diffs
}

func diffSlice(path string, want, got []any) []string {
	if len(want) == 0 {
		return nil
	}
	if isObjectSlice(want) {
		return DiffContainsByKey(path, want, got)
	}
	if len(want) != len(got) {
		return []string{fmt.Sprintf("%s: want list len %d, got %d", path, len(want), len(got))}
	}
	var diffs []string
	for i := range want {
		diffs = append(diffs, DiffSubset(fmt.Sprintf("%s[%d]", path, i), want[i], got[i])...)
	}
	return diffs
}

func isObjectSlice(s []any) bool {
	for _, v := range s {
		if _, ok := asMap(v); ok {
			return true
		}
	}
	return false
}

func identityKey(m map[string]any) (any, string) {
	if v, ok := m["name"]; ok && v != nil {
		return v, "name"
	}
	if v, ok := m["type"]; ok && v != nil {
		return v, "type"
	}
	return nil, ""
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case []map[string]any:
		out := make([]any, 0, len(s))
		for _, m := range s {
			out = append(out, m)
		}
		return out, true
	default:
		return nil, false
	}
}

func numbersEqual(a, b any) bool {
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	return aok && bok && af == bf
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func describe(v any) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%T(%v)", v, v)
}
