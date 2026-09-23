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

package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/testing/testpath"
)

// TestValidate_Environment covers the validate command across manifest-only and
// environment resolution paths. Each case runs the whole app against a static
// fixture under testdata; validate only reads the fixture, so the shared
// directories are safe to reuse under t.Parallel.
func TestValidate_Environment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		fixture      string
		args         []string
		wantErr      bool
		wantContains []string // substrings required in the error when wantErr is true
		wantSuccess  []string // substrings required on stderr when the command succeeds
	}{
		{
			name:        "valid runtime env",
			fixture:     "runtime-env-valid",
			args:        []string{"validate", "dev"},
			wantSuccess: []string{"Manifest and environment valid"},
		},
		{
			name:         "invalid dotenv key",
			fixture:      "runtime-env-bad-key",
			args:         []string{"validate", "dev"},
			wantErr:      true,
			wantContains: []string{"FOO-BAR", spec.ErrCodeInvalidEnvKey},
		},
		{
			name:         "missing explicit envFile",
			fixture:      "runtime-env-missing",
			args:         []string{"validate", "dev"},
			wantErr:      true,
			wantContains: []string{"runtime.env", spec.ErrCodeEnvFileNotFound},
		},
		{
			name:         "task profiles require platform",
			fixture:      "task-profiles",
			args:         []string{"validate", "dev"},
			wantErr:      true,
			wantContains: []string{"resolution failed (PLATFORM_NOT_FOUND)"},
		},
		{
			name:         "platform-owned feature still fails",
			fixture:      "expose-domain",
			args:         []string{"validate", "dev"},
			wantErr:      true,
			wantContains: []string{spec.ErrCodePlatformNotFound, "expose"},
		},
		{
			// Same fixture as the missing-envFile case: without an environment,
			// validate checks only the manifest and must not discover runtime
			// files, so the absent runtime.env is fine.
			name:        "manifest-only does not discover runtime files",
			fixture:     "runtime-env-missing",
			args:        []string{"validate"},
			wantSuccess: []string{"Manifest schema valid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, nabattest.WithDir(testpath.Dir(t, "testdata", tt.fixture)))

			if !tt.wantErr {
				require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
				for _, want := range tt.wantSuccess {
					assert.Contains(t, errOut.String(), want)
				}
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantContains {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}
