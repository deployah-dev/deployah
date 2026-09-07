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
// See the License for the specific language governing the License.

package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
)

const resolveJSONSpec = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: nginx:1.27
    port: 80
    env:
      LOG_LEVEL: debug
    envFile: api.env
tasks:
  migrate:
    from: api
    "on": preDeploy
    command: [migrate]
    env:
      MIGRATION_MODE: safe
environments:
  staging: {}
`

func TestResolve_JSONContract(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(resolveJSONSpec), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "api.env"), []byte("REGION=eu\n"), 0o600))

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "staging", "--output", "json"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	var got struct {
		Environment string `json:"environment"`
		Components  map[string]struct {
			FileValues     map[string]string `json:"file_values"`
			ExplicitValues map[string]string `json:"explicit_values"`
		} `json:"components"`
		Tasks map[string]struct {
			FileValues     map[string]string `json:"file_values"`
			ExplicitValues map[string]string `json:"explicit_values"`
		} `json:"tasks"`
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), out.String())

	assert.Equal(t, "staging", got.Environment)
	require.Contains(t, got.Components, "api")
	assert.Equal(t, map[string]string{"REGION": "eu"}, got.Components["api"].FileValues)
	assert.Equal(t, map[string]string{"LOG_LEVEL": "debug"}, got.Components["api"].ExplicitValues)

	require.Contains(t, got.Tasks, "migrate")
	assert.Equal(t, map[string]string{"REGION": "eu"}, got.Tasks["migrate"].FileValues)
	assert.Equal(t, map[string]string{
		"LOG_LEVEL":      "debug",
		"MIGRATION_MODE": "safe",
	}, got.Tasks["migrate"].ExplicitValues)

	require.NotEmpty(t, got.Warnings)
	assert.Contains(t, got.Warnings[0], "platform file not found")
}
