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

	dir := t.TempDir()
	writeWorkedExampleFiles(t, dir)
	appSpec := runtimeShopSpec(dir)
	migrate := appSpec.Tasks["migrate"]
	migrate.After = []string{"seed"}
	appSpec.Tasks["migrate"] = migrate
	appSpec.Tasks["seed"] = Task{
		From:    "api",
		On:      TaskOnPreDeploy,
		After:   []string{"migrate"},
		Command: []string{"seed"},
	}

	resolved, report, err := ResolveForDisplay(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.NoError(t, err)
	assert.Empty(t, report.ErrorCode)
	assert.NotEqual(t, ErrCodePlatformNotFound, report.ErrorCode)
	require.NotEmpty(t, report.Warnings)
	assert.Contains(t, strings.Join(report.Warnings, "\n"), "hook ordering")
	assert.Equal(t, "us", resolved.Components["api"].Runtime.FileValues["REGION"])
	assert.Zero(t, resolved.Tasks["migrate"].HookWeight)
	assert.Zero(t, resolved.Tasks["seed"].HookWeight)

	_, _, resolveErr := Resolve(appSpec, nil, NormalizeEnv("production"), SubstitutionReport{})
	require.Error(t, resolveErr)
	assert.ErrorContains(t, resolveErr, "cycle")
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
