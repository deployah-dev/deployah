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
	"github.com/stretchr/testify/require"
)

func TestChartContainsTargetNamespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		manifest  string
		namespace string
		want      bool
		wantErr   string
	}{
		{
			name:      "bare target namespace",
			manifest:  "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: prod\n",
			namespace: "prod",
			want:      true,
		},
		{
			name:      "list item target namespace",
			manifest:  "apiVersion: v1\nkind: List\nitems:\n- apiVersion: v1\n  kind: Namespace\n  metadata:\n    name: prod\n",
			namespace: "prod",
			want:      true,
		},
		{
			name:      "other namespace",
			manifest:  "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: extra\n",
			namespace: "prod",
		},
		{
			name:      "invalid yaml",
			manifest:  "not: [valid",
			namespace: "prod",
			wantErr:   "parse manifest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := chartContainsTargetNamespace(tt.manifest, tt.namespace)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
