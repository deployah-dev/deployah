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

package semantic

import (
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// FieldOp classifies one structural field difference. The zero value is
// invalid.
type FieldOp int

const (
	// FieldAdd is a path present only in After.
	FieldAdd FieldOp = iota + 1
	// FieldRemove is a path present only in Before.
	FieldRemove
	// FieldReplace is a scalar replacement or a JSON type change.
	FieldReplace
)

func (o FieldOp) String() string {
	switch o {
	case FieldAdd:
		return "add"
	case FieldRemove:
		return "remove"
	case FieldReplace:
		return "replace"
	default:
		return fmt.Sprintf("FieldOp(%d)", int(o))
	}
}

// FieldChange is one structural difference between Before and After.
// Path is an RFC 6901 JSON Pointer. Values are copies, not aliases into
// snapshots. This type is not a JSON rendering contract.
type FieldChange struct {
	Path   string
	Op     FieldOp
	Before any
	After  any
}

// DiffFields walks two unstructured objects and returns deterministic
// field changes. It does not mutate before or after.
func DiffFields(before, after map[string]any) []FieldChange {
	if before == nil {
		before = map[string]any{}
	}
	if after == nil {
		after = map[string]any{}
	}
	var out []FieldChange
	walkMaps("", before, after, &out)
	out = filterBookkeeping(out)
	slices.SortFunc(out, func(a, b FieldChange) int {
		return cmp.Compare(a.Path, b.Path)
	})
	return out
}

func walkMaps(parent string, before, after map[string]any, out *[]FieldChange) {
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range after {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		path := joinPointer(parent, key)
		bv, bOk := before[key]
		av, aOk := after[key]
		switch {
		case !bOk:
			*out = append(*out, FieldChange{
				Path:  path,
				Op:    FieldAdd,
				After: copyJSONValue(av),
			})
		case !aOk:
			*out = append(*out, FieldChange{
				Path:   path,
				Op:     FieldRemove,
				Before: copyJSONValue(bv),
			})
		default:
			walkValue(path, bv, av, out)
		}
	}
}

func walkSlices(parent string, before, after []any, out *[]FieldChange) {
	n := min(len(before), len(after))
	for i := range n {
		walkValue(joinPointer(parent, strconv.Itoa(i)), before[i], after[i], out)
	}
	for i := n; i < len(before); i++ {
		*out = append(*out, FieldChange{
			Path:   joinPointer(parent, strconv.Itoa(i)),
			Op:     FieldRemove,
			Before: copyJSONValue(before[i]),
		})
	}
	for i := n; i < len(after); i++ {
		*out = append(*out, FieldChange{
			Path:  joinPointer(parent, strconv.Itoa(i)),
			Op:    FieldAdd,
			After: copyJSONValue(after[i]),
		})
	}
}

func walkValue(path string, before, after any, out *[]FieldChange) {
	bMap, bIsMap := asMap(before)
	aMap, aIsMap := asMap(after)
	if bIsMap && aIsMap {
		walkMaps(path, bMap, aMap, out)
		return
	}
	bArr, bIsArr := asSlice(before)
	aArr, aIsArr := asSlice(after)
	if bIsArr && aIsArr {
		walkSlices(path, bArr, aArr, out)
		return
	}
	if jsonEqual(before, after) {
		return
	}
	*out = append(*out, FieldChange{
		Path:   path,
		Op:     FieldReplace,
		Before: copyJSONValue(before),
		After:  copyJSONValue(after),
	})
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[string]string:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[k] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func asSlice(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		out := make([]any, 0, len(s))
		for _, val := range s {
			out = append(out, val)
		}
		return out, true
	default:
		return nil, false
	}
}

func jsonEqual(a, b any) bool {
	if an, aNum := asNumber(a); aNum {
		bn, bNum := asNumber(b)
		return bNum && an == bn
	}
	if _, bNum := asNumber(b); bNum {
		return false
	}
	return reflect.DeepEqual(a, b)
}

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
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

func joinPointer(parent, token string) string {
	return parent + "/" + escapePointerToken(token)
}

func escapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
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

func copySnapshot(s *ResourceSnapshot) *ResourceSnapshot {
	if s == nil {
		return nil
	}
	if s.Object == nil {
		return &ResourceSnapshot{}
	}
	return &ResourceSnapshot{Object: copyJSONMap(s.Object)}
}

func snapshotObject(s *ResourceSnapshot) map[string]any {
	if s == nil {
		return nil
	}
	return s.Object
}

func filterBookkeeping(in []FieldChange) []FieldChange {
	out := make([]FieldChange, 0, len(in))
	for _, f := range in {
		if isBookkeepingPath(f.Path) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func isBookkeepingPath(path string) bool {
	prefixes := []string{
		"/status",
		"/metadata/resourceVersion",
		"/metadata/uid",
		"/metadata/generation",
		"/metadata/creationTimestamp",
		"/metadata/managedFields",
	}
	for _, p := range prefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
