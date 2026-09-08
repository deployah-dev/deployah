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

package spec

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func runtimeShopSpec(dir string) *Spec {
	return &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]Environment{
			"production": {Variables: StringMap{"TAG": "1.2.3"}},
		},
		Components: map[string]Component{
			"api": {
				Role:  ComponentRoleService,
				Image: "ghcr.io/acme/shop:${TAG}",
				Env:   StringMap{"LOG_LEVEL": "debug"},
			},
			"worker": {
				Role:  ComponentRoleWorker,
				Image: "ghcr.io/acme/shop:${TAG}",
			},
		},
		Tasks: map[string]Task{
			"migrate": {From: "api", On: TaskOnPreDeploy, Command: []string{"migrate"}},
		},
	}
}

func writeWorkedExampleFiles(t *testing.T, dir string) {
	t.Helper()
	requireWrite(t, dir, ".env", "LOG_LEVEL=info\nREGION=eu\n")
	requireWrite(t, dir, ".env.production", "DPY_VAR_TAG=1.2.3\nREGION=us\nFEATURE_X=on\n")
	requireWrite(t, dir, ".env.api", "FEATURE_X=off\n")
}

func TestResolveRuntimeEnvironment_WorkedExample(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkedExampleFiles(t, dir)
	appSpec := runtimeShopSpec(dir)
	resolved, report, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.NoError(t, err)

	apiFile := map[string]string{
		"LOG_LEVEL":   "info",
		"REGION":      "us",
		"FEATURE_X":   "off",
		"DPY_VAR_TAG": "1.2.3",
	}
	apiExplicit := map[string]string{"LOG_LEVEL": "debug"}

	tests := []struct {
		name         string
		component    string
		task         string
		wantFile     map[string]string
		wantExplicit map[string]string
		wantEntity   map[string]string
	}{
		{
			name:         "api",
			component:    "api",
			wantFile:     apiFile,
			wantExplicit: apiExplicit,
			wantEntity:   map[string]string{"FEATURE_X": "off"},
		},
		{
			name:         "worker",
			component:    "worker",
			wantFile:     map[string]string{"LOG_LEVEL": "info", "REGION": "us", "FEATURE_X": "on", "DPY_VAR_TAG": "1.2.3"},
			wantExplicit: map[string]string{},
			wantEntity:   map[string]string{},
		},
		{
			name:         "migrate inherits api maps",
			task:         "migrate",
			wantFile:     apiFile,
			wantExplicit: apiExplicit,
			wantEntity:   map[string]string{"FEATURE_X": "off"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := resolvedRuntime(resolved, tt.component, tt.task)
			assert.Equal(t, tt.wantFile, rt.FileValues)
			assert.Equal(t, tt.wantExplicit, rt.ExplicitValues)
			assert.Equal(t, tt.wantEntity, rt.EntityValues)
		})
	}

	var fileKeys, explicitKeys []string
	for _, f := range report.Fields {
		switch {
		case f.Component == "api" && f.Path == "runtime.fileValues.FEATURE_X":
			fileKeys = append(fileKeys, f.Source)
		case f.Component == "api" && f.Path == "runtime.explicitValues.LOG_LEVEL":
			explicitKeys = append(explicitKeys, f.Source)
		}
	}
	assert.NotEmpty(t, fileKeys)
	assert.NotEmpty(t, explicitKeys)
}

