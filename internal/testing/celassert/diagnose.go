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

// comparisonFns are CEL's built-in binary comparison overloads.
var comparisonFns = map[string]struct{}{
	"_==_": {},
	"_!=_": {},
	"_<_":  {},
	"_<=_": {},
	"_>_":  {},
	"_>=_": {},
}

// Diagnose explains a false comparison when the actual value is known.
func (p *Program) Diagnose(ctx context.Context, vars map[string]any) string {
	// A field compared with a literal includes the observed value.
	// Anything else returns an empty string. Eval itself is unchanged.
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
	if _, ok := comparisonFns[call.FunctionName()]; !ok || call.IsMemberFunction() {
		return ""
	}
	args := call.Args()
	if len(args) != 2 {
		return ""
	}
	path, pathOK := selectorPath(args[0])
	lit, litOK := literalText(args[1])
	if pathOK && litOK {
		actual, ok := state.Value(args[0].ID())
		if !ok {
			return ""
		}
		return fmt.Sprintf("expected: %s = %s\nactual: %s = %s", path, lit, path, formatCEL(actual))
	}
	return ""
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
