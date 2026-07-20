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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// TestScenariosDir is the directory containing integration test scenarios.
var TestScenariosDir = getTestScenariosDir()

// getTestScenariosDir finds the test scenarios directory
func getTestScenariosDir() string {
	// Try different possible locations relative to the test file
	possiblePaths := []string{
		"../../scenarios",
		"../scenarios",
		"./scenarios",
		"scenarios",
		"../../testdata/scenarios",
		"../testdata/scenarios",
		"./testdata/scenarios",
		"testdata/scenarios",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			// Resolve to absolute now: callers later t.Chdir into a
			// per-scenario temp directory, which would break a relative
			// path silently (os.Stat just fails and the caller skips
			// golden-file comparison without reporting anything).
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				return path
			}
			return abs
		}
	}

	// Default fallback
	return "scenarios"
}

// TestScenario describes one integration test case under scenarios/.
type TestScenario struct {
	// Name is the scenario directory name.
	Name string
	// ScenarioDir is the relative path under TestScenariosDir.
	ScenarioDir string
	// ManifestFile is the manifest filename within ScenarioDir.
	ManifestFile string
	// Environment selects the manifest environment for chart generation.
	Environment string
	// EnvFiles lists dotenv files to copy into the test workspace.
	EnvFiles []string
	// PlatformFile is the platform filename within ScenarioDir when present.
	PlatformFile string
	// ExpectedDir is the golden output directory, relative to TestScenariosDir.
	ExpectedDir string
	// ExpectError requires manifest loading or resolution to fail.
	ExpectError bool
	// ExpectedErrors requires specific substrings in the load/resolve error message.
	ExpectedErrors []string
}

// IntegrationTestSuite runs scenario-based chart and manifest tests.
type IntegrationTestSuite struct {
	// ScenariosDir is the root directory of test scenarios.
	ScenariosDir string
	// OutputDir is the temporary workspace for a test run.
	OutputDir string
}

// NewIntegrationTestSuite creates a suite rooted at [TestScenariosDir].
func NewIntegrationTestSuite(t *testing.T) *IntegrationTestSuite {
	t.Helper()
	outputDir := t.TempDir()
	return &IntegrationTestSuite{
		ScenariosDir: TestScenariosDir,
		OutputDir:    outputDir,
	}
}

// RunScenarioTest executes a test for a specific scenario
func (suite *IntegrationTestSuite) RunScenarioTest(t *testing.T, scenario TestScenario) {
	t.Helper()
	t.Run(scenario.Name, func(t *testing.T) {
		testDir := suite.setupScenarioEnvironment(t, scenario)

		t.Chdir(testDir)

		// Phase 1: Load, validate, and resolve (platform optional)
		manifest, environment, resolved, err := suite.loadAndResolve(t, scenario)

		// Hybrid error checking approach
		if scenario.ExpectError || len(scenario.ExpectedErrors) > 0 {
			require.Error(t, err)

			if len(scenario.ExpectedErrors) > 0 {
				for _, expectedErr := range scenario.ExpectedErrors {
					assert.Contains(t, err.Error(), expectedErr)
				}
			}
			return // Don't proceed to chart generation
		}

		require.NoError(t, err)

		// Phase 2 & 3: Generate the Helm chart and render it to Kubernetes manifests
		renderedManifests, err := suite.renderChart(t, manifest, environment, resolved)
		require.NoError(t, err)

		// Phase 4: Validate against expected output
		if scenario.ExpectedDir != "" {
			expectedPath := filepath.Join(suite.ScenariosDir, scenario.ExpectedDir)
			if _, statErr := os.Stat(expectedPath); statErr != nil {
				t.Fatalf("expected output directory %s vanished after discovery: %v", expectedPath, statErr)
			}
			suite.validateAgainstExpected(t, renderedManifests, scenario.ExpectedDir)
		}

		// Phase 5: Save rendered manifests for inspection (only in verbose mode)
		if testing.Verbose() {
			suite.saveRenderedManifests(t, renderedManifests, scenario.Name)
		}
	})
}

// setupScenarioEnvironment copies scenario files to a temporary directory
func (suite *IntegrationTestSuite) setupScenarioEnvironment(t *testing.T, scenario TestScenario) string {
	t.Helper()
	testDir := filepath.Join(suite.OutputDir, scenario.Name)
	err := os.MkdirAll(testDir, 0o750)
	require.NoError(t, err)

	// Copy manifest file (always named deployah.yaml)
	manifestSrc := filepath.Join(suite.ScenariosDir, scenario.ScenarioDir, scenario.ManifestFile)
	manifestDst := filepath.Join(testDir, "deployah.yaml")
	err = copyFile(manifestSrc, manifestDst)
	require.NoError(t, err)

	for _, envFile := range scenario.EnvFiles {
		envSrc := filepath.Join(suite.ScenariosDir, scenario.ScenarioDir, envFile)
		envDst := filepath.Join(testDir, filepath.Base(envFile))
		err = copyFile(envSrc, envDst)
		require.NoError(t, err)
	}

	if scenario.PlatformFile != "" {
		platformSrc := filepath.Join(suite.ScenariosDir, scenario.ScenarioDir, scenario.PlatformFile)
		platformDst := filepath.Join(testDir, filepath.Base(scenario.PlatformFile))
		err = copyFile(platformSrc, platformDst)
		require.NoError(t, err)
	}

	return testDir
}

