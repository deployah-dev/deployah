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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"cel.dev/cel-go/cel"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// DefaultCostLimit is the runtime CEL cost bound for one evaluation.
	// It counts estimated CEL operations, not time.
	DefaultCostLimit uint64 = 1_000_000

	// DefaultInterruptCheckFrequency is how many comprehension steps run
	// between context checks.
	DefaultInterruptCheckFrequency uint = 100
)

var (
	// ErrFalse means the expression evaluated to false.
	ErrFalse = errors.New("expression evaluated to false")

	// ErrNotBool means the expression result is not a Boolean.
	ErrNotBool = errors.New("expression result is not bool")

	// ErrIntegerRange means a JSON integer does not fit in int64.
	ErrIntegerRange = errors.New("integer outside int64")

	errNilContext      = errors.New("context is nil")
	errNilUnstructured = errors.New("unstructured object is nil")
)

// Program is a compiled Boolean CEL expression.
//
// Build one with [Compile]. The zero value is not usable.
// [Program.Eval] is safe for concurrent callers.
type Program struct {
	expression string
	prog       cel.Program
}

// Option configures Compile.
type Option func(*config)

type config struct {
	vars []string
	cost uint64
}

// WithVariable declares a dynamic root variable.
//
// An expression that uses any other root fails at compile time. Duplicate
// names are rejected.
//
// Example:
//
//	Compile(`object.kind == "Pod"`, WithVariable("object"))
func WithVariable(name string) Option {
	return func(cfg *config) {
		cfg.vars = append(cfg.vars, name)
	}
}

// WithCostLimit sets the runtime cost limit for one evaluation.
//
// The limit must be greater than zero. It replaces [DefaultCostLimit].
//
// Example:
//
//	Compile(`items.all(i, true)`, WithCostLimit(1), WithVariable("items"))
func WithCostLimit(limit uint64) Option {
	return func(cfg *config) {
		cfg.cost = limit
	}
}

// Compile type-checks expression and returns a program for repeated
// evaluation.
//
// Roots come from [WithVariable]. The cost limit defaults to
// [DefaultCostLimit]. A concrete non-Boolean result is [ErrNotBool].
//
// Example:
//
//	p, err := Compile(
//		`object.status.readyReplicas == 1`,
//		WithVariable("object"),
//	)
func Compile(expression string, opts ...Option) (*Program, error) {
	cfg := config{cost: DefaultCostLimit}
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(expression); err != nil {
		return nil, err
	}

	envOpts := make([]cel.EnvOption, 0, 1+len(cfg.vars))
	envOpts = append(envOpts, cel.OptionalTypes())
	for _, name := range cfg.vars {
		envOpts = append(envOpts, cel.Variable(name, cel.DynType))
	}
	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		return nil, fmt.Errorf("compiling expression: %w", err)
	}
	ast, iss := env.Compile(expression)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("compiling expression: %w", iss.Err())
	}
	out := ast.OutputType()
	// dyn is checked again in Eval. A field read has no static type.
	if !out.IsExactType(cel.BoolType) && !out.IsExactType(cel.DynType) {
		return nil, fmt.Errorf("compiling expression: got %s: %w", out.TypeName(), ErrNotBool)
	}
	prog, err := env.Program(ast,
		cel.CostLimit(cfg.cost),
		cel.InterruptCheckFrequency(DefaultInterruptCheckFrequency),
	)
	if err != nil {
		return nil, fmt.Errorf("compiling expression: %w", err)
	}
	return &Program{expression: expression, prog: prog}, nil
}

