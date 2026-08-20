package initialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/spec"
)

func testConfig(t *testing.T, name string, envNames []string, components map[string]spec.Component) *ProjectConfig {
	t.Helper()
	dir := t.TempDir()
	return &ProjectConfig{
		Name:             name,
		EnvironmentNames: envNames,
		Components:       components,
		SpecPath:         filepath.Join(dir, "deployah.yaml"),
		PlatformPath:     filepath.Join(dir, spec.DefaultPlatformPath),
	}
}

// nabatContext builds a minimal *nabat.Context for tests that call
// functions requiring one. Non-TTY: any prompt reachable from the tested
// function must have a fallback, or must not be reached.
func nabatContext(t *testing.T) *nabat.Context {
	t.Helper()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	return nabattest.Context(t, app)
}

func readSpecFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is from t.TempDir()
	require.NoError(t, err)
	return string(data)
}

func captureShowSummary(t *testing.T, config *ProjectConfig) (out, errOut string) {
	t.Helper()
	io, _, stdout, stderr := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	c := nabattest.Context(t, app)
	saved, err := showSummaryAndSave(c, config)
	require.NoError(t, err)
	require.True(t, saved)
	return stdout.String(), stderr.String()
}

// TestShowSummaryAndSave_RoleAwareComponentsProduceValidSpec verifies a
// service plus worker save a valid sparse spec and a local platform entry.
func TestShowSummaryAndSave_RoleAwareComponentsProduceValidSpec(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"local"}, map[string]spec.Component{
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
	})

	out, errOut := captureShowSummary(t, config)
	combined := out + errOut
	assert.Contains(t, combined, "deployah cluster up")
	assert.Contains(t, combined, "deployah deploy local")
	assert.NotContains(t, combined, "cat ")
	assert.NotContains(t, combined, "deployah validate")

	body := readSpecFile(t, config.SpecPath)
	assert.Contains(t, body, spec.SchemaModeline(spec.ManifestSchemaURL()))
	assert.Contains(t, body, "image: nginx:1.28.0-alpine")
	assert.Contains(t, body, "resourcePreset: small")
	assert.Contains(t, body, "role: worker")
	assert.NotContains(t, body, "role: service")
	assert.NotContains(t, body, "port:")
	assert.NotContains(t, body, "kind:")
	assert.NotContains(t, body, "replicas:")
	assert.NotContains(t, body, "shutdownTimeout:")
	assert.NotContains(t, body, "cpu:")
	assert.NotContains(t, body, "memory:")

	loaded, err := spec.Load(t.Context(), config.SpecPath, "", nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "shop", loaded.Project)
	assert.Empty(t, loaded.Environments,
		"the spec must not register environments; the platform file owns them")
	assert.Equal(t, spec.DefaultServicePort, loaded.Components["web"].Port)

	platform, loadErr := spec.LoadPlatform(config.PlatformPath)
	require.NoError(t, loadErr)
	_, hasLocal := platform.Environments["local"]
	assert.True(t, hasLocal, "the local environment should have a full platform entry")
	assert.Contains(t, readSpecFile(t, config.PlatformPath), spec.SchemaModeline(spec.PlatformSchemaURL()))

	dir := filepath.Dir(config.SpecPath)
	manifestsDir := filepath.Join(dir, spec.DeployahConfigDir, spec.ManifestsDir)
	crdsDir := filepath.Join(dir, spec.DeployahConfigDir, spec.CRDsDir)
	require.DirExists(t, manifestsDir)
	require.DirExists(t, crdsDir)
	require.FileExists(t, filepath.Join(manifestsDir, "README.md"))
	require.FileExists(t, filepath.Join(crdsDir, "README.md"))
}

// TestShowSummaryAndSave_ServiceHealthCheckWithoutPortIsValid verifies a
// service with a health path and no explicit port omits port in YAML, and
// spec.Load still fills the default service port.
func TestShowSummaryAndSave_ServiceHealthCheckWithoutPortIsValid(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"local"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			ResourcePreset: spec.ResourcePresetSmall,
			Health: &spec.Health{
				Ready: &spec.HealthReady{Path: "/healthz"},
			},
		},
	})

	c := nabatContext(t)
	saved, err := showSummaryAndSave(c, config)
	require.NoError(t, err)
	require.True(t, saved)

	body := readSpecFile(t, config.SpecPath)
	assert.NotContains(t, body, "port:")

	loaded, err := spec.Load(t.Context(), config.SpecPath, "", nil)
	require.NoError(t, err)
	assert.Equal(t, spec.DefaultServicePort, loaded.Components["web"].Port)
}

