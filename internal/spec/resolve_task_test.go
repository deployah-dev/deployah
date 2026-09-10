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

package spec

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resolveTaskSpec(dir string) *Spec {
	return &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Components: map[string]Component{
			"api": {Image: "ghcr.io/acme/shop:1.2.3", Env: StringMap{"DATABASE_URL": "postgres://db"}},
			"web": {Image: "ghcr.io/acme/web:1.0.0"},
		},
		Tasks: map[string]Task{
			"migrate": {
				From:    "api",
				On:      TaskOnPreDeploy,
				Command: []string{"migrate", "up"},
			},
			"cleanup": {
				From:    "api",
				On:      TaskOnManual,
				Command: []string{"cleanup"},
			},
			"backfill": {
				From:         "api",
				On:           TaskOnManual,
				Command:      []string{"backfill"},
				Environments: []string{"prod"},
			},
		},
	}
}

func resolveTaskPlatform() *PlatformConfig {
	return &PlatformConfig{
		APIVersion: "platform/v1-alpha.3",
		Environments: map[string]PlatformEnvironment{
			"dev": {
				Context: "kind",
				Domains: map[string]PlatformDomain{
					"public": {
						BaseDomain: "example.com",
						TLS:        &PlatformTLS{Mode: TLSModeSelfSigned},
					},
				},
			},
		},
	}
}

func requireResolutionCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	re, ok := errors.AsType[*ResolutionError](err)
	require.True(t, ok, "ResolveTask error %v", err)
	assert.Equal(t, code, re.Code)
}

func TestResolveTask_UnrelatedComponentMissingEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	web := appSpec.Components["web"]
	web.EnvFile = "missing.web.env"
	appSpec.Components["web"] = web

	rt, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)
	assert.Equal(t, "postgres://db", rt.Runtime.ExplicitValues["DATABASE_URL"])
}

func TestResolveTask_UnrelatedTaskMissingEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	cleanup := appSpec.Tasks["cleanup"]
	cleanup.EnvFile = "missing.cleanup.env"
	appSpec.Tasks["cleanup"] = cleanup

	rt, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	assert.Equal(t, []string{"migrate", "up"}, rt.Task.Command)
}

func TestResolveTask_UnrelatedComponentInvalidDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	web := appSpec.Components["web"]
	web.Expose = &Expose{Domain: "missing"}
	appSpec.Components["web"] = web
	platform := resolveTaskPlatform()

	_, _, resolveErr := Resolve(appSpec, platform, NormalizeEnv("dev"), SubstitutionReport{})
	require.Error(t, resolveErr)

	rt, err := ResolveTask(appSpec, platform, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/acme/shop:1.2.3", rt.Task.Image)
}

func TestResolveTask_ParentEnvFileInheritedMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	api := appSpec.Components["api"]
	api.EnvFile = "missing.parent.env"
	appSpec.Components["api"] = api

	_, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "migrate")
	requireResolutionCode(t, err, ErrCodeEnvFileNotFound)
	assert.Contains(t, err.Error(), "parent api")
}

func TestResolveTask_ParentEnvFileOverriddenUnused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requireWrite(t, dir, "task.env", "TASK_ONLY=1\n")
	appSpec := resolveTaskSpec(dir)
	api := appSpec.Components["api"]
	api.EnvFile = "missing.parent.env"
	appSpec.Components["api"] = api
	migrate := appSpec.Tasks["migrate"]
	migrate.EnvFile = "task.env"
	appSpec.Tasks["migrate"] = migrate

	rt, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	assert.Equal(t, "1", rt.Runtime.FileValues["TASK_ONLY"])
	assert.Equal(t, "postgres://db", rt.Runtime.ExplicitValues["DATABASE_URL"])
}

func TestResolveTask_EnvironmentLevelEnvFileMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	appSpec.Environments = map[string]Environment{
		"dev": {EnvFile: "missing.env"},
	}

	_, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "migrate")
	requireResolutionCode(t, err, ErrCodeEnvFileNotFound)
}