// loadAndResolve loads the manifest (and optional platform file), resolves the
// environment, and runs [spec.Resolve] when a platform is present.
func (suite *IntegrationTestSuite) loadAndResolve(t *testing.T, scenario TestScenario) (*spec.Spec, string, *spec.ResolvedSpec, error) {
	t.Helper()
	ctx := context.Background()

	var platform *spec.PlatformConfig
	if scenario.PlatformFile != "" {
		loaded, err := spec.LoadPlatform(filepath.Base(scenario.PlatformFile))
		if err != nil {
			return nil, "", nil, err
		}
		platform = loaded
	}

	manifest, err := spec.Load(ctx, scenario.ManifestFile, scenario.Environment, platform)
	if err != nil {
		return nil, "", nil, err
	}
	envName, _, err := spec.ResolveEnvironment(manifest.Environments, platform, scenario.Environment)
	if err != nil {
		return nil, "", nil, err
	}

	if platform == nil {
		return manifest, envName, nil, nil
	}

	envIdentity := spec.NormalizeEnv(envName)
	resolved, _, err := spec.Resolve(manifest, platform, envIdentity, spec.SubstitutionReport{})
	if err != nil {
		return nil, "", nil, err
	}
	return manifest, envName, resolved, nil
}

// renderChart renders manifest/environment through Helm's real template
// engine via [helm.Client.RenderOffline] (no cluster access, matching
// `deployah plan --offline`), returning the resulting Kubernetes objects.
func (suite *IntegrationTestSuite) renderChart(t *testing.T, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) ([]unstructured.Unstructured, error) {
	t.Helper()

	client, err := helm.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create helm client: %w", err)
	}

	result, cleanup, err := client.RenderOffline(context.Background(), manifest, environment, resolved)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	if err != nil {
		return nil, fmt.Errorf("render chart: %w", err)
	}

	return parseManifestYAML(result.Manifest)
}

// parseManifestYAML decodes a "---"-concatenated Kubernetes YAML string
// into unstructured objects, skipping empty documents.
func parseManifestYAML(content string) ([]unstructured.Unstructured, error) {
	var manifests []unstructured.Unstructured

	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(content), 4096)
	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return manifests, fmt.Errorf("decoding manifest YAML: %w", err)
		}
		if obj.Object != nil {
			manifests = append(manifests, obj)
		}
	}

	return manifests, nil
}

// validateAgainstExpected validates rendered manifests against expected files
func (suite *IntegrationTestSuite) validateAgainstExpected(t *testing.T, manifests []unstructured.Unstructured, expectedDir string) {
	t.Helper()
	expectedPath := filepath.Join(suite.ScenariosDir, expectedDir)

	expectedManifests, err := suite.loadExpectedManifests(expectedPath)
	require.NoError(t, err)

	actualMap := make(map[string]unstructured.Unstructured)
	for _, manifest := range manifests {
		key := fmt.Sprintf("%s/%s", manifest.GetKind(), manifest.GetName())
		actualMap[key] = manifest
	}

	expectedMap := make(map[string]unstructured.Unstructured)
	for _, manifest := range expectedManifests {
		key := fmt.Sprintf("%s/%s", manifest.GetKind(), manifest.GetName())
		expectedMap[key] = manifest
	}

	for key, expected := range expectedMap {
		actual, exists := actualMap[key]
		if !exists {
			t.Errorf("Expected resource not found: %s", key)
			continue
		}

		suite.compareManifests(t, actual, expected, key)
		delete(actualMap, key)
	}

	// Check for unexpected resources
	for key := range actualMap {
		t.Errorf("Unexpected resource generated: %s", key)
	}
}

// loadExpectedManifests loads expected Kubernetes manifests from files
func (suite *IntegrationTestSuite) loadExpectedManifests(expectedDir string) ([]unstructured.Unstructured, error) {
	var manifests []unstructured.Unstructured

	err := filepath.Walk(expectedDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		content, err := os.ReadFile(path) // #nosec G304,G122 -- path from filepath.Walk in chart templates dir
		if err != nil {
			return err
		}

		fileManifests, err := parseManifestYAML(string(content))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		manifests = append(manifests, fileManifests...)

		return nil
	})

	return manifests, err
}