func TestResolveRuntimeEnvironment_EnvFileLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		envFile     string
		apiEnvFile  string
		taskEnvFile string
		wantAPI     map[string]string
		wantWorker  map[string]string
		wantMigrate map[string]string
		wantTaskEV  map[string]string
	}{
		{
			name: "explicit envFile replaces implicit entity layer",
			files: map[string]string{
				".env":            "LOG_LEVEL=info\nREGION=eu\n",
				".env.production": "REGION=us\n",
				"custom.env":      "SHARED=from-env\nENV_ONLY=1\n",
				"api.env":         "SHARED=from-entity\nENTITY_ONLY=1\n",
			},
			envFile:    "custom.env",
			apiEnvFile: "api.env",
			wantAPI:    map[string]string{"SHARED": "from-entity", "ENV_ONLY": "1", "ENTITY_ONLY": "1"},
			wantWorker: map[string]string{"SHARED": "from-env", "ENV_ONLY": "1"},
			wantMigrate: map[string]string{
				"SHARED":      "from-entity",
				"ENV_ONLY":    "1",
				"ENTITY_ONLY": "1",
			},
			wantTaskEV: map[string]string{"SHARED": "from-entity", "ENTITY_ONLY": "1"},
		},
		{
			name: "task envFile keeps environment values",
			files: map[string]string{
				".env":            "LOG_LEVEL=info\nREGION=eu\n",
				".env.production": "DPY_VAR_TAG=1.2.3\nREGION=us\nFEATURE_X=on\n",
				".env.api":        "FEATURE_X=off\n",
				"migrate.env":     "TASK_ONLY=1\nFEATURE_X=task\n",
			},
			taskEnvFile: "migrate.env",
			wantAPI: map[string]string{
				"LOG_LEVEL":   "info",
				"REGION":      "us",
				"FEATURE_X":   "off",
				"DPY_VAR_TAG": "1.2.3",
			},
			wantWorker: map[string]string{
				"LOG_LEVEL":   "info",
				"REGION":      "us",
				"FEATURE_X":   "on",
				"DPY_VAR_TAG": "1.2.3",
			},
			wantMigrate: map[string]string{
				"LOG_LEVEL":   "info",
				"REGION":      "us",
				"FEATURE_X":   "task",
				"DPY_VAR_TAG": "1.2.3",
				"TASK_ONLY":   "1",
			},
			wantTaskEV: map[string]string{"TASK_ONLY": "1", "FEATURE_X": "task"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for rel, body := range tt.files {
				requireWrite(t, dir, rel, body)
			}
			appSpec := runtimeShopSpec(dir)
			env := appSpec.Environments["production"]
			env.EnvFile = tt.envFile
			appSpec.Environments["production"] = env
			api := appSpec.Components["api"]
			api.EnvFile = tt.apiEnvFile
			appSpec.Components["api"] = api
			task := appSpec.Tasks["migrate"]
			task.EnvFile = tt.taskEnvFile
			appSpec.Tasks["migrate"] = task

			resolved, _, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
			require.NoError(t, err)
			assert.Equal(t, tt.wantAPI, resolved.Components["api"].Runtime.FileValues)
			assert.Equal(t, tt.wantWorker, resolved.Components["worker"].Runtime.FileValues)
			assert.Equal(t, tt.wantMigrate, resolved.Tasks["migrate"].Runtime.FileValues)
			assert.Equal(t, tt.wantTaskEV, resolved.Tasks["migrate"].Runtime.EntityValues)
		})
	}
}

