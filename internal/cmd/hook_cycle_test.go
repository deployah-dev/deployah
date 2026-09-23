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

package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/testing/testpath"
)

func TestStrictCommands_HookCycleIsHardError(t *testing.T) {
	t.Parallel()

	dir := testpath.Dir(t, "resolve", "testdata", "hook-cycle-platform")

	tests := []struct {
		name string
		args []string
	}{
		{name: "validate", args: []string{"validate", "staging"}},
		{name: "run", args: []string{"run", "migrate", "staging", "--yes"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, nabattest.WithDir(dir))
			require.Error(t, err)
			assert.ErrorContains(t, err, "cycle")
			assert.Contains(t, errOut.String(), "cycle")
		})
	}
}
