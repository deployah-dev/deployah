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

const resolveSubstSpec = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
    env:
      LOG_LEVEL: ${LEVEL}
    envFile: ${ENV_FILE}
environments:
  production:
    variables:
      LEVEL: debug
      ENV_FILE: runtime.env
`

func TestResolve_UsesSubstitutedSpec(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(resolveSubstSpec), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.env"), []byte("REGION=eu\nDPY_VAR_LEVEL=from-dotenv\n"), 0o600))

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "production"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
	got := out.String()
	assert.Contains(t, got, "LOG_LEVEL: debug")
	assert.NotContains(t, got, "LOG_LEVEL: ${LEVEL}")
	assert.Contains(t, got, "REGION: eu")
	assert.Contains(t, got, "DPY_VAR_LEVEL: from-dotenv")
}

func TestResolve_DotenvDPYVarDoesNotSubstitute(t *testing.T) {
	t.Parallel()

	const spec = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
    env:
      LOG_LEVEL: ${LEVEL}
environments:
  production:
    envFile: runtime.env
    variables:
      LEVEL: from-variables
`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(spec), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.env"), []byte("DPY_VAR_LEVEL=from-dotenv\n"), 0o600))

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "production"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
	got := out.String()
	assert.Contains(t, got, "LOG_LEVEL: from-variables")
	assert.NotContains(t, got, "LOG_LEVEL: from-dotenv")
	assert.Contains(t, got, "DPY_VAR_LEVEL: from-dotenv")
}

func TestResolve_ProcessDPYVarSubstitutes(t *testing.T) {
	t.Setenv("DPY_VAR_LEVEL", "from-process")

	const spec = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
    env:
      LOG_LEVEL: ${LEVEL}
environments:
  production:
    variables:
      LEVEL: from-variables
`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(spec), 0o600))

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.Run(t, app, []string{"resolve", "production"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())
	got := out.String()
	assert.Contains(t, got, "LOG_LEVEL: from-process")
	assert.NotContains(t, got, "LOG_LEVEL: from-variables")
}
