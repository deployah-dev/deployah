package initialize

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/spec"
)

// nabatContext builds a minimal *nabat.Context for tests that call
// functions requiring one.
func nabatContext(t *testing.T) *nabat.Context {
	t.Helper()
	var captured *nabat.Context
	// Non-TTY: any prompt reachable from the tested function must have a
	// fallback, or must not be reached.
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	app.MustCommand("run", nabat.WithRun(func(c *nabat.Context) error {
		captured = c
		return nil
	}))
	require.NoError(t, nabattest.Run(t, app, []string{"run"}))
	return captured
}

// TestShowSummaryAndSave_RoleAwareComponentsProduceValidSpec is an
// end-to-end test for the role-aware component flow.
func TestShowSummaryAndSave_RoleAwareComponentsProduceValidSpec(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	// One component per role the wizard offers: service (with a port and
	// an HTTP health check), worker, and job.
	config := &ProjectConfig{
		Name:             "shop",
		EnvironmentNames: []string{"local"},
		Components: map[string]spec.Component{
			"web": {
				Role:           spec.ComponentRoleService,
				Image:          "nginx:1.28.0-alpine",
				Port:           8080,
				ResourcePreset: spec.ResourcePresetSmall,
				Health: &spec.Health{
					Ready: &spec.HealthReady{Path: "/healthz"},
					Alive: &spec.HealthAlive{Path: "/healthz"},
				},
			},
			"worker": {
				Role:           spec.ComponentRoleWorker,
				Image:          "shop/worker:1.0.0",
				ResourcePreset: spec.ResourcePresetSmall,
			},
			"migrate": {
				Role:           spec.ComponentRoleJob,
				Image:          "shop/migrate:1.0.0",
				ResourcePreset: spec.ResourcePresetSmall,
			},
		},
		OutputPath: outputPath,
	}

	c := nabatContext(t)
	require.NoError(t, showSummaryAndSave(c, config))

	// Load runs the same schema and cross-field validation as
	// "deployah validate", catching gaps saving alone would miss.
	loaded, err := spec.Load(t.Context(), outputPath, "", nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "shop", loaded.Project)
	assert.Empty(t, loaded.Environments,
		"the spec must not register environments; the platform file owns them")

	platformPath := filepath.Join(dir, spec.DefaultPlatformPath)
	platform, loadErr := spec.LoadPlatform(platformPath)
	require.NoError(t, loadErr)
	_, hasLocal := platform.Environments["local"]
	assert.True(t, hasLocal, "the local environment should have a full platform entry")
}

// TestShowSummaryAndSave_ServiceHealthCheckWithoutPortIsValid locks in the
// port default: a service with a health path and no explicit port saves
// fine because the schema defaults the port to 8080 at load time.
func TestShowSummaryAndSave_ServiceHealthCheckWithoutPortIsValid(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	config := &ProjectConfig{
		Name:             "shop",
		EnvironmentNames: []string{"local"},
		Components: map[string]spec.Component{
			"web": {
				Role:           spec.ComponentRoleService,
				Image:          "nginx:1.28.0-alpine",
				ResourcePreset: spec.ResourcePresetSmall,
				Health: &spec.Health{
					Ready: &spec.HealthReady{Path: "/healthz"},
				},
			},
		},
		OutputPath: outputPath,
	}

	c := nabatContext(t)
	require.NoError(t, showSummaryAndSave(c, config))

	loaded, err := spec.Load(t.Context(), outputPath, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 8080, loaded.Components["web"].Port)
}

// TestShowSummaryAndSave_NonLocalOnlyRegistersEmptyEntry verifies that a
// non-local environment is still registered in the platform file (as an
// empty entry the user fills in later): the platform file owns which
// environments exist.
func TestShowSummaryAndSave_NonLocalOnlyRegistersEmptyEntry(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "deployah.yaml")

	config := &ProjectConfig{
		Name:             "shop",
		EnvironmentNames: []string{"production"},
		Components: map[string]spec.Component{
			"web": {
				Role:           spec.ComponentRoleService,
				Image:          "nginx:1.28.0-alpine",
				Port:           8080,
				ResourcePreset: spec.ResourcePresetSmall,
			},
		},
		OutputPath: outputPath,
	}

	c := nabatContext(t)
	require.NoError(t, showSummaryAndSave(c, config))

	platformPath := filepath.Join(dir, spec.DefaultPlatformPath)
	platform, loadErr := spec.LoadPlatform(platformPath)
	require.NoError(t, loadErr)
	production, hasProduction := platform.Environments["production"]
	require.True(t, hasProduction, "production must be registered in the platform file")
	assert.Empty(t, production.Context)
}
