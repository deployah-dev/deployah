// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/spec"
)

const resolveHookCycleSpec = `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
    env:
      LOG_LEVEL: debug
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [seed]
    command: [migrate]
  seed:
    from: api
    "on": preDeploy
    after: [migrate]
    command: [seed]
environments:
  staging: {}
`

func TestResolve_HookCycleIsWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(resolveHookCycleSpec), 0o600))

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "staging"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	got := out.String()
	assert.Contains(t, got, "hook ordering")
	assert.Contains(t, strings.ToLower(got), "cycle")
	assert.Contains(t, got, "LOG_LEVEL: debug")
}

func TestResolve_HookCycleJSONWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(resolveHookCycleSpec), 0o600))

	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.RunParallel(t, app, []string{"resolve", "staging", "--output", "json"}, nabattest.WithDir(dir))
	require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

	var got struct {
		Components map[string]struct {
			ExplicitValues map[string]string `json:"explicit_values"`
		} `json:"components"`
		Warnings []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), out.String())

	require.NotEmpty(t, got.Warnings)
	joined := strings.Join(got.Warnings, "\n")
	assert.Contains(t, joined, "hook ordering")
	assert.Contains(t, strings.ToLower(joined), "cycle")
	require.Contains(t, got.Components, "api")
	assert.Equal(t, "debug", got.Components["api"].ExplicitValues["LOG_LEVEL"])
}

func TestResolve_HookCycleRemainsHardError(t *testing.T) {
	t.Parallel()

	appSpec := &spec.Spec{
		APIVersion: spec.CurrentManifestVersion,
		Project:    "shop",
		Components: map[string]spec.Component{
			"api": {
				Image: "busybox",
				Env:   spec.StringMap{"LOG_LEVEL": "debug"},
			},
		},
		Tasks: map[string]spec.Task{
			"migrate": {
				From:    "api",
				On:      spec.TaskOnPreDeploy,
				After:   []string{"seed"},
				Command: []string{"migrate"},
			},
			"seed": {
				From:    "api",
				On:      spec.TaskOnPreDeploy,
				After:   []string{"migrate"},
				Command: []string{"seed"},
			},
		},
		Environments: map[string]spec.Environment{
			"staging": {},
		},
	}

	_, _, err := spec.Resolve(appSpec, nil, spec.NormalizeEnv("staging"), spec.SubstitutionReport{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "cycle")
}
