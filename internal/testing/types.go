package testing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/manifest"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// TestScenariosDir is the directory containing test scenarios
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
			return path
		}
	}

	// Default fallback
	return "scenarios"
}

// TestScenario represents a single test scenario
type TestScenario struct {
	Name           string
	ScenarioDir    string
	ManifestFile   string
	Environment    string
	EnvFiles       []string
	ExpectedDir    string
	ExpectError    bool     // Simple: just check if error occurs
	ExpectedErrors []string // Advanced: check for specific error messages
}

// IntegrationTestSuite provides comprehensive testing using scenario directories
type IntegrationTestSuite struct {
	ScenariosDir string
	OutputDir    string
}

// NewIntegrationTestSuite creates a new integration test suite
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
		// Setup test environment by copying scenario files to a temporary directory
		testDir := suite.setupScenarioEnvironment(t, scenario)

		t.Chdir(testDir)

		// Phase 1: Load and validate manifest
		manifest, err := suite.loadManifest(t, scenario.ManifestFile)

		// Hybrid error checking approach
		if scenario.ExpectError || len(scenario.ExpectedErrors) > 0 {
			// This is an error scenario - we EXPECT an error
			require.Error(t, err)

			// If specific errors are expected, check them
			if len(scenario.ExpectedErrors) > 0 {
				for _, expectedErr := range scenario.ExpectedErrors {
					assert.Contains(t, err.Error(), expectedErr)
				}
			}
			return // Don't proceed to chart generation
		}

		// Normal scenario - should NOT have errors
		require.NoError(t, err)

		// Phase 2: Generate Helm chart and values
		chartPath, err := suite.generateChart(t, manifest, scenario.Environment)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, os.RemoveAll(chartPath))
		})

		// Phase 3: Render Kubernetes manifests
		renderedManifests, err := suite.renderManifests(t, chartPath)
		require.NoError(t, err)

		// Phase 4: Validate against expected output
		if scenario.ExpectedDir != "" {
			expectedPath := filepath.Join(suite.ScenariosDir, scenario.ExpectedDir)
			if _, statErr := os.Stat(expectedPath); statErr == nil {
				suite.validateAgainstExpected(t, renderedManifests, scenario.ExpectedDir)
			}
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

	// Copy environment files
	for _, envFile := range scenario.EnvFiles {
		envSrc := filepath.Join(suite.ScenariosDir, scenario.ScenarioDir, envFile)
		envDst := filepath.Join(testDir, filepath.Base(envFile))
		err = copyFile(envSrc, envDst)
		require.NoError(t, err)
	}

	return testDir
}

// loadManifest loads a manifest from the current directory
func (suite *IntegrationTestSuite) loadManifest(t *testing.T, manifestFile string) (*manifest.Manifest, error) {
	t.Helper()
	ctx := context.Background()
	return manifest.Load(ctx, "deployah.yaml", "")
}

// generateChart generates the Helm chart from the manifest
func (suite *IntegrationTestSuite) generateChart(t *testing.T, manifest *manifest.Manifest, environment string) (string, error) {
	t.Helper()
	ctx := context.Background()
	return helm.PrepareChart(ctx, manifest, environment)
}

// renderManifests renders the Kubernetes manifests from the Helm chart
func (suite *IntegrationTestSuite) renderManifests(t *testing.T, chartPath string) ([]unstructured.Unstructured, error) {
	t.Helper()
	return suite.renderWithHelm(t, chartPath)
}

// renderWithHelm uses Helm to render templates to Kubernetes manifests
func (suite *IntegrationTestSuite) renderWithHelm(t *testing.T, chartPath string) ([]unstructured.Unstructured, error) {
	t.Helper()
	var manifests []unstructured.Unstructured

	templatesDir := filepath.Join(chartPath, "templates")
	if _, err := os.Stat(templatesDir); errors.Is(err, fs.ErrNotExist) {
		return manifests, nil
	}

	err := filepath.Walk(templatesDir, func(path string, info os.FileInfo, walkErr error) error {
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

		// Parse YAML documents
		decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(string(content)), 4096)
		for {
			var obj unstructured.Unstructured
			if decodeErr := decoder.Decode(&obj); decodeErr != nil {
				if errors.Is(decodeErr, io.EOF) {
					break
				}
				continue // Skip invalid YAML
			}

			if obj.Object != nil {
				manifests = append(manifests, obj)
			}
		}

		return nil
	})

	return manifests, err
}

// validateAgainstExpected validates rendered manifests against expected files
func (suite *IntegrationTestSuite) validateAgainstExpected(t *testing.T, manifests []unstructured.Unstructured, expectedDir string) {
	t.Helper()
	expectedPath := filepath.Join(suite.ScenariosDir, expectedDir)

	// Load expected manifests
	expectedManifests, err := suite.loadExpectedManifests(expectedPath)
	require.NoError(t, err)

	// Create maps for comparison
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

	// Compare manifests
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

		// Parse YAML documents
		decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(string(content)), 4096)
		for {
			var obj unstructured.Unstructured
			if decodeErr := decoder.Decode(&obj); decodeErr != nil {
				if errors.Is(decodeErr, io.EOF) {
					break
				}
				continue // Skip invalid YAML
			}

			if obj.Object != nil {
				manifests = append(manifests, obj)
			}
		}

		return nil
	})

	return manifests, err
}

// compareManifests compares two Kubernetes manifests
func (suite *IntegrationTestSuite) compareManifests(t *testing.T, actual, expected unstructured.Unstructured, key string) {
	t.Helper()
	// Compare basic metadata
	assert.Equal(t, expected.GetKind(), actual.GetKind(), "Kind mismatch for %s", key)
	assert.Equal(t, expected.GetName(), actual.GetName(), "Name mismatch for %s", key)

	// Compare labels
	actualLabels := actual.GetLabels()
	expectedLabels := expected.GetLabels()
	for k, v := range expectedLabels {
		assert.Equal(t, v, actualLabels[k], "Label %s mismatch for %s", k, key)
	}

	// Compare annotations
	actualAnnotations := actual.GetAnnotations()
	expectedAnnotations := expected.GetAnnotations()
	for k, v := range expectedAnnotations {
		assert.Equal(t, v, actualAnnotations[k], "Annotation %s mismatch for %s", k, key)
	}

	// Compare spec (basic comparison)
	actualSpec, actualFound, err := unstructured.NestedMap(actual.Object, "spec")
	require.NoError(t, err)
	expectedSpec, expectedFound, err := unstructured.NestedMap(expected.Object, "spec")
	require.NoError(t, err)

	if expectedFound {
		require.True(t, actualFound, "Expected spec not found for %s", key)
		suite.compareSpecs(t, actualSpec, expectedSpec, key)
	}
}

// compareSpecs compares the spec sections of two manifests
func (suite *IntegrationTestSuite) compareSpecs(t *testing.T, actual, expected map[string]any, resourceName string) {
	t.Helper()
	for key, expectedValue := range expected {
		actualValue, exists := actual[key]
		if !exists {
			t.Errorf("Missing spec field '%s' in resource %s", key, resourceName)
			continue
		}

		// Basic comparison - you might want to expand this
		if expectedValue != actualValue {
			t.Errorf("Spec field '%s' mismatch in %s: expected %v, got %v", key, resourceName, expectedValue, actualValue)
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

		// Convert to YAML
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
