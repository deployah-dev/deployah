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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnose(t *testing.T) {
	t.Parallel()
	doc := map[string]any{
		"helmAction": "upgrade",
		"summary":    map[string]any{"total": int64(0), "create": int64(0)},
	}
	vars := map[string]any{"plan": doc}
	tests := []struct {
		name      string
		expr      string
		want      string
		evalFalse bool
	}{
		{
			name:      "equal",
			expr:      "plan.summary.total == 1",
			want:      "expected: plan.summary.total == 1\nactual: plan.summary.total = 0",
			evalFalse: true,
		},
		{
			name:      "not equal",
			expr:      "plan.summary.total != 0",
			want:      "expected: plan.summary.total != 0\nactual: plan.summary.total = 0",
			evalFalse: true,
		},
		{
			name:      "greater than",
			expr:      "plan.summary.create > 0",
			want:      "expected: plan.summary.create > 0\nactual: plan.summary.create = 0",
			evalFalse: true,
		},
		{
			name:      "greater or equal",
			expr:      "plan.summary.create >= 1",
			want:      "expected: plan.summary.create >= 1\nactual: plan.summary.create = 0",
			evalFalse: true,
		},
		{
			name:      "less than",
			expr:      "plan.summary.total < 0",
			want:      "expected: plan.summary.total < 0\nactual: plan.summary.total = 0",
			evalFalse: true,
		},
		{
			name:      "less or equal",
			expr:      "plan.summary.total <= -1",
			want:      "expected: plan.summary.total <= -1\nactual: plan.summary.total = 0",
			evalFalse: true,
		},
		{
			name:      "string",
			expr:      `plan.helmAction == "install"`,
			want:      "expected: plan.helmAction == \"install\"\nactual: plan.helmAction = \"upgrade\"",
			evalFalse: true,
		},
		{
			name:      "nested field",
			expr:      "plan.summary.total == 2",
			want:      "expected: plan.summary.total == 2\nactual: plan.summary.total = 0",
			evalFalse: true,
		},
		{
			name:      "reversed literal",
			expr:      "0 < plan.summary.create",
			want:      "expected: 0 < plan.summary.create\nactual: plan.summary.create = 0",
			evalFalse: true,
		},
		{
			name:      "reversed equality",
			expr:      "1 == plan.summary.total",
			want:      "expected: 1 == plan.summary.total\nactual: plan.summary.total = 0",
			evalFalse: true,
		},
		{
			name:      "reversed string",
			expr:      `"install" == plan.helmAction`,
			want:      "expected: \"install\" == plan.helmAction\nactual: plan.helmAction = \"upgrade\"",
			evalFalse: true,
		},
		{
			name:      "logical expression",
			expr:      "plan.summary.total == 1 || plan.summary.total == 2",
			evalFalse: true,
		},
		{
			name:      "call is not a field comparison",
			expr:      "plan.summary.total == size(plan.summary)",
			evalFalse: true,
		},
		{
			name: "missing field",
			expr: "plan.missing == 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Compile(tt.expr, WithVariable("plan"))
			require.NoError(t, err)
			evalErr := prog.Eval(t.Context(), vars)
			require.Error(t, evalErr)
			assert.Equal(t, tt.evalFalse, errors.Is(evalErr, ErrFalse))
			assert.Equal(t, tt.want, prog.Diagnose(t.Context(), vars))
			again := prog.Eval(t.Context(), vars)
			require.Error(t, again)
			assert.Equal(t, tt.evalFalse, errors.Is(again, ErrFalse))
		})
	}
}
