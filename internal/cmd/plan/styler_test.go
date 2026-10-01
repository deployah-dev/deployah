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

package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/plan/view"
)

func TestRoleTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		role view.Role
		tok  theme.Token
	}{
		{name: "title", role: view.RoleTitle, tok: theme.TextTitle},
		{name: "primary", role: view.RolePrimary, tok: theme.TextPrimary},
		{name: "added", role: view.RoleDiffAdded, tok: theme.StatusSuccess},
		{name: "removed", role: view.RoleDiffRemoved, tok: theme.StatusError},
		{name: "modified", role: view.RoleDiffModified, tok: theme.StatusWarning},
		{name: "context", role: view.RoleDiffContext, tok: theme.TextMuted},
	}
	assert.Len(t, roleTokens, len(tests))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.tok, roleTokens[tc.role])
		})
	}
}

func TestNabatStyler_UnknownRole(t *testing.T) {
	t.Parallel()
	got := (nabatStyler{}).Style(0, "plain")
	assert.Equal(t, "plain", got)
}