// TestShowSummaryAndSave_NonLocalOnlyRegistersEmptyEntry verifies a
// non-local environment is still registered in the platform file as an
// empty entry the user fills in later.
func TestShowSummaryAndSave_NonLocalOnlyRegistersEmptyEntry(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"production"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			Port:           8080,
			ResourcePreset: spec.ResourcePresetSmall,
		},
	})

	out, errOut := captureShowSummary(t, config)
	combined := out + errOut
	assert.Contains(t, combined, "Fill in context and domains")
	assert.Contains(t, combined, "deployah deploy production")
	assert.NotContains(t, combined, "deployah cluster up")

	platform, loadErr := spec.LoadPlatform(config.PlatformPath)
	require.NoError(t, loadErr)
	production, hasProduction := platform.Environments["production"]
	require.True(t, hasProduction, "production must be registered in the platform file")
	assert.Empty(t, production.Context)
}

// TestShowSummaryAndSave_MergeListsOnlyAddedNames verifies the merge success
// line names only environments that were inserted, not ones already present.
func TestShowSummaryAndSave_MergeListsOnlyAddedNames(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"staging", "production"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			ResourcePreset: spec.ResourcePresetSmall,
		},
	})
	require.NoError(t, os.WriteFile(config.PlatformPath, []byte("apiVersion: platform/v1-alpha.3\nenvironments:\n  staging:\n    context: existing\n"), 0o600))

	_, errOut := captureShowSummary(t, config)
	assert.Contains(t, errOut, "Added missing environments to "+config.PlatformPath+": production")
	assert.NotContains(t, errOut, "Added missing environments to "+config.PlatformPath+": production and staging")

	platform, loadErr := spec.LoadPlatform(config.PlatformPath)
	require.NoError(t, loadErr)
	assert.Equal(t, "existing", platform.Environments["staging"].Context)
	assert.Empty(t, platform.Environments["production"].Context)
}

// TestShowSummaryAndSave_PlatformWriteError verifies a platform write failure
// after the spec is saved returns an error instead of reporting success.
func TestShowSummaryAndSave_PlatformWriteError(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"local"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			ResourcePreset: spec.ResourcePresetSmall,
		},
	})
	require.NoError(t, os.Mkdir(config.PlatformPath, 0o750))

	c := nabatContext(t)
	saved, err := showSummaryAndSave(c, config)
	require.Error(t, err)
	assert.False(t, saved)
	assert.ErrorContains(t, err, "saved spec but failed to update platform file")
	require.FileExists(t, config.SpecPath)
}

// TestShowSummaryAndSave_ExposeWithoutDomainsWarns verifies exposing a
// component in a non-local env with no domains prints a warning.
func TestShowSummaryAndSave_ExposeWithoutDomainsWarns(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"production"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			ResourcePreset: spec.ResourcePresetSmall,
			Expose:         &spec.Expose{},
		},
	})

	_, errOut := captureShowSummary(t, config)
	assert.Contains(t, errOut, "production")
	assert.Contains(t, errOut, "no domains yet")
}

// TestScaffoldExtrasDirs_Idempotent verifies a second scaffold leaves
// existing extras dirs in place.
func TestScaffoldExtrasDirs_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	created, err := scaffoldExtrasDirs(dir)
	require.NoError(t, err)
	assert.True(t, created)

	created, err = scaffoldExtrasDirs(dir)
	require.NoError(t, err)
	assert.False(t, created)
}

// TestShowSummaryAndSave_ExtrasAlreadyExist verifies save notes extras dirs
// that were already on disk.
func TestShowSummaryAndSave_ExtrasAlreadyExist(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"local"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			Port:           8080,
			ResourcePreset: spec.ResourcePresetSmall,
		},
	})
	dir := filepath.Dir(config.SpecPath)
	manifestsDir := filepath.Join(dir, spec.DeployahConfigDir, spec.ManifestsDir)
	crdsDir := filepath.Join(dir, spec.DeployahConfigDir, spec.CRDsDir)
	require.NoError(t, os.MkdirAll(manifestsDir, 0o750))
	require.NoError(t, os.MkdirAll(crdsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(manifestsDir, "README.md"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(crdsDir, "README.md"), []byte("x"), 0o600))

	_, errOut := captureShowSummary(t, config)
	assert.Contains(t, errOut, ".deployah/manifests/ and .deployah/crds/ already exist")
}