func TestResolveRuntimeEnvironment_SanitizedEnvFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envName  string
		envFile  string
		files    map[string]string
		wantFile map[string]string
	}{
		{
			name:     "wildcard environment name finds sanitized file",
			envName:  "review/*",
			files:    map[string]string{".env.review": "TEST_VAR=test\n"},
			wantFile: map[string]string{"TEST_VAR": "test"},
		},
		{
			name:     "path separator environment name finds sanitized file",
			envName:  "feature/branch",
			files:    map[string]string{".env.featurebranch": "TEST_VAR=test\n"},
			wantFile: map[string]string{"TEST_VAR": "test"},
		},
		{
			name:     "multiple special chars in environment name",
			envName:  "dev/*/test?env",
			files:    map[string]string{".deployah/.env.devtestenv": "TEST_VAR=test\n"},
			wantFile: map[string]string{"TEST_VAR": "test"},
		},
		{
			name:     "fallback to default .env when sanitized file not found",
			envName:  "review/*",
			files:    map[string]string{".env": "TEST_VAR=test\n"},
			wantFile: map[string]string{"TEST_VAR": "test"},
		},
		{
			name:     "no files found",
			envName:  "review/*",
			wantFile: map[string]string{},
		},
		{
			name:     "explicit env file with wildcard environment name",
			envName:  "review/*",
			envFile:  "custom.env",
			files:    map[string]string{"custom.env": "EXPLICIT_VAR=explicit\n"},
			wantFile: map[string]string{"EXPLICIT_VAR": "explicit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for rel, body := range tt.files {
				requireWrite(t, dir, rel, body)
			}
			appSpec := runtimeShopSpec(dir)
			appSpec.Environments = map[string]Environment{
				tt.envName: {EnvFile: tt.envFile},
			}

			resolved, _, err := Resolve(appSpec, nil, NormalizeEnv(tt.envName), SubstitutionReport{})
			require.NoError(t, err)
			assert.Equal(t, tt.wantFile, resolved.Components["api"].Runtime.FileValues)
		})
	}
}

func TestResolveRuntimeEnvironment_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envFile  string
		dirs     []string
		files    map[string]string
		wantCode string
		wantErr  string
	}{
		{
			name:     "missing explicit env file",
			envFile:  "missing.env",
			wantCode: ErrCodeEnvFileNotFound,
			wantErr:  "missing.env",
		},
		{
			name:     "unreadable explicit env file",
			envFile:  "not-a-file",
			dirs:     []string{"not-a-file"},
			wantCode: ErrCodeEnvFileReadError,
			wantErr:  "not-a-file",
		},
		{
			name:     "invalid key",
			files:    map[string]string{".env": "not-a-key=1\n"},
			wantCode: ErrCodeInvalidEnvKey,
			wantErr:  "not-a-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range tt.dirs {
				require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o750))
			}
			for rel, body := range tt.files {
				requireWrite(t, dir, rel, body)
			}
			appSpec := runtimeShopSpec(dir)
			env := appSpec.Environments["production"]
			env.EnvFile = tt.envFile
			appSpec.Environments["production"] = env

			_, report, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			if tt.envFile != "" {
				assert.ErrorContains(t, err, filepath.Join(dir, tt.envFile))
			}
			assert.Equal(t, tt.wantCode, report.ErrorCode)
			re, ok := errors.AsType[*ResolutionError](err)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, re.Code)
		})
	}
}

func TestResolveForDisplay_MissingEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := runtimeShopSpec(dir)
	env := appSpec.Environments["production"]
	env.EnvFile = "missing.env"
	appSpec.Environments["production"] = env

	_, report, err := ResolveForDisplay(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.Error(t, err)
	assert.Equal(t, ErrCodeEnvFileNotFound, report.ErrorCode)
	assert.NotEqual(t, ErrCodePlatformNotFound, report.ErrorCode)
	require.NotEmpty(t, report.Warnings)
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "platform file not found")
}

