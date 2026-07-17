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

package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// TestHooksChanged covers the named case.
func TestHooksChanged(t *testing.T) {
	t.Parallel()
	preInstall := &v1.Hook{Name: "templates/hooks/pre-install.yaml", Manifest: "kind: Job\nspec: v1"}

	tests := []struct {
		name     string
		previous []*v1.Hook
		current  []*v1.Hook
		want     bool
	}{
		{name: "both empty", previous: nil, current: nil, want: false},
		{name: "identical", previous: []*v1.Hook{preInstall}, current: []*v1.Hook{preInstall}, want: false},
		{
			name:     "content changed",
			previous: []*v1.Hook{preInstall},
			current:  []*v1.Hook{{Name: preInstall.Name, Manifest: "kind: Job\nspec: v2"}},
			want:     true,
		},
		{name: "hook added", previous: nil, current: []*v1.Hook{preInstall}, want: true},
		{name: "hook removed", previous: []*v1.Hook{preInstall}, current: nil, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, HooksChanged(tt.previous, tt.current))
		})
	}
}