func (cfg config) validate(expression string) error {
	if strings.TrimSpace(expression) == "" {
		return errors.New("expression is empty")
	}
	if cfg.cost == 0 {
		return errors.New("cost limit must be greater than zero")
	}
	seen := make(map[string]struct{}, len(cfg.vars))
	for _, name := range cfg.vars {
		if name == "" {
			return errors.New("variable name is empty")
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate variable %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// Eval reports whether the compiled expression is true for vars.
//
// Nil means true. [ErrFalse] means false. [ErrNotBool] means the result
// was not a Boolean. ctx must be non-nil.
//
// Example:
//
//	err = p.Eval(ctx, map[string]interface{}{
//		"object": map[string]interface{}{
//			"status": map[string]interface{}{
//				"readyReplicas": 1,
//			},
//		},
//	})
func (p *Program) Eval(ctx context.Context, vars map[string]interface{}) error {
	if ctx == nil {
		return fmt.Errorf("evaluating %s: %w", p.expression, errNilContext)
	}
	// An already done context fails here, before CEL, including for an
	// expression with no comprehension. [errors.Is] matches
	// [context.Canceled] and [context.DeadlineExceeded].
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("evaluating %s: %w", p.expression, err)
	}
	prepared, err := prepare(vars)
	if err != nil {
		return fmt.Errorf("evaluating %s: %w", p.expression, err)
	}
	val, _, err := p.prog.ContextEval(ctx, prepared)
	if err != nil {
		return fmt.Errorf("evaluating %s: %w", p.expression, err)
	}
	result, ok := val.Value().(bool)
	if !ok {
		return fmt.Errorf("evaluating %s: got %s: %w", p.expression, val.Type().TypeName(), ErrNotBool)
	}
	if !result {
		return fmt.Errorf("evaluating %s: %w", p.expression, ErrFalse)
	}
	return nil
}

func prepare(vars map[string]interface{}) (map[string]interface{}, error) {
	if len(vars) == 0 {
		return map[string]interface{}{}, nil
	}
	out := make(map[string]interface{}, len(vars))
	for name, value := range vars {
		normalized, err := normalize(value)
		if err != nil {
			return nil, err
		}
		numbered, err := normalizeNumbers(normalized)
		if err != nil {
			return nil, err
		}
		out[name] = numbered
	}
	return out, nil
}

func normalize(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case json.RawMessage:
		return decodeJSON(v)
	case unstructured.Unstructured:
		return unstructuredObject(v.Object)
	case *unstructured.Unstructured:
		if v == nil {
			return nil, errNilUnstructured
		}
		return unstructuredObject(v.Object)
	case unstructured.UnstructuredList:
		return unstructuredObjects(v.Items)
	case *unstructured.UnstructuredList:
		if v == nil {
			return nil, errNilUnstructured
		}
		return unstructuredObjects(v.Items)
	case []unstructured.Unstructured:
		return unstructuredObjects(v)
	case []*unstructured.Unstructured:
		out := make([]interface{}, 0, len(v))
		for i := range v {
			item, err := normalize(v[i])
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return value, nil
	}
}

func unstructuredObject(obj map[string]interface{}) (interface{}, error) {
	if obj == nil {
		return nil, errNilUnstructured
	}
	return obj, nil
}

func unstructuredObjects(items []unstructured.Unstructured) (interface{}, error) {
	out := make([]interface{}, 0, len(items))
	for i := range items {
		obj, err := unstructuredObject(items[i].Object)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, nil
}

func decodeJSON(raw json.RawMessage) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("decoding JSON input: %w", err)
	}
	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		// The extra value is not included in the error text.
		return nil, errors.New("decoding JSON input: trailing data")
	}
	return value, nil
}

func normalizeNumbers(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case json.Number:
		return normalizeNumber(v)
	case []json.Number:
		out := make([]json.Number, 0, len(v))
		for _, n := range v {
			nn, err := normalizeNumber(n)
			if err != nil {
				return nil, err
			}
			out = append(out, nn)
		}
		return out, nil
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, child := range v {
			nv, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case []map[string]interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, child := range v {
			nv, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			m, ok := nv.(map[string]interface{})
			if !ok {
				return nil, errors.New("normalizing numbers: expected a map")
			}
			out = append(out, m)
		}
		return out, nil
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, child := range v {
			nv, err := normalizeNumbers(child)
			if err != nil {
				return nil, err
			}
			out = append(out, nv)
		}
		return out, nil
	default:
		return value, nil
	}
}

// normalizeNumber keeps integers exact for CEL.
func normalizeNumber(n json.Number) (json.Number, error) {
	// A plain int64 needs no rewrite.
	if _, err := n.Int64(); err == nil {
		return n, nil
	}
	// Fractions stay as written. Integers that fit are rewritten as
	// decimals so CEL does not round them through float64.
	r, ok := new(big.Rat).SetString(string(n))
	if !ok || !r.IsInt() {
		return n, nil
	}
	if !r.Num().IsInt64() {
		return "", fmt.Errorf("json number %s: %w", n, ErrIntegerRange)
	}
	return json.Number(r.Num().String()), nil
}