func TestResolveForDisplay_HookCycleIsWarning(t *testing.T) {
	t.Parallel()

	platform := &PlatformConfig{
		APIVersion: "platform/v1-alpha.3",
		Environments: map[string]PlatformEnvironment{
			"production": {Context: "prod-eks"},
		},
	}

	tests := []struct {
		name     string
		platform *PlatformConfig
		self     bool
	}{
		{name: "two-node without platform"},
		{name: "two-node with platform", platform: platform},
		{name: "self-cycle without platform", self: true},
		{name: "self-cycle with platform", platform: platform, self: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeWorkedExampleFiles(t, dir)
			appSpec := runtimeShopSpec(dir)
			if tt.self {
				migrate := appSpec.Tasks["migrate"]
				migrate.After = []string{"migrate"}
				appSpec.Tasks["migrate"] = migrate
			} else {
				migrate := appSpec.Tasks["migrate"]
				migrate.After = []string{"seed"}
				appSpec.Tasks["migrate"] = migrate
				appSpec.Tasks["seed"] = Task{
					From:    "api",
					On:      TaskOnPreDeploy,
					After:   []string{"migrate"},
					Command: []string{"seed"},
				}
			}

			resolved, report, err := ResolveForDisplay(appSpec, tt.platform, NormalizeEnv("production"), SubstitutionReport{})
			require.NoError(t, err)
			assert.Empty(t, report.ErrorCode)
			assert.NotEqual(t, ErrCodePlatformNotFound, report.ErrorCode)
			require.NotEmpty(t, report.Warnings)
			joined := strings.Join(report.Warnings, "\n")
			assert.Contains(t, joined, "hook ordering")
			assert.Contains(t, strings.ToLower(joined), "cycle")
			assert.Equal(t, "debug", resolved.Components["api"].Runtime.ExplicitValues["LOG_LEVEL"])
			assert.Equal(t, "us", resolved.Components["api"].Runtime.FileValues["REGION"])
			assert.Zero(t, resolved.Tasks["migrate"].HookWeight)
			if !tt.self {
				assert.Zero(t, resolved.Tasks["seed"].HookWeight)
			}

			_, _, resolveErr := Resolve(appSpec, tt.platform, NormalizeEnv("production"), SubstitutionReport{})
			require.Error(t, resolveErr)
			assert.ErrorContains(t, resolveErr, "cycle")
			cycle, ok := errors.AsType[*HookCycleError](resolveErr)
			assert.True(t, ok)
			assert.NotNil(t, cycle)
		})
	}
}

func TestResolveRuntimeEnvironment_FromDoesNotAliasParent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkedExampleFiles(t, dir)
	appSpec := runtimeShopSpec(dir)
	resolved, _, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.NoError(t, err)

	parent := resolved.Components["api"].Runtime
	child := resolved.Tasks["migrate"].Runtime
	require.NotNil(t, parent.FileValues)
	require.NotNil(t, child.FileValues)
	child.FileValues["REGION"] = "mutated"
	child.ExplicitValues["LOG_LEVEL"] = "mutated"
	child.EntityValues["FEATURE_X"] = "mutated"
	assert.Equal(t, "us", parent.FileValues["REGION"])
	assert.Equal(t, "debug", parent.ExplicitValues["LOG_LEVEL"])
	assert.Equal(t, "off", parent.EntityValues["FEATURE_X"])
}

func TestResolveRuntimeEnvironment_ProvenanceOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkedExampleFiles(t, dir)
	appSpec := runtimeShopSpec(dir)
	resolved1, report1, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.NoError(t, err)
	_, report2, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.NoError(t, err)
	require.NotNil(t, resolved1)

	paths1 := runtimeFieldPaths(report1)
	paths2 := runtimeFieldPaths(report2)
	assert.Equal(t, paths1, paths2)
	assertRuntimeFieldOrder(t, paths1, resolved1)

	var featureSource string
	for _, f := range report1.Fields {
		if f.Component == "api" && f.Path == "runtime.fileValues.FEATURE_X" {
			featureSource = f.Source
		}
	}
	assert.Equal(t, ".env.production, .env.api", featureSource)
}

func resolvedRuntime(resolved *ResolvedSpec, component, task string) ResolvedRuntimeEnvironment {
	if task != "" {
		return resolved.Tasks[task].Runtime
	}
	return resolved.Components[component].Runtime
}

func runtimeFieldPaths(report *ResolutionReport) []string {
	var out []string
	for _, f := range report.Fields {
		if strings.HasPrefix(f.Path, "runtime.") {
			out = append(out, f.Component+"/"+f.Path)
		}
	}
	return out
}

