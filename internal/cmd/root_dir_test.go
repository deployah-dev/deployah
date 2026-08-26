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
)

const specWithImageVar = `apiVersion: v1-alpha.5
project: withdir
components:
  web:
    image: ${IMAGE}
    port: 80
    environments: [dev]
environments:
  dev:
    envFile: .env.dev
`

const specLiteralImage = `apiVersion: v1-alpha.5
project: withdir
components:
  web:
    image: nginx:latest
    port: 80
    environments: [dev]
environments:
  dev: {}
`

func writeWithDirFixture(t *testing.T, specFile, spec, envBody string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, specFile), []byte(spec), 0o600))
	if envBody != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".env.dev"), []byte(envBody), 0o600))
	}
	return dir
}

// TestWithDirResolvesSpecAndEnvFile checks that nabattest.WithDir and
// c.Abs resolve --spec and .env files against the virtual directory
// rather than the process working directory. The test never calls [os.Chdir].
func TestWithDirResolvesSpecAndEnvFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		specFile string
		envBody  string
		args     []string
	}{
		{
			name:     "env file relative to virtual dir",
			spec:     specWithImageVar,
			specFile: "deployah.yaml",
			envBody:  "DPY_VAR_IMAGE=nginx:1.27\n",
			args:     []string{"plan", "dev", "--offline"},
		},
		{
			name:     "explicit --spec is Abs against virtual dir",
			spec:     specWithImageVar,
			specFile: "app.yaml",
			envBody:  "DPY_VAR_IMAGE=nginx:1.27\n",
			args:     []string{"plan", "dev", "--offline", "--spec", "app.yaml"},
		},
		{
			name:     "literal image without env file",
			spec:     specLiteralImage,
			specFile: "deployah.yaml",
			args:     []string{"plan", "dev", "--offline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := writeWithDirFixture(t, tt.specFile, tt.spec, tt.envBody)
			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, nabattest.WithDir(dir))
			require.NoErrorf(t, err, "plan --offline under WithDir\nstderr:\n%s", errOut.String())
		})
	}
}

func TestWithDirResolvesSpecAndEnvFile_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		spec     string
		specFile string
		args     []string
		wantErr  string
	}{
		{
			name:     "missing env file fails substitution",
			spec:     specWithImageVar,
			specFile: "deployah.yaml",
			args:     []string{"plan", "dev", "--offline"},
			wantErr:  "environment file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := writeWithDirFixture(t, tt.specFile, tt.spec, "")
			appIO, _, _, _ := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, tt.args, nabattest.WithDir(dir))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