// compareManifests compares two Kubernetes manifests
func (suite *IntegrationTestSuite) compareManifests(t *testing.T, actual, expected unstructured.Unstructured, key string) {
	t.Helper()
	assert.Equal(t, expected.GetKind(), actual.GetKind(), "Kind mismatch for %s", key)
	assert.Equal(t, expected.GetName(), actual.GetName(), "Name mismatch for %s", key)

	actualLabels := actual.GetLabels()
	expectedLabels := expected.GetLabels()
	for k, v := range expectedLabels {
		assert.Equal(t, v, actualLabels[k], "Label %s mismatch for %s", k, key)
	}

	actualAnnotations := actual.GetAnnotations()
	expectedAnnotations := expected.GetAnnotations()
	for k, v := range expectedAnnotations {
		assert.Equal(t, v, actualAnnotations[k], "Annotation %s mismatch for %s", k, key)
	}

	actualSpec, actualFound, err := unstructured.NestedMap(actual.Object, "spec")
	require.NoError(t, err)
	expectedSpec, expectedFound, err := unstructured.NestedMap(expected.Object, "spec")
	require.NoError(t, err)

	if expectedFound {
		require.True(t, actualFound, "Expected spec not found for %s", key)
		suite.compareSpecs(t, actualSpec, expectedSpec, key)
	}
}

// compareSpecs compares the spec sections of two manifests, recursing into
// nested maps and slices instead of comparing them with `!=` (which panics
// at runtime for map/slice-typed interface values).
func (suite *IntegrationTestSuite) compareSpecs(t *testing.T, actual, expected map[string]any, resourceName string) {
	t.Helper()
	for key, expectedValue := range expected {
		actualValue, exists := actual[key]
		if !exists {
			t.Errorf("Missing spec field '%s' in resource %s", key, resourceName)
			continue
		}
		compareValues(t, actualValue, expectedValue, fmt.Sprintf("%s.spec.%s", resourceName, key))
	}
}

// compareValues deep-compares an actual/expected pair from decoded YAML
// (map[string]any, []any, or a scalar), recursing field-by-field so a
// mismatch anywhere reports its exact dotted/indexed path instead of
// comparing whole maps or slices with `!=`, which panics at runtime for
// non-comparable dynamic types.
func compareValues(t *testing.T, actual, expected any, path string) {
	t.Helper()
	switch expectedTyped := expected.(type) {
	case map[string]any:
		actualTyped, ok := actual.(map[string]any)
		if !ok {
			t.Errorf("%s: expected a map, got %T (%v)", path, actual, actual)
			return
		}
		for key, expectedValue := range expectedTyped {
			actualValue, exists := actualTyped[key]
			if !exists {
				t.Errorf("Missing field '%s'", path+"."+key)
				continue
			}
			compareValues(t, actualValue, expectedValue, path+"."+key)
		}

	case []any:
		actualTyped, ok := actual.([]any)
		if !ok {
			t.Errorf("%s: expected a list, got %T (%v)", path, actual, actual)
			return
		}
		if len(actualTyped) != len(expectedTyped) {
			t.Errorf("%s: length mismatch: expected %d, got %d", path, len(expectedTyped), len(actualTyped))
			return
		}
		for i, expectedValue := range expectedTyped {
			compareValues(t, actualTyped[i], expectedValue, fmt.Sprintf("%s[%d]", path, i))
		}

	default:
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("%s mismatch: expected %v, got %v", path, expected, actual)
		}
	}
}

// saveRenderedManifests saves rendered manifests to files for inspection
func (suite *IntegrationTestSuite) saveRenderedManifests(t *testing.T, manifests []unstructured.Unstructured, scenarioName string) {
	t.Helper()
	outputDir := filepath.Join(suite.OutputDir, scenarioName, "rendered")
	err := os.MkdirAll(outputDir, 0o750)
	require.NoError(t, err)

	for _, manifest := range manifests {
		kind := strings.ToLower(manifest.GetKind())
		name := manifest.GetName()
		filename := fmt.Sprintf("%s-%s.yaml", kind, name)
		filepath := filepath.Join(outputDir, filename)

		data, marshalErr := yaml.Marshal(manifest.Object)
		require.NoError(t, marshalErr)

		marshalErr = os.WriteFile(filepath, data, 0o600)
		require.NoError(t, marshalErr)
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src) // #nosec G304 -- src from scenario test setup
	if err != nil {
		return err
	}

	destFile, err := os.Create(dst) // #nosec G304 -- dst from scenario test setup
	if err != nil {
		if closeErr := sourceFile.Close(); closeErr != nil {
			return fmt.Errorf("create destination: %w", errors.Join(err, closeErr))
		}
		return err
	}

	_, copyErr := destFile.ReadFrom(sourceFile)
	srcCloseErr := sourceFile.Close()
	dstCloseErr := destFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if srcCloseErr != nil {
		return srcCloseErr
	}
	return dstCloseErr
}
