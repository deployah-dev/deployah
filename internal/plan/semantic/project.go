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

package semantic

import (
	"fmt"
	"strconv"
	"strings"
)

// ProjectOntoDeclared returns a copy of live containing only the JSON
// surface declared by the union of declared objects. Live-only map keys
// and array tails are dropped. Named map-array misalignment is an error.
// It does not mutate live or declared.
func ProjectOntoDeclared(live map[string]any, declared ...map[string]any) (map[string]any, error) {
	if live == nil {
		return map[string]any{}, nil
	}
	surfaces := make([]any, 0, len(declared))
	for _, d := range declared {
		if d == nil {
			continue
		}
		surfaces = append(surfaces, d)
	}
	if len(surfaces) == 0 {
		return map[string]any{}, nil
	}
	projected, err := projectValue(live, surfaces, "")
	if err != nil {
		return nil, err
	}
	out, ok := projected.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("projected live is %T, want object", projected)
	}
	return out, nil
}

func projectValue(live any, declared []any, path string) (any, error) {
	if len(declared) == 0 {
		return omittedProjection{}, nil
	}
	if live == nil {
		return jsonNullProjection{}, nil
	}
	if liveMap, ok := asJSONMap(live); ok {
		return projectMap(liveMap, declaredMaps(declared), path)
	}
	if liveArr, ok := asJSONArray(live); ok {
		return projectArray(liveArr, declaredArrays(declared), path)
	}
	return copyJSONValue(live), nil
}

func projectMap(live map[string]any, declared []map[string]any, path string) (map[string]any, error) {
	keys := map[string]struct{}{}
	for _, d := range declared {
		for k := range d {
			keys[k] = struct{}{}
		}
	}
	out := make(map[string]any, len(keys))
	for k := range keys {
		liveChild, hasLive := live[k]
		if !hasLive {
			continue
		}
		childDeclared := make([]any, 0, len(declared))
		for _, d := range declared {
			if v, ok := d[k]; ok {
				childDeclared = append(childDeclared, v)
			}
		}
		projected, err := projectValue(liveChild, childDeclared, path+"/"+escapePointerToken(k))
		if err != nil {
			return nil, err
		}
		value, keep := unwrapProjection(projected)
		if !keep {
			continue
		}
		out[k] = value
	}
	return out, nil
}

func projectArray(live []any, declared [][]any, path string) ([]any, error) {
	declaredLen := 0
	for _, d := range declared {
		if len(d) > declaredLen {
			declaredLen = len(d)
		}
	}
	if declaredLen == 0 {
		return []any{}, nil
	}
	if err := checkNamedArrayAlignment(live, declared, path, declaredLen); err != nil {
		return nil, err
	}
	n := min(len(live), declaredLen)
	out := make([]any, 0, n)
	for i := range n {
		childDeclared := make([]any, 0, len(declared))
		for _, d := range declared {
			if i < len(d) {
				childDeclared = append(childDeclared, d[i])
			}
		}
		projected, err := projectValue(live[i], childDeclared, path+"/"+strconv.Itoa(i))
		if err != nil {
			return nil, err
		}
		value, _ := unwrapProjection(projected)
		out = append(out, value)
	}
	return out, nil
}

func checkNamedArrayAlignment(live []any, declared [][]any, path string, declaredLen int) error {
	if !declaredArraysNamed(declared) {
		return nil
	}
	desired := lastDeclaredArray(declared)
	previous := firstDeclaredArray(declared)
	for i := range min(len(live), declaredLen) {
		liveName, liveOK := elementName(live, i)
		wantName, wantOK := declaredNameAt(desired, previous, i)
		if !liveOK || !wantOK {
			return fmt.Errorf("named list at %s is misaligned at index %d", path, i)
		}
		if liveName != wantName {
			return fmt.Errorf("named list at %s is misaligned at index %d: live %q, declared %q", path, i, liveName, wantName)
		}
	}
	return nil
}

func declaredArraysNamed(declared [][]any) bool {
	anyElem := false
	for _, d := range declared {
		for _, el := range d {
			anyElem = true
			if _, ok := elementName([]any{el}, 0); !ok {
				return false
			}
		}
	}
	return anyElem
}

func declaredNameAt(desired, previous []any, i int) (string, bool) {
	if i < len(desired) {
		return elementName(desired, i)
	}
	return elementName(previous, i)
}

func firstDeclaredArray(declared [][]any) []any {
	if len(declared) == 0 {
		return nil
	}
	return declared[0]
}

func lastDeclaredArray(declared [][]any) []any {
	if len(declared) == 0 {
		return nil
	}
	return declared[len(declared)-1]
}

func elementName(arr []any, i int) (string, bool) {
	if i < 0 || i >= len(arr) {
		return "", false
	}
	m, ok := asJSONMap(arr[i])
	if !ok {
		return "", false
	}
	raw, ok := m["name"]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func declaredMaps(declared []any) []map[string]any {
	out := make([]map[string]any, 0, len(declared))
	for _, d := range declared {
		if m, ok := asJSONMap(d); ok {
			out = append(out, m)
		}
	}
	return out
}

func declaredArrays(declared []any) [][]any {
	out := make([][]any, 0, len(declared))
	for _, d := range declared {
		if a, ok := asJSONArray(d); ok {
			out = append(out, a)
		}
	}
	return out
}

func asJSONMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[string]string:
		out := make(map[string]any, len(m))
		for k, x := range m {
			out[k] = x
		}
		return out, true
	default:
		return nil, false
	}
}

func asJSONArray(v any) ([]any, bool) {
	switch a := v.(type) {
	case []any:
		return a, true
	case []string:
		out := make([]any, 0, len(a))
		for _, s := range a {
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func escapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

type omittedProjection struct{}

type jsonNullProjection struct{}

func unwrapProjection(projected any) (any, bool) {
	switch projected.(type) {
	case omittedProjection:
		return nil, false
	case jsonNullProjection:
		return nil, true
	default:
		return projected, true
	}
}
