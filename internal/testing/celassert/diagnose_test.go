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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnose(t *testing.T) {
	t.Parallel()
	doc := map[string]any{"summary": map[string]any{"total": int64(0)}}
	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "field compared with a literal",
			expr: "plan.summary.total == 1",
			want: "expected: plan.summary.total = 1\nactual: plan.summary.total = 0",
		},
		{
			name: "logical expression has no invented pair",
			expr: "plan.summary.total == 1 || plan.summary.total == 2",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Compile(tt.expr, WithVariable("plan"))
			require.NoError(t, err)
			require.ErrorIs(t, prog.Eval(t.Context(), map[string]any{"plan": doc}), ErrFalse)
			assert.Equal(t, tt.want, prog.Diagnose(t.Context(), map[string]any{"plan": doc}))
		})
	}
}
