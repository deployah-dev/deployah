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
	"deployah.dev/deployah/internal/k8s"
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

		manifest, environment, resolved, err := suite.loadAndResolve(t, scenario)

		if scenario.ExpectError || len(scenario.ExpectedErrors) > 0 {
			require.Error(t, err)
			for _, expectedErr := range scenario.ExpectedErrors {
				assert.Contains(t, err.Error(), expectedErr)
			}
			return
		}
		require.NoError(t, err)

		renderedManifests, err := suite.renderChart(t, manifest, environment, resolved)
		require.NoError(t, err)

		// Every rendered object must decode cleanly into its typed
		// Kubernetes API struct, catching wrong field names/types that a
		// subset-only comparison would miss.
		validateAgainstScheme(t, renderedManifests)

		if scenario.ExpectedDir != "" {
			expectedPath := filepath.Join(suite.ScenariosDir, scenario.ExpectedDir)
			if _, statErr := os.Stat(expectedPath); statErr != nil {
				t.Fatalf("expected output directory %s vanished after discovery: %v", expectedPath, statErr)
			}
			suite.validateAgainstExpected(t, renderedManifests, scenario.ExpectedDir)
		}

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

	// Materialize self-signed TLS certs offline (no cluster access),
	// matching `deployah plan --offline`. Without this, a selfSigned
	// expose scenario fails render with "certificate not materialized
	// before render".
	if tlsErr := k8s.MaterializeSelfSignedTLS(ctx, nil, "", resolved); tlsErr != nil {
		return nil, "", nil, tlsErr
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

// validateAgainstExpected compares each rendered manifest against an
// exact golden file (dir/<kind>-<name>.yaml, lowercased kind). Run with
// `go test -update` to regenerate every golden file from the current
// render.
func (suite *IntegrationTestSuite) validateAgainstExpected(t *testing.T, manifests []unstructured.Unstructured, expectedDir string) {
	t.Helper()
	expectedPath := filepath.Join(suite.ScenariosDir, expectedDir)

	if *updateGolden {
		suite.writeGoldenManifests(t, manifests, expectedPath)
		return
	}

	remainingGoldenFiles := listGoldenFiles(t, expectedPath)

	for _, manifest := range manifests {
		filename := goldenFilename(manifest)
		goldenPath := filepath.Join(expectedPath, filename)
		if _, statErr := os.Stat(goldenPath); errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("unexpected resource generated (no golden file %s): %s/%s", filename, manifest.GetKind(), manifest.GetName())
			continue
		}
		compareOrUpdateGolden(t, goldenPath, marshalGolden(t, manifest))
		delete(remainingGoldenFiles, filename)
	}

	for filename := range remainingGoldenFiles {
		t.Errorf("expected resource not found: golden file %s has no matching rendered resource", filename)
	}
}

// writeGoldenManifests regenerates expectedDir from manifests: it removes
// every existing golden YAML file first, so a resource that stops
// rendering doesn't leave a stale golden file behind, then writes one
// golden file per manifest.
func (suite *IntegrationTestSuite) writeGoldenManifests(t *testing.T, manifests []unstructured.Unstructured, expectedDir string) {
	t.Helper()
	entries, err := os.ReadDir(expectedDir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !isYAMLFile(entry.Name()) {
			continue
		}
		require.NoError(t, os.Remove(filepath.Join(expectedDir, entry.Name())))
	}

	for _, manifest := range manifests {
		goldenPath := filepath.Join(expectedDir, goldenFilename(manifest))
		require.NoError(t, os.WriteFile(goldenPath, []byte(marshalGolden(t, manifest)), 0o600))
	}
}

// listGoldenFiles returns the set of golden YAML filenames present in dir.
func listGoldenFiles(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	files := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && isYAMLFile(entry.Name()) {
			files[entry.Name()] = struct{}{}
		}
	}
	return files
}

func isYAMLFile(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

// goldenFilename returns the golden/rendered-output filename for manifest:
// "<kind>-<name>.yaml", lowercased kind, e.g. "deployment-web.yaml".
func goldenFilename(manifest unstructured.Unstructured) string {
	return fmt.Sprintf("%s-%s.yaml", strings.ToLower(manifest.GetKind()), manifest.GetName())
}

// marshalGolden renders manifest as the deterministic YAML stored in and
// compared against golden files: yaml.Marshal sorts map keys, and
// maskNondeterministicFields removes values that legitimately differ
// between runs (e.g. a freshly generated self-signed certificate).
func marshalGolden(t *testing.T, manifest unstructured.Unstructured) string {
	t.Helper()
	masked := manifest.DeepCopy()
	maskNondeterministicFields(t, masked)

	data, err := yaml.Marshal(masked.Object)
	require.NoError(t, err)
	return string(data)
}

// maskNondeterministicFields replaces values that are allowed to differ
// between two otherwise-identical renders, so golden comparisons don't
// flag them as regressions. Currently: a kubernetes.io/tls Secret's
// tls.crt/tls.key, which [k8s.MaterializeSelfSignedTLS] generates fresh
// (a new keypair) on every offline render.
func maskNondeterministicFields(t *testing.T, manifest *unstructured.Unstructured) {
	t.Helper()
	if manifest.GetKind() != "Secret" {
		return
	}
	secretType, _, err := unstructured.NestedString(manifest.Object, "type")
	require.NoError(t, err)
	if secretType != "kubernetes.io/tls" {
		return
	}
	for _, key := range []string{"tls.crt", "tls.key"} {
		_, found, nestedErr := unstructured.NestedString(manifest.Object, "data", key)
		require.NoError(t, nestedErr)
		if found {
			require.NoError(t, unstructured.SetNestedField(manifest.Object, "(masked, generated fresh on every render)", "data", key))
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
		path := filepath.Join(outputDir, goldenFilename(manifest))

		data, marshalErr := yaml.Marshal(manifest.Object)
		require.NoError(t, marshalErr)

		marshalErr = os.WriteFile(path, data, 0o600)
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
