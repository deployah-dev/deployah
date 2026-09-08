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
	"encoding/json"
	"maps"
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

const resolveStagingPlatform = `apiVersion: platform/v1-alpha.3
environments:
  staging: {}
`

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

const resolveSelfCycleSpec = `apiVersion: v1-alpha.5
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
    after: [migrate]
    command: [migrate]
environments:
  staging: {}
`

func writeResolveSpec(t *testing.T, specYAML string, withPlatform bool) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deployah.yaml"), []byte(specYAML), 0o600))
	if withPlatform {
		require.NoError(t, os.WriteFile(filepath.Join(dir, spec.DefaultPlatformPath), []byte(resolveStagingPlatform), 0o600))
	}
	return dir
}

func TestResolve_HookCycleIsWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		specYAML string
		platform bool
	}{
		{name: "two-node without platform", specYAML: resolveHookCycleSpec},
		{name: "two-node with platform", specYAML: resolveHookCycleSpec, platform: true},
		{name: "self-cycle without platform", specYAML: resolveSelfCycleSpec},
		{name: "self-cycle with platform", specYAML: resolveSelfCycleSpec, platform: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := writeResolveSpec(t, tt.specYAML, tt.platform)
			appIO, _, out, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, []string{"resolve", "staging"}, nabattest.WithDir(dir))
			require.NoErrorf(t, err, "stderr:\n%s", errOut.String())

			got := out.String()
			assert.Contains(t, got, "hook ordering")
			assert.Contains(t, strings.ToLower(got), "cycle")
			assert.Contains(t, got, "LOG_LEVEL: debug")
		})
	}
}

func TestResolve_HookCycleJSONWarning(t *testing.T) {
	t.Parallel()

	dir := writeResolveSpec(t, resolveHookCycleSpec, true)
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

func TestResolve_UnrelatedGraphErrorsRemainHard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    string
		wantErr string
	}{
		{
			name: "missing after target",
			spec: `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [does-not-exist]
    command: [migrate]
environments:
  staging: {}
`,
			wantErr: "does not name a task",
		},
		{
			name: "after in different hook phase",
			spec: `apiVersion: v1-alpha.5
project: shop
components:
  api:
    image: busybox
tasks:
  migrate:
    from: api
    "on": preDeploy
    after: [cleanup]
    command: [migrate]
  cleanup:
    from: api
    "on": postDeploy
    command: [cleanup]
environments:
  staging: {}
`,
			wantErr: "not in the same on phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := writeResolveSpec(t, tt.spec, true)
			appIO, _, _, errOut := nabattest.NewIO()
			app := cmd.NewApp(nabat.WithIO(appIO))
			err := nabattest.RunParallel(t, app, []string{"resolve", "staging"}, nabattest.WithDir(dir))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Contains(t, errOut.String()+err.Error(), tt.wantErr)
		})
	}
}

func TestResolve_HookCycleRemainsHardError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		after []string
		extra map[string]spec.Task
	}{
		{
			name:  "two-node cycle",
			after: []string{"seed"},
			extra: map[string]spec.Task{
				"seed": {
					From:    "api",
					On:      spec.TaskOnPreDeploy,
					After:   []string{"migrate"},
					Command: []string{"seed"},
				},
			},
		},
		{
			name:  "self cycle",
			after: []string{"migrate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
						After:   tt.after,
						Command: []string{"migrate"},
					},
				},
				Environments: map[string]spec.Environment{
					"staging": {},
				},
			}
			maps.Copy(appSpec.Tasks, tt.extra)

			_, _, err := spec.Resolve(appSpec, nil, spec.NormalizeEnv("staging"), spec.SubstitutionReport{})
			require.Error(t, err)
			assert.ErrorContains(t, err, "cycle")
		})
	}
}