func assertRuntimeFieldOrder(t *testing.T, paths []string, resolved *ResolvedSpec) {
	t.Helper()
	var lastKind, lastEntity, lastGroup, lastKey string
	for _, p := range paths {
		entity, rest, ok := strings.Cut(p, "/")
		require.True(t, ok, p)
		group, key, ok := strings.Cut(strings.TrimPrefix(rest, "runtime."), ".")
		require.True(t, ok, p)

		kind := "component"
		if _, isTask := resolved.Tasks[entity]; isTask {
			kind = "task"
		}
		if lastKind == "task" && kind == "component" {
			t.Fatalf("component runtime field %s after task fields", p)
		}
		if kind == lastKind && entity < lastEntity {
			t.Fatalf("entity order %s after %s", entity, lastEntity)
		}
		if entity == lastEntity {
			if lastGroup == "explicitValues" && group == "fileValues" {
				t.Fatalf("fileValues after explicitValues for %s", entity)
			}
			if group == lastGroup && key < lastKey {
				t.Fatalf("key order %s after %s in %s", key, lastKey, entity)
			}
		}
		lastKind, lastEntity, lastGroup, lastKey = kind, entity, group, key
	}
}

func TestResolveRuntimeEnvironment_ComponentsBeforeTasks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeWorkedExampleFiles(t, dir)
	appSpec := runtimeShopSpec(dir)
	resolved, report, err := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.NoError(t, err)
	assertRuntimeFieldOrder(t, runtimeFieldPaths(report), resolved)
}

func TestResolveRuntimeEnvironment_WildcardProvenanceUsesSpecKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requireWrite(t, dir, "review.env", "REGION=eu\n")
	appSpec := runtimeShopSpec(dir)
	appSpec.Environments = map[string]Environment{
		"review": {EnvFile: "review.env"},
	}

	resolved, report, err := Resolve(appSpec, nil, NormalizeEnv("review/pr-123"), SubstitutionReport{})
	require.NoError(t, err)
	assert.Equal(t, "review/pr-123", resolved.Env.Original)
	assert.Equal(t, "eu", resolved.Components["api"].Runtime.FileValues["REGION"])

	var source string
	for _, f := range report.Fields {
		if f.Component == "api" && f.Path == "runtime.fileValues.REGION" {
			source = f.Source
		}
	}
	assert.Contains(t, source, "environments.review.envFile")
	assert.NotContains(t, source, "environments.review/pr-123.envFile")
}

func TestResolveRuntimeEnvironment_InactiveParentInheritedByActiveTasks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requireWrite(t, dir, ".env", "REGION=eu\n")
	requireWrite(t, dir, ".env.api", "FEATURE_X=off\n")
	appSpec := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]Environment{
			"staging":    {},
			"production": {},
		},
		Components: map[string]Component{
			"api": {
				Role:         ComponentRoleService,
				Image:        "ghcr.io/acme/shop:1",
				Environments: []string{"production"},
				Env:          StringMap{"LOG_LEVEL": "debug"},
			},
		},
		Tasks: map[string]Task{
			"migrate": {
				From:         "api",
				On:           TaskOnPreDeploy,
				Command:      []string{"migrate"},
				Environments: []string{"staging"},
			},
			"seed": {
				From:         "api",
				On:           TaskOnPreDeploy,
				Command:      []string{"seed"},
				Environments: []string{"staging"},
			},
		},
	}

	resolved, _, err := Resolve(appSpec, nil, NormalizeEnv("staging"), SubstitutionReport{})
	require.NoError(t, err)
	_, parentRendered := resolved.Components["api"]
	assert.False(t, parentRendered)

	wantEntity := map[string]string{"FEATURE_X": "off"}
	wantExplicit := map[string]string{"LOG_LEVEL": "debug"}
	wantFile := map[string]string{"REGION": "eu", "FEATURE_X": "off"}
	for _, name := range []string{"migrate", "seed"} {
		rt := resolved.Tasks[name].Runtime
		assert.Equal(t, wantFile, rt.FileValues, name)
		assert.Equal(t, wantExplicit, rt.ExplicitValues, name)
		assert.Equal(t, wantEntity, rt.EntityValues, name)
	}

	resolved.Tasks["migrate"].Runtime.EntityValues["FEATURE_X"] = "mutated"
	resolved.Tasks["migrate"].Runtime.ExplicitValues["LOG_LEVEL"] = "mutated"
	assert.Equal(t, "off", resolved.Tasks["seed"].Runtime.EntityValues["FEATURE_X"])
	assert.Equal(t, "debug", resolved.Tasks["seed"].Runtime.ExplicitValues["LOG_LEVEL"])
}

