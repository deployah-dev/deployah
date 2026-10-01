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
	"nabat.dev/nabat"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/plan/view"
)

// roleTokens is the Nabat color for each Human plan role.
var roleTokens = map[view.Role]theme.Token{
	view.RoleTitle:        theme.TextTitle,
	view.RolePrimary:      theme.TextPrimary,
	view.RoleDiffAdded:    theme.StatusSuccess,
	view.RoleDiffRemoved:  theme.StatusError,
	view.RoleDiffModified: theme.StatusWarning,
	view.RoleDiffContext:  theme.TextMuted,
}

// nabatStyler paints Human plan lines with the command theme.
type nabatStyler struct{ c *nabat.Context }

// Style implements [view.Styler].
func (s nabatStyler) Style(role view.Role, line string) string {
	tok, ok := roleTokens[role]
	if !ok {
		return line // unknown roles stay plain
	}
	// Nabat applies color, including NO_COLOR and a non-TTY.
	return s.c.Render(tok, line)
}
