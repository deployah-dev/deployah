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

package celassert

import (
	"context"
	"fmt"
	"strings"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"
)

// comparisonOps maps a CEL overload to the operator written in the source.
// A literal on the left is supported. Other shapes stay empty.
var comparisonOps = map[string]string{
	"_==_": "==",
	"_!=_": "!=",
	"_<_":  "<",
	"_<=_": "<=",
	"_>_":  ">",
	"_>=_": ">=",
}

// Diagnose explains a false field comparison when the actual value is known.
// The source operator is kept. Other expressions return an empty string.
func (p *Program) Diagnose(ctx context.Context, vars map[string]any) string {
	// A field compared with a literal, in either order, includes the observed
	// value. Eval itself is unchanged.
	if p == nil || p.tracked == nil || p.root == nil || ctx == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	prepared, err := prepare(vars)
	if err != nil {
		return ""
	}
	_, details, err := p.tracked.ContextEval(ctx, prepared)
	if err != nil || details == nil {
		return ""
	}
	return comparisonDetail(p.root, details.State())
}

func comparisonDetail(root ast.Expr, state interpreter.EvalState) string {
	if root == nil || root.Kind() != ast.CallKind || state == nil {
		return ""
	}
	call := root.AsCall()
	op, ok := comparisonOps[call.FunctionName()]
	if !ok || call.IsMemberFunction() {
		return ""
	}
	args := call.Args()
	if len(args) != 2 {
		return ""
	}
	if text := fieldLiteral(op, args[0], args[1], true, state); text != "" {
		return text
	}
	return fieldLiteral(op, args[1], args[0], false, state)
}

func fieldLiteral(op string, field, lit ast.Expr, fieldOnLeft bool, state interpreter.EvalState) string {
	path, pathOK := selectorPath(field)
	text, litOK := literalText(lit)
	if !pathOK || !litOK {
		return ""
	}
	actual, ok := state.Value(field.ID())
	if !ok {
		return ""
	}
	expected := path + " " + op + " " + text
	if !fieldOnLeft {
		expected = text + " " + op + " " + path
	}
	return fmt.Sprintf("expected: %s\nactual: %s = %s", expected, path, formatCEL(actual))
}

func selectorPath(e ast.Expr) (string, bool) {
	switch e.Kind() {
	case ast.IdentKind:
		name := e.AsIdent()
		if name == "" {
			return "", false
		}
		return name, true
	case ast.SelectKind:
		sel := e.AsSelect()
		if sel.IsTestOnly() {
			return "", false
		}
		base, ok := selectorPath(sel.Operand())
		if !ok || sel.FieldName() == "" {
			return "", false
		}
		return base + "." + sel.FieldName(), true
	default:
		return "", false
	}
}

func literalText(e ast.Expr) (string, bool) {
	if e.Kind() != ast.LiteralKind {
		return "", false
	}
	val := e.AsLiteral()
	if val == nil {
		return "", false
	}
	return formatAny(val.Value()), true
}

func formatCEL(val ref.Val) string {
	if val == nil {
		return "null"
	}
	return formatAny(val.Value())
}

func formatAny(v any) string {
	switch n := v.(type) {
	case string:
		return fmt.Sprintf("%q", n)
	case nil:
		return "null"
	default:
		return strings.TrimSpace(fmt.Sprint(n))
	}
}
