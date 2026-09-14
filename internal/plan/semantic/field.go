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
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/wI2L/jsondiff"
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

const jsonPointerAppend = "/-"

// DiffFields compares two unstructured objects and returns deterministic
// field changes. It encodes the snapshots, diffs them with an RFC 6902
// engine that keeps JSON numbers exact, then maps add, remove, and
// replace operations onto [FieldChange]. It does not mutate before or
// after.
func DiffFields(before, after map[string]any) ([]FieldChange, error) {
	src, err := encodeSnapshot(before)
	if err != nil {
		return nil, fmt.Errorf("encode before snapshot: %w", err)
	}
	tgt, err := encodeSnapshot(after)
	if err != nil {
		return nil, fmt.Errorf("encode after snapshot: %w", err)
	}
	patch, err := jsondiff.CompareJSON(src, tgt, jsondiff.UnmarshalFunc(unmarshalUseNumber))
	if err != nil {
		return nil, fmt.Errorf("diff snapshots: %w", err)
	}

	appendCounts := map[string]int{}
	out := make([]FieldChange, 0, len(patch))
	for _, op := range patch {
		change, keep, mapErr := mapPatchOp(op, before, appendCounts)
		if mapErr != nil {
			return nil, mapErr
		}
		if keep {
			out = append(out, change)
		}
	}
	out = filterBookkeeping(out)
	slices.SortFunc(out, func(a, b FieldChange) int {
		return cmp.Compare(a.Path, b.Path)
	})
	return out, nil
}

func mapPatchOp(op jsondiff.Operation, before map[string]any, appendCounts map[string]int) (FieldChange, bool, error) {
	switch op.Type {
	case jsondiff.OperationAdd:
		return FieldChange{
			Path:  rewriteAppendPath(op.Path, before, appendCounts),
			Op:    FieldAdd,
			After: copyJSONValue(op.Value),
		}, true, nil
	case jsondiff.OperationRemove:
		return FieldChange{
			Path:   op.Path,
			Op:     FieldRemove,
			Before: copyJSONValue(op.OldValue),
		}, true, nil
	case jsondiff.OperationReplace:
		if numbersEquivalent(op.OldValue, op.Value) {
			return FieldChange{}, false, nil
		}
		return FieldChange{
			Path:   op.Path,
			Op:     FieldReplace,
			Before: copyJSONValue(op.OldValue),
			After:  copyJSONValue(op.Value),
		}, true, nil
	default:
		return FieldChange{}, false, fmt.Errorf("unsupported json patch operation %s at %s", op.Type, op.Path)
	}
}

func encodeSnapshot(m map[string]any) ([]byte, error) {
	if m == nil {
		m = map[string]any{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func unmarshalUseNumber(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

func numbersEquivalent(a, b any) bool {
	left, leftOK := asRat(a)
	right, rightOK := asRat(b)
	return leftOK && rightOK && left.Cmp(right) == 0
}

// asRat converts a jsondiff patch operand to an exact rational.
// DiffFields encodes snapshots as JSON and compares them with
// UseNumber, so operands arrive as [json.Number], not native Go
// integer types.
func asRat(v any) (*big.Rat, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(string(n))
	return r, ok
}

func rewriteAppendPath(path string, before map[string]any, appendCounts map[string]int) string {
	if !strings.HasSuffix(path, jsonPointerAppend) {
		return path
	}
	parent := strings.TrimSuffix(path, jsonPointerAppend)
	idx := arrayLenAt(before, parent) + appendCounts[parent]
	appendCounts[parent]++
	return parent + "/" + strconv.Itoa(idx)
}

func arrayLenAt(obj map[string]any, pointer string) int {
	v, ok := lookupPointer(obj, pointer)
	if !ok {
		return 0
	}
	switch a := v.(type) {
	case []any:
		return len(a)
	case []string:
		return len(a)
	default:
		return 0
	}
}

func lookupPointer(obj map[string]any, pointer string) (any, bool) {
	if pointer == "" {
		return obj, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	var cur any = obj
	for token := range strings.SplitSeq(pointer[1:], "/") {
		token = unescapePointerToken(token)
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
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

func unescapePointerToken(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
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

func copyFields(fields []FieldChange) []FieldChange {
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

func copyOrigin(o ResourceOrigin) ResourceOrigin {
	if o.Helm != nil {
		h := *o.Helm
		o.Helm = &h
	}
	return o
}

func copyApply(a ApplySemantics) ApplySemantics {
	if a.Write != nil {
		w := *a.Write
		a.Write = &w
	}
	if a.Delete != nil {
		d := *a.Delete
		a.Delete = &d
	}
	return a
}

func copyDiagnostics(in []Diagnostic) []Diagnostic {
	out := slices.Clone(in)
	for i := range out {
		if out[i].Resource != nil {
			r := *out[i].Resource
			out[i].Resource = &r
		}
	}
	return out
}

func copyTasks(in []TaskPlan) []TaskPlan {
	if in == nil {
		return []TaskPlan{}
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].Definitions = copyDefinitions(out[i].Definitions)
		out[i].Resources = slices.Clone(out[i].Resources)
		if out[i].Resources == nil {
			out[i].Resources = []ResourceRef{}
		}
	}
	return out
}

func copyDefinitions(in []HookDefinition) []HookDefinition {
	if in == nil {
		return []HookDefinition{}
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].Before = copySnapshot(out[i].Before)
		out[i].After = copySnapshot(out[i].After)
		out[i].Fields = copyFields(out[i].Fields)
		if out[i].Fields == nil {
			out[i].Fields = []FieldChange{}
		}
	}
	return out
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
		if IsBookkeepingPath(f.Path) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// bookkeepingPointers are RFC 6901 JSON Pointer prefixes that are not
// semantic field changes. [DiffFields] omits them. Human YAML strips the
// same keys from render copies. Snapshots keep the original fields.
var bookkeepingPointers = []string{
	"/status",
	"/metadata/resourceVersion",
	"/metadata/uid",
	"/metadata/generation",
	"/metadata/creationTimestamp",
	"/metadata/managedFields",
}

// BookkeepingPointers returns the JSON Pointer prefixes omitted from
// [FieldChange] results and from human YAML. The returned slice is a copy.
func BookkeepingPointers() []string {
	return slices.Clone(bookkeepingPointers)
}

// IsBookkeepingPath reports whether path is a [BookkeepingPointers]
// prefix or a descendant of one.
func IsBookkeepingPath(path string) bool {
	for _, p := range bookkeepingPointers {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}
