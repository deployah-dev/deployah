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
	"strings"

	"gopkg.in/yaml.v3"
)

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
		scenario := TestScenario{
			Name:         scenarioName,
			ScenarioDir:  scenarioName,
			ManifestFile: "deployah.yaml",
		}

		// Look for environment files based on naming convention
		envFiles, err := findEnvFiles(path)
		if err != nil {
			return err
		}
		scenario.EnvFiles = envFiles

		platformPath := filepath.Join(path, "deployah.platform.yaml")
		if _, statErr := os.Stat(platformPath); statErr == nil {
			scenario.PlatformFile = "deployah.platform.yaml"
		}

		// Look for expected output directory based on naming convention
		expectedDir := filepath.Join(path, "expected")
		if _, statErr := os.Stat(expectedDir); statErr == nil {
			scenario.ExpectedDir = filepath.Join(scenarioName, "expected")
		}

		// Detect error scenarios using hybrid approach
		scenario = detectErrorScenario(scenario, path)

		scenarios = append(scenarios, scenario)
		return nil
	})

	return scenarios, err
}

// detectErrorScenario detects scenarios that test error conditions using a
// hybrid naming and config-file approach.
func detectErrorScenario(scenario TestScenario, scenarioPath string) TestScenario {
	errorIndicators := []string{
		"invalid-",
		"error-",
		"fail-",
		"bad-",
		"malformed-",
	}

	for _, indicator := range errorIndicators {
		if strings.HasPrefix(scenario.Name, indicator) {
			scenario.ExpectError = true
			scenario.ExpectedErrors = []string{"validation failed"} // Default specific error
			break
		}
	}

	errorConfigPath := filepath.Join(scenarioPath, "error-config.yaml")
	if _, statErr := os.Stat(errorConfigPath); statErr == nil {
		expectedErrors, loadErr := loadErrorConfig(errorConfigPath)
		if loadErr == nil {
			scenario.ExpectedErrors = expectedErrors
		}
	}

	return scenario
}

// loadErrorConfig loads error configuration from a YAML file
func loadErrorConfig(configPath string) ([]string, error) {
	data, err := os.ReadFile(configPath) // #nosec G304 -- scenario config under test scenarios dir
	if err != nil {
		return nil, err
	}

	var config struct {
		ExpectedErrors []string `yaml:"expectedErrors"`
	}

	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return config.ExpectedErrors, nil
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

// LoadScenario loads a specific scenario by name
func LoadScenario(scenariosDir, scenarioName string) (*TestScenario, error) {
	scenarioDir := filepath.Join(scenariosDir, scenarioName)

	if _, err := os.Stat(scenarioDir); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("scenario directory not found: %s", scenarioDir)
	}

	manifestPath := filepath.Join(scenarioDir, "deployah.yaml")
	if _, statErr := os.Stat(manifestPath); errors.Is(statErr, fs.ErrNotExist) {
		return nil, fmt.Errorf("deployah.yaml not found in scenario: %s", scenarioName)
	}

	scenario := &TestScenario{
		Name:         scenarioName,
		ScenarioDir:  scenarioName,
		ManifestFile: "deployah.yaml",
	}

	envFiles, err := findEnvFiles(scenarioDir)
	if err != nil {
		return nil, err
	}
	scenario.EnvFiles = envFiles

	platformPath := filepath.Join(scenarioDir, "deployah.platform.yaml")
	if _, statErr := os.Stat(platformPath); statErr == nil {
		scenario.PlatformFile = "deployah.platform.yaml"
	}

	expectedDir := filepath.Join(scenarioDir, "expected")
	if _, statErr := os.Stat(expectedDir); statErr == nil {
		scenario.ExpectedDir = filepath.Join(scenarioName, "expected")
	}

	*scenario = detectErrorScenario(*scenario, scenarioDir)

	return scenario, nil
}

// LoadScenarioWithEnvironment loads a scenario with a specific environment
func LoadScenarioWithEnvironment(scenariosDir, scenarioName, environment string) (*TestScenario, error) {
	scenario, err := LoadScenario(scenariosDir, scenarioName)
	if err != nil {
		return nil, err
	}

	scenario.Environment = environment

	// Look for environment-specific expected output
	expectedDir := filepath.Join(scenariosDir, scenarioName, "expected-"+environment)
	if _, statErr := os.Stat(expectedDir); statErr == nil {
		scenario.ExpectedDir = filepath.Join(scenarioName, "expected-"+environment)
	}

	return scenario, nil
}