func TestParentRuntimeCache_ResolvesInactiveParentOnce(t *testing.T) {
	t.Parallel()

	explicitCalls := 0
	entityCalls := 0
	cache := &parentRuntimeCache{
		appSpec: &Spec{
			Components: map[string]Component{"api": {EnvFile: ".env.api"}},
		},
		resolved: &ResolvedSpec{Components: map[string]ResolvedComponent{}},
		inactive: make(map[string]*cachedParentLayers),
		loadExplicit: func(from string, parent Component) (map[string]string, error) {
			explicitCalls++
			assert.Equal(t, "api", from)
			assert.Equal(t, ".env.api", parent.EnvFile)
			return map[string]string{"LOG_LEVEL": "debug"}, nil
		},
		loadEntity: func(from string, parent Component) (map[string]string, error) {
			entityCalls++
			assert.Equal(t, "api", from)
			return map[string]string{"FEATURE_X": "off"}, nil
		},
	}

	first, err := cache.get("api", true)
	require.NoError(t, err)
	second, err := cache.get("api", true)
	require.NoError(t, err)
	assert.Equal(t, 1, explicitCalls)
	assert.Equal(t, 1, entityCalls)
	assert.Equal(t, "off", first.EntityValues["FEATURE_X"])
	assert.Equal(t, "off", second.EntityValues["FEATURE_X"])
	first.EntityValues["FEATURE_X"] = "mutated"
	assert.Equal(t, "off", second.EntityValues["FEATURE_X"])
	assert.Equal(t, "off", cache.inactive["api"].entity["FEATURE_X"])
}

func TestParentRuntimeCache_SkipsEntityWhenChildHasEnvFile(t *testing.T) {
	t.Parallel()

	entityCalls := 0
	cache := &parentRuntimeCache{
		appSpec: &Spec{
			Components: map[string]Component{"api": {EnvFile: "missing.env"}},
		},
		resolved: &ResolvedSpec{Components: map[string]ResolvedComponent{}},
		inactive: make(map[string]*cachedParentLayers),
		loadExplicit: func(string, Component) (map[string]string, error) {
			return map[string]string{"LOG_LEVEL": "debug"}, nil
		},
		loadEntity: func(string, Component) (map[string]string, error) {
			entityCalls++
			return nil, errors.New("parent entity should not load")
		},
	}

	first, err := cache.get("api", false)
	require.NoError(t, err)
	assert.Equal(t, 0, entityCalls)
	assert.Equal(t, "debug", first.ExplicitValues["LOG_LEVEL"])
	assert.Empty(t, first.EntityValues)

	second, err := cache.get("api", true)
	require.Error(t, err)
	assert.ErrorContains(t, err, "parent entity should not load")
	assert.Equal(t, 1, entityCalls)
	assert.Nil(t, second)
}

