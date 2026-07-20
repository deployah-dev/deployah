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

package testing

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// errorScenarioIndicators are name prefixes that mark a scenario as
// expecting a load/resolve error. Every scenario matching one of these
// must carry an error-config.yaml (see [detectErrorScenario]) so the exact
// expected error is asserted rather than a generic default.
var errorScenarioIndicators = []string{
	"invalid-",
	"error-",
	"fail-",
	"bad-",
	"malformed-",
}

// DiscoverScenarios discovers test scenarios from the directory structure.
func DiscoverScenarios(scenariosDir string) ([]TestScenario, error) {
	var scenarios []TestScenario

	err := filepath.Walk(scenariosDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Skip if not a directory or if it's the root scenarios directory
		if !info.IsDir() || path == scenariosDir {
			return nil
		}

		// plan-* directories hold plan-config.yaml scenarios (see
		// plan_scenarios.go), not render/golden-file scenarios: skip them
		// here so they never get treated as a render scenario missing its
		// expected/ directory.
		if strings.HasPrefix(info.Name(), "plan-") {
			return nil
		}

		manifestPath := filepath.Join(path, "deployah.yaml")
		if _, statErr := os.Stat(manifestPath); errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}

		scenarioName := info.Name()
		base := TestScenario{
			Name:         scenarioName,
			ScenarioDir:  scenarioName,
			ManifestFile: "deployah.yaml",
		}

		// Look for environment files based on naming convention
		envFiles, err := findEnvFiles(path)
		if err != nil {
			return err
		}
		base.EnvFiles = envFiles

		platformPath := filepath.Join(path, "deployah.platform.yaml")
		if _, statErr := os.Stat(platformPath); statErr == nil {
			base.PlatformFile = "deployah.platform.yaml"
		}

		// A scenario with per-environment goldens (expected-<env>/
		// directories) is discovered as one TestScenario per environment,
		// instead of a single scenario with a plain "expected" directory.
		envNames, err := findEnvironmentGoldenDirs(path)
		if err != nil {
			return err
		}
		if len(envNames) > 0 {
			for _, envName := range envNames {
				envScenario := base
				envScenario.Name = fmt.Sprintf("%s[%s]", scenarioName, envName)
				envScenario.Environment = envName
				envScenario.ExpectedDir = filepath.Join(scenarioName, "expected-"+envName)

				envScenario, detectErr := detectErrorScenario(envScenario, path)
				if detectErr != nil {
					return detectErr
				}
				scenarios = append(scenarios, envScenario)
			}
			return nil
		}

		// Look for expected output directory based on naming convention
		expectedDir := filepath.Join(path, "expected")
		if _, statErr := os.Stat(expectedDir); statErr == nil {
			base.ExpectedDir = filepath.Join(scenarioName, "expected")
		}

		base, err = detectErrorScenario(base, path)
		if err != nil {
			return err
		}

		scenarios = append(scenarios, base)
		return nil
	})

	return scenarios, err
}

// findEnvironmentGoldenDirs returns the sorted environment names for every
// "expected-<env>" directory directly under scenarioPath.
func findEnvironmentGoldenDirs(scenarioPath string) ([]string, error) {
	entries, err := os.ReadDir(scenarioPath)
	if err != nil {
		return nil, err
	}

	var envNames []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "expected-") {
			envNames = append(envNames, strings.TrimPrefix(entry.Name(), "expected-"))
		}
	}
	slices.Sort(envNames)
	return envNames, nil
}

// detectErrorScenario detects scenarios that test error conditions using a
// hybrid naming and config-file approach: a scenario.Name prefix (see
// [errorScenarioIndicators]) requires an error-config.yaml to exist in
// scenarioPath, naming the exact expected error substrings. This is
// mandatory, not just a default, so an error scenario's assertion always
// documents the specific error it exercises instead of matching the
// generic "validation failed" against any failure.
func detectErrorScenario(scenario TestScenario, scenarioPath string) (TestScenario, error) {
	hasErrorIndicator := slices.ContainsFunc(errorScenarioIndicators, func(indicator string) bool {
		return strings.HasPrefix(scenario.Name, indicator)
	})

	errorConfigPath := filepath.Join(scenarioPath, "error-config.yaml")
	expectedErrors, hasErrorConfig, err := tryLoadErrorConfig(errorConfigPath)
	if err != nil {
		return scenario, fmt.Errorf("scenario %s: loading error-config.yaml: %w", scenario.Name, err)
	}

	if hasErrorIndicator && !hasErrorConfig {
		return scenario, fmt.Errorf(
			"scenario %s: name looks like an error scenario but has no error-config.yaml; "+
				"add one with an expectedErrors list naming the specific error", scenario.Name)
	}

	if hasErrorConfig {
		scenario.ExpectError = true
		scenario.ExpectedErrors = expectedErrors
	}

	return scenario, nil
}

// tryLoadErrorConfig reads and parses configPath's expectedErrors list.
// found is false (with a nil error) when configPath does not exist.
func tryLoadErrorConfig(configPath string) (expectedErrors []string, found bool, err error) {
	data, readErr := os.ReadFile(configPath) // #nosec G304 -- scenario config under test scenarios dir
	if errors.Is(readErr, fs.ErrNotExist) {
		return nil, false, nil
	}
	if readErr != nil {
		return nil, false, readErr
	}

	var config struct {
		ExpectedErrors []string `yaml:"expectedErrors"`
	}
	if unmarshalErr := yaml.Unmarshal(data, &config); unmarshalErr != nil {
		return nil, true, unmarshalErr
	}

	return config.ExpectedErrors, true, nil
}

// findEnvFiles finds .env files in a scenario directory based on naming convention
func findEnvFiles(scenarioPath string) ([]string, error) {
	var envFiles []string

	entries, err := os.ReadDir(scenarioPath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasPrefix(entry.Name(), ".env") || strings.HasSuffix(entry.Name(), ".env")) {
			envFiles = append(envFiles, entry.Name())
		}
	}

	return envFiles, nil
}
