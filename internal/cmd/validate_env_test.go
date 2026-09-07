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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/spec"
)

func writeValidateSpec(t *testing.T, dir, specBody string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(specBody), 0o600))
}

func TestValidateEnvironment_RuntimeEnvWithoutPlatform(t *testing.T) {
	t.Parallel()

	const specBody = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
environments:
  dev:
    envFile: runtime.env
`

	tests := []struct {
		name     string
		envBody  string
		envFile  string
		args     []string
		wantErr  string
		wantCode string
	}{
		{
			name:    "valid runtime env",
			envBody: "REGION=eu\n",
			envFile: "runtime.env",
			args:    []string{"validate", "dev"},
		},
		{
			name:     "invalid dotenv key",
			envBody:  "FOO-BAR=1\n",
			envFile:  "runtime.env",
			args:     []string{"validate", "dev"},
			wantErr:  "FOO-BAR",
			wantCode: spec.ErrCodeInvalidEnvKey,
		},
		{
			name:     "missing explicit envFile",
			args:     []string{"validate", "dev"},
			wantErr:  "runtime.env",
			wantCode: spec.ErrCodeEnvFileNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeValidateSpec(t, dir, specBody)
			if tt.envFile != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tt.envFile), []byte(tt.envBody), 0o600))
			}

			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, nabattest.WithDir(dir))
			if tt.wantErr == "" {
				require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.wantCode)
		})
	}
}

func TestValidateEnvironment_PlatformOwnedFeatureStillFails(t *testing.T) {
	t.Parallel()

	const specBody = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
    expose:
      domain: public
environments:
  dev: {}
`
	dir := t.TempDir()
	writeValidateSpec(t, dir, specBody)

	appIO, _, _, _ := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"validate", "dev"}, nabattest.WithDir(dir))
	require.Error(t, err)
	assert.ErrorContains(t, err, spec.ErrCodePlatformNotFound)
	assert.ErrorContains(t, err, "expose")
}

func TestValidate_ManifestOnlyDoesNotDiscoverRuntimeFiles(t *testing.T) {
	t.Parallel()

	const specBody = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
environments:
  dev:
    envFile: missing.env
`
	dir := t.TempDir()
	writeValidateSpec(t, dir, specBody)

	appIO, _, _, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"validate"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
}