func TestResolveRuntimeEnvironment_InactiveParentMissingEnvFileIgnoredWhenChildHasEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requireWrite(t, dir, "migrate.env", "MIGRATION_TARGET=users\n")
	appSpec := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]Environment{
			"staging":    {},
			"production": {},
		},
		Components: map[string]Component{
			"api": {
				Role:         ComponentRoleService,
				Image:        "ghcr.io/acme/shop:1",
				Environments: []string{"production"},
				EnvFile:      "missing.env",
				Env:          StringMap{"LOG_LEVEL": "debug"},
			},
		},
		Tasks: map[string]Task{
			"migrate": {
				From:         "api",
				On:           TaskOnPreDeploy,
				Command:      []string{"migrate"},
				Environments: []string{"staging"},
				EnvFile:      "migrate.env",
				Env:          StringMap{"MIGRATION_MODE": "safe"},
			},
		},
	}

	resolved, _, err := Resolve(appSpec, nil, NormalizeEnv("staging"), SubstitutionReport{})
	require.NoError(t, err)
	_, parentRendered := resolved.Components["api"]
	assert.False(t, parentRendered)

	rt := resolved.Tasks["migrate"].Runtime
	assert.Equal(t, map[string]string{"MIGRATION_TARGET": "users"}, rt.FileValues)
	assert.Equal(t, map[string]string{
		"LOG_LEVEL":      "debug",
		"MIGRATION_MODE": "safe",
	}, rt.ExplicitValues)
	assert.Equal(t, map[string]string{"MIGRATION_TARGET": "users"}, rt.EntityValues)
}

func TestResolveRuntimeEnvironment_InactiveParentInvalidEnvKeyIgnoredWhenChildHasEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	requireWrite(t, dir, "missing.env", "FOO-BAR=1\n")
	requireWrite(t, dir, "migrate.env", "MIGRATION_TARGET=users\n")
	appSpec := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]Environment{
			"staging":    {},
			"production": {},
		},
		Components: map[string]Component{
			"api": {
				Role:         ComponentRoleService,
				Image:        "ghcr.io/acme/shop:1",
				Environments: []string{"production"},
				EnvFile:      "missing.env",
				Env:          StringMap{"LOG_LEVEL": "debug"},
			},
		},
		Tasks: map[string]Task{
			"migrate": {
				From:         "api",
				On:           TaskOnPreDeploy,
				Command:      []string{"migrate"},
				Environments: []string{"staging"},
				EnvFile:      "migrate.env",
			},
			"seed": {
				From:         "api",
				On:           TaskOnPreDeploy,
				Command:      []string{"seed"},
				Environments: []string{"staging"},
			},
		},
	}

	resolved, _, err := Resolve(appSpec, nil, NormalizeEnv("staging"), SubstitutionReport{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "FOO-BAR")
	var re *ResolutionError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, ErrCodeInvalidEnvKey, re.Code)
	assert.Nil(t, resolved)

	delete(appSpec.Tasks, "seed")
	resolved, _, err = Resolve(appSpec, nil, NormalizeEnv("staging"), SubstitutionReport{})
	require.NoError(t, err)
	rt := resolved.Tasks["migrate"].Runtime
	assert.Equal(t, map[string]string{"MIGRATION_TARGET": "users"}, rt.FileValues)
	assert.Equal(t, map[string]string{"LOG_LEVEL": "debug"}, rt.ExplicitValues)
}

func TestResolveRuntimeEnvironment_InactiveParentMissingEnvFileRequiredWithoutChildEnvFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appSpec := &Spec{
		APIVersion: CurrentManifestVersion,
		Project:    "shop",
		SpecDir:    dir,
		Environments: map[string]Environment{
			"staging":    {},
			"production": {},
		},
		Components: map[string]Component{
			"api": {
				Role:         ComponentRoleService,
				Image:        "ghcr.io/acme/shop:1",
				Environments: []string{"production"},
				EnvFile:      "missing.env",
				Env:          StringMap{"LOG_LEVEL": "debug"},
			},
		},
		Tasks: map[string]Task{
			"migrate": {
				From:         "api",
				On:           TaskOnPreDeploy,
				Command:      []string{"migrate"},
				Environments: []string{"staging"},
			},
		},
	}

	_, report, err := Resolve(appSpec, nil, NormalizeEnv("staging"), SubstitutionReport{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "missing.env")
	assert.Equal(t, ErrCodeEnvFileNotFound, report.ErrorCode)
}
