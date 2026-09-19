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

package extras_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"deployah.dev/deployah/internal/extras"
)

func TestCRDLifecycleNote(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		n           int
		upgrade     bool
		skipInstall bool
		want        string
	}{
		{name: "zero files", n: 0, want: ""},
		{name: "negative files", n: -1, want: ""},
		{
			name: "install",
			n:    2,
			want: "CRD files: 2 from .deployah/crds/ (Helm processes chart CRDs on install)",
		},
		{
			name:    "upgrade",
			n:       1,
			upgrade: true,
			want:    "CRD files: 1 from .deployah/crds/ (Helm does not install or update chart CRDs on upgrade, including newly added files)",
		},
		{
			name:        "skip on install",
			n:           3,
			skipInstall: true,
			want:        "CRD files: 3 from .deployah/crds/ (install-time CRD processing disabled; files stay in the chart)",
		},
		{
			name:        "upgrade wins over skip",
			n:           1,
			upgrade:     true,
			skipInstall: true,
			want:        "CRD files: 1 from .deployah/crds/ (Helm does not install or update chart CRDs on upgrade, including newly added files)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, extras.CRDLifecycleNote(tc.n, tc.upgrade, tc.skipInstall))
		})
	}
}