// TestShowSummaryAndSave_DryRunWritesNothing verifies --dry-run previews
// without writing the spec or platform file.
func TestShowSummaryAndSave_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"local"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:1.28.0-alpine",
			ResourcePreset: spec.ResourcePresetSmall,
		},
	})
	config.DryRun = true

	c := nabatContext(t)
	saved, err := showSummaryAndSave(c, config)
	require.NoError(t, err)
	require.True(t, saved)
	_, err = os.Stat(config.SpecPath)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(config.PlatformPath)
	assert.True(t, os.IsNotExist(err))
}

func TestSparseSpec_Table(t *testing.T) {
	t.Parallel()

	cpu := resource.MustParse("500m")
	memory := resource.MustParse("512Mi")

	tests := []struct {
		name     string
		comp     spec.Component
		contains []string
		omits    []string
	}{
		{
			name: "service local expose small omits schema defaults",
			comp: spec.Component{
				Role:           spec.ComponentRoleService,
				Image:          "nginx:latest",
				Port:           8080,
				ResourcePreset: spec.ResourcePresetSmall,
				Expose:         &spec.Expose{},
			},
			contains: []string{
				spec.SchemaModeline(spec.ManifestSchemaURL()),
				"image: nginx:latest",
				"resourcePreset: small",
				"expose: true",
			},
			omits: []string{"role:", "port:", "kind:", "replicas:", "shutdownTimeout:", "cpu:", "memory:", "resources:"},
		},
		{
			name: "worker writes role and omits port and expose",
			comp: spec.Component{
				Role:           spec.ComponentRoleWorker,
				Image:          "shop/worker:1",
				ResourcePreset: spec.ResourcePresetSmall,
			},
			contains: []string{"role: worker", "image: shop/worker:1"},
			omits:    []string{"port:", "expose:", "resources:"},
		},
		{
			name: "custom resources writes resources not preset",
			comp: spec.Component{
				Role:  spec.ComponentRoleService,
				Image: "nginx:latest",
				Resources: spec.Resources{
					CPU:    &cpu,
					Memory: &memory,
				},
			},
			contains: []string{"cpu:", "memory:"},
			omits:    []string{"resourcePreset:"},
		},
		{
			name: "non-default port is written",
			comp: spec.Component{
				Role:           spec.ComponentRoleService,
				Image:          "nginx:latest",
				Port:           3000,
				ResourcePreset: spec.ResourcePresetSmall,
			},
			contains: []string{"port: 3000"},
			omits:    []string{"role:", "resources:"},
		},
		{
			name: "autoscaling without replicas stays valid after fill",
			comp: spec.Component{
				Role:  spec.ComponentRoleService,
				Image: "nginx:latest",
				Autoscaling: &spec.Autoscaling{
					Enabled:     true,
					MinReplicas: spec.DefaultMinReplicas,
					MaxReplicas: spec.DefaultMaxReplicas,
					Metrics: []spec.Metric{
						{Type: spec.MetricTypeCPU, Target: spec.DefaultCPUTarget},
					},
				},
			},
			contains: []string{"autoscaling:", "enabled: true"},
			omits:    []string{"replicas:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := &ProjectConfig{
				Name:             "shop",
				EnvironmentNames: []string{"local"},
				Components:       map[string]spec.Component{"web": tt.comp},
			}
			sparse := sparseSpec(config)
			body, err := marshalSpecYAML(&sparse)
			require.NoError(t, err)
			got := string(body)
			for _, want := range tt.contains {
				assert.Contains(t, got, want)
			}
			for _, omit := range tt.omits {
				assert.NotContains(t, got, omit)
			}

			clone, err := cloneSpec(&sparse)
			require.NoError(t, err)
			require.NoError(t, spec.FillSpecWithDefaults(clone, clone.APIVersion))
			require.NoError(t, spec.ValidateSpecComponents(clone))
		})
	}
}

func TestSparseSpec_RoundTripLoadFillsPort(t *testing.T) {
	t.Parallel()

	config := testConfig(t, "shop", []string{"local"}, map[string]spec.Component{
		"web": {
			Role:           spec.ComponentRoleService,
			Image:          "nginx:latest",
			Port:           8080,
			ResourcePreset: spec.ResourcePresetSmall,
		},
	})
	sparse := sparseSpec(config)
	require.NoError(t, writeSpecFile(&sparse, config.SpecPath))

	loaded, err := spec.Load(t.Context(), config.SpecPath, "", nil)
	require.NoError(t, err)
	require.NoError(t, spec.ValidateSpecComponents(loaded))
	assert.Equal(t, spec.DefaultServicePort, loaded.Components["web"].Port)
	assert.Equal(t, spec.ComponentRoleService, loaded.Components["web"].Role)
}