func TestResolveTask_ProfilesWithPlatformIgnoresUnrelatedDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	web := appSpec.Components["web"]
	web.Expose = &Expose{Domain: "missing"}
	appSpec.Components["web"] = web
	migrate := appSpec.Tasks["migrate"]
	migrate.Profiles = []string{"batch"}
	appSpec.Tasks["migrate"] = migrate

	platform := resolveTaskPlatform()
	platform.Profiles = map[string]PlatformProfile{
		"batch": {NodeSelector: map[string]string{"workload": "batch"}},
	}

	rt, err := ResolveTask(appSpec, platform, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	require.NotNil(t, rt.MergedProfile)
	assert.Equal(t, []string{"batch"}, rt.Profiles)
	assert.Equal(t, "batch", rt.MergedProfile.NodeSelector["workload"])
}

func TestResolveTask_ProfilesWithoutPlatform(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	migrate := appSpec.Tasks["migrate"]
	migrate.Profiles = []string{"batch"}
	appSpec.Tasks["migrate"] = migrate

	_, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "migrate")
	requireResolutionCode(t, err, ErrCodePlatformNotFound)
	assert.Contains(t, err.Error(), "no platform file")
}

func TestResolveTask_InactiveInEnvironment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	_, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "backfill")
	require.Error(t, err)
	assert.ErrorContains(t, err, "skipped")
}

func TestResolveTask_UnknownTask(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	_, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "missing")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown task")
}

func TestResolveTask_DefaultProfileWhenOmitted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	platform := resolveTaskPlatform()
	platform.Profiles = map[string]PlatformProfile{
		"default": {NodeSelector: map[string]string{"workload": "general"}},
	}

	rt, err := ResolveTask(appSpec, platform, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	require.NotNil(t, rt.MergedProfile)
	assert.Equal(t, []string{"default"}, rt.Profiles)
	assert.Equal(t, "general", rt.MergedProfile.NodeSelector["workload"])
}

func TestResolveTask_InheritsProfilesFromParent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	api := appSpec.Components["api"]
	api.Profiles = []string{"batch"}
	appSpec.Components["api"] = api

	platform := resolveTaskPlatform()
	platform.Profiles = map[string]PlatformProfile{
		"default": {NodeSelector: map[string]string{"workload": "general"}},
		"batch":   {PodLabels: map[string]string{"tier": "jobs"}},
	}

	rt, err := ResolveTask(appSpec, platform, NormalizeEnv("dev"), "migrate")
	require.NoError(t, err)
	require.NotNil(t, rt.MergedProfile)
	assert.Equal(t, []string{"default", "batch"}, rt.Profiles)
	assert.Equal(t, "general", rt.MergedProfile.NodeSelector["workload"])
	assert.Equal(t, "jobs", rt.MergedProfile.PodLabels["tier"])
	assert.Equal(t, []string{"batch"}, rt.Task.Profiles)
}

func TestResolveTask_InheritedProfileValidation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	api := appSpec.Components["api"]
	api.Profiles = []string{"tight"}
	appSpec.Components["api"] = api
	migrate := appSpec.Tasks["migrate"]
	migrate.Resources = Resources{CPU: MustQuantity("2000m")}
	appSpec.Tasks["migrate"] = migrate

	platform := resolveTaskPlatform()
	platform.Profiles = map[string]PlatformProfile{
		"tight": {MaxResources: &ProfileMaxResources{CPU: MustQuantity("1000m")}},
	}

	_, err := ResolveTask(appSpec, platform, NormalizeEnv("dev"), "migrate")
	requireResolutionCode(t, err, ErrCodeProfileResourceExceeded)
	assert.Contains(t, err.Error(), `task "migrate"`)
}

func TestResolveTask_NonZeroHookWeight(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := resolveTaskSpec(dir)
	appSpec.Tasks["seed"] = Task{
		From:    "api",
		On:      TaskOnPreDeploy,
		After:   []string{"migrate"},
		Command: []string{"seed"},
	}

	rt, err := ResolveTask(appSpec, nil, NormalizeEnv("dev"), "seed")
	require.NoError(t, err)
	assert.Equal(t, 1, rt.HookWeight)
	assert.Equal(t, []string{"seed"}, rt.Task.Command)
}
