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

package view_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
)

func TestPrepareRender_InvalidEnums(t *testing.T) {
	t.Parallel()
	validChange := func() semantic.ResourceChange {
		return semantic.ResourceChange{
			Resource: ref("ConfigMap", "app"),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
		}
	}
	tests := []struct {
		name    string
		plan    semantic.Plan
		wantErr string
	}{
		{
			name: "invalid action",
			plan: semantic.Plan{
				HelmAction: semantic.HelmUpgrade,
				Changes:    []semantic.ResourceChange{{}},
			},
			wantErr: "invalid change 0 action",
		},
		{
			name: "invalid field op",
			plan: func() semantic.Plan {
				c := validChange()
				c.Fields = []semantic.FieldChange{{Path: "/data/key"}}
				return semantic.Plan{HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{c}}
			}(),
			wantErr: "invalid change 0 field 0 op",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errH := view.WriteHuman(&bytes.Buffer{}, tt.plan, view.Options{})
			require.Error(t, errH)
			assert.ErrorContains(t, errH, tt.wantErr)
			errJ := view.WriteJSON(&bytes.Buffer{}, tt.plan, view.Options{})
			require.Error(t, errJ)
			assert.ErrorContains(t, errJ, tt.wantErr)
		})
	}
}
