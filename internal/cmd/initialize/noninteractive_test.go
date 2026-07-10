package initialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/spec"
)

// newInitApp builds a fresh, isolated *nabat.App wired to io, so no flag
// state leaks between tests.
func newInitApp(io *nabat.IOStreams) *nabat.App {
	app := nabat.MustNew("deployah", nabat.WithIO(io))
	Register(app)
	return app
}

// validateManifestFile mirrors the checks "deployah validate" runs:
// apiVersion, environments, and schema validation.
func validateManifestFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, err)

	var specObj map[string]any
	require.NoError(t, yaml.Unmarshal(data, &specObj))

	version, err := spec.ValidateAPIVersion(specObj)
	require.NoError(t, err)
	require.NoError(t, spec.ValidateEnvironments(specObj, version))
	require.NoError(t, spec.ValidateSpec(specObj, version))
}

// TestInit_DefaultsProducesValidSpec verifies --defaults, with no prompting
// at all, produces a spec that passes validation.
func TestInit_DefaultsProducesValidSpec(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	require.NoError(t, nabattest.Run(t, app, []string{"init", "--defaults", "--output", outputPath}))

	validateManifestFile(t, outputPath)
}

// TestInit_DefaultsWithoutForceAgainstExistingFileFails locks in that
// --defaults does not imply --force.
func TestInit_DefaultsWithoutForceAgainstExistingFileFails(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(outputPath, []byte("apiVersion: v1-alpha.2\n"), 0o600))

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	err := nabattest.Run(t, app, []string{"init", "--defaults", "--output", outputPath})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force") // grep-friendly error, not a stack trace
}

// TestInit_DefaultsWithForceAgainstExistingFileSucceeds verifies --force
// unblocks --defaults against an existing file.
func TestInit_DefaultsWithForceAgainstExistingFileSucceeds(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")
	require.NoError(t, os.WriteFile(outputPath, []byte("apiVersion: v1-alpha.2\n"), 0o600))

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	require.NoError(t, nabattest.Run(t, app, []string{"init", "--defaults", "--force", "--output", outputPath}))

	validateManifestFile(t, outputPath)
}

// TestInit_SetCreatesValidComponentOverride verifies a --set on a string
// field lands in the saved spec.
func TestInit_SetCreatesValidComponentOverride(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	require.NoError(t, nabattest.Run(t, app, []string{
		"init", "--defaults", "--output", outputPath,
		"--set", "components.app.image=nginx:1.25",
	}))

	validateManifestFile(t, outputPath)

	data, err := os.ReadFile(outputPath) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, err)
	var specObj map[string]any
	require.NoError(t, yaml.Unmarshal(data, &specObj))
	components, ok := specObj["components"].(map[string]any)
	require.True(t, ok)
	appComponent, ok := components["app"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "nginx:1.25", appComponent["image"])
}

// TestInit_SetCoercesTypedFields verifies --set on a schema-typed field is
// coerced to that type instead of being written as a string.
func TestInit_SetCoercesTypedFields(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	require.NoError(t, nabattest.Run(t, app, []string{
		"init", "--defaults", "--output", outputPath,
		"--set", "components.app.port=8080", // integer
		"--set", "components.app.autoscaling.enabled=true", // boolean
		"--set", "components.app.autoscaling.minReplicas=2", // required alongside enabled
		"--set", "components.app.autoscaling.maxReplicas=5",
	}))

	validateManifestFile(t, outputPath)

	data, err := os.ReadFile(outputPath) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, err)
	var specObj map[string]any
	require.NoError(t, yaml.Unmarshal(data, &specObj))
	components, ok := specObj["components"].(map[string]any)
	require.True(t, ok)
	appComponent, ok := components["app"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 8080, appComponent["port"])
	autoscaling, ok := appComponent["autoscaling"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, autoscaling["enabled"])
}

// TestInit_SetInvalidPathErrorIncludesToken verifies a typo'd --set path
// produces an error naming the offending token.
func TestInit_SetInvalidPathErrorIncludesToken(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	err := nabattest.Run(t, app, []string{
		"init", "--defaults", "--output", outputPath,
		"--set", "components.app.imagee=nginx:1.25",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "imagee") // so CI logs point straight at the mistake
}

// TestInit_ProjectFlagInvalidGivesFriendlyError verifies an invalid
// --project value fails immediately with a friendly error.
func TestInit_ProjectFlagInvalidGivesFriendlyError(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	err := nabattest.Run(t, app, []string{
		"init", "--defaults", "--output", outputPath,
		"--project", "Invalid Name",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project name")  // ValidateProjectName's message
	assert.NotContains(t, err.Error(), "jsonschema") // not a raw schema error
}

// TestInit_EnvironmentsFlagInvalidGivesFriendlyError verifies an invalid
// --environments entry fails immediately with a friendly error.
func TestInit_EnvironmentsFlagInvalidGivesFriendlyError(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	err := nabattest.Run(t, app, []string{
		"init", "--defaults", "--output", outputPath,
		"--environments", "Bad_Env",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "environment name") // ValidateEnvName's message
	assert.NotContains(t, err.Error(), "jsonschema")    // not a raw schema error
}

// TestInit_NoTTYNoFlagsBehavesLikeDefaults verifies that running with no
// TTY and no flags behaves like --defaults instead of crashing on the
// first prompt without a fallback.
func TestInit_NoTTYNoFlagsBehavesLikeDefaults(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO() // reports non-TTY by default
	app := newInitApp(io)
	require.NoError(t, nabattest.Run(t, app, []string{"init", "--output", outputPath}))

	validateManifestFile(t, outputPath)

	data, err := os.ReadFile(outputPath) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, err)
	var specObj map[string]any
	require.NoError(t, yaml.Unmarshal(data, &specObj))
	_, hasEnvironments := specObj["environments"]
	assert.False(t, hasEnvironments,
		"the spec must not register environments; the platform file owns them")

	platform, loadErr := spec.LoadPlatform(filepath.Join(dir, spec.DefaultPlatformPath))
	require.NoError(t, loadErr)
	_, hasLocal := platform.Environments[DefaultEnvironmentName]
	assert.True(t, hasLocal)
}

// TestInit_DefaultsForceThenValidate verifies "deployah init --defaults"
// followed by "deployah validate" both succeed.
func TestInit_DefaultsForceThenValidate(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := newInitApp(io)
	require.NoError(t, nabattest.Run(t, app, []string{"init", "--defaults", "--force", "--output", outputPath}))

	validateManifestFile(t, outputPath)
}
