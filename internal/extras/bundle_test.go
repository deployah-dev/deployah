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
	one := []extras.RawFile{{Path: "/abs/.deployah/crds/foo.yaml"}}
	two := []extras.RawFile{
		{Path: ".deployah/crds/foo.yaml"},
		{Path: "bar.yml"},
	}
	tests := []struct {
		name        string
		files       []extras.RawFile
		upgrade     bool
		skipInstall bool
		want        string
	}{
		{name: "no files", want: ""},
		{
			name:  "install lists files to process",
			files: two,
			want:  "CRD files to process on install:\n  + .deployah/crds/foo.yaml\n  + .deployah/crds/bar.yml",
		},
		{
			name:    "upgrade lists files as not processed",
			files:   one,
			upgrade: true,
			want:    "CRD files not processed on upgrade:\n  .deployah/crds/foo.yaml",
		},
		{
			name:    "newly added file on upgrade is not processed",
			files:   []extras.RawFile{{Path: "existing.yaml"}, {Path: "new.yaml"}},
			upgrade: true,
			want:    "CRD files not processed on upgrade:\n  .deployah/crds/existing.yaml\n  .deployah/crds/new.yaml",
		},
		{
			name:        "skip lists files kept in the chart",
			files:       two,
			skipInstall: true,
			want:        "CRD files in chart (install-time processing disabled):\n  .deployah/crds/foo.yaml\n  .deployah/crds/bar.yml",
		},
		{
			name:        "upgrade wins over skip",
			files:       one,
			upgrade:     true,
			skipInstall: true,
			want:        "CRD files not processed on upgrade:\n  .deployah/crds/foo.yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, extras.CRDLifecycleNote(tc.files, tc.upgrade, tc.skipInstall))
		})
	}
}
