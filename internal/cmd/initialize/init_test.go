package initialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/spec"
)

// TestCheckOverwrite verifies --force skips the prompt and a missing file
// proceeds without asking.
func TestCheckOverwrite(t *testing.T) {
	t.Parallel()

	existing := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("apiVersion: v1-alpha.5\n"), 0o600))
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	tests := []struct {
		name        string
		path        string
		skipPrompt  bool
		wantProceed bool
	}{
		{
			name:        "skipPrompt with existing file always proceeds",
			path:        existing,
			skipPrompt:  true,
			wantProceed: true,
		},
		{
			name:        "skipPrompt with missing file always proceeds",
			path:        missing,
			skipPrompt:  true,
			wantProceed: true,
		},
		{
			name:        "no skipPrompt, missing file proceeds",
			path:        missing,
			skipPrompt:  false,
			wantProceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			io, _, _, _ := nabattest.NewIO()
			app := nabat.MustNew("test", nabat.WithIO(io))
			c := nabattest.Context(t, app)

			proceed, err := checkOverwrite(c, tt.path, tt.skipPrompt)
			require.NoError(t, err)
			assert.Equal(t, tt.wantProceed, proceed)
		})
	}
}

func TestCheckOverwrite_RequiresTTY(t *testing.T) {
	t.Parallel()

	existing := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("apiVersion: v1-alpha.5\n"), 0o600))

	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	c := nabattest.Context(t, app)

	proceed, err := checkOverwrite(c, existing, false)
	require.ErrorIs(t, err, nabat.ErrConfirmationRequired)
	var ce *nabat.ConfirmationError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "--force", ce.BypassHint)
	assert.False(t, proceed)
}

func TestCheckOverwrite_StatError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o600))
	target := filepath.Join(notDir, "deployah.yaml")

	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	c := nabattest.Context(t, app)

	proceed, err := checkOverwrite(c, target, false)
	require.Error(t, err)
	assert.False(t, proceed)
	assert.ErrorContains(t, err, "stat")
}

// TestStatefulWizardDefaultsMatchValidation verifies stateful shapes the
// wizard can produce (with or without a volume) pass persistence and
// replica validation.
func TestStatefulWizardDefaultsMatchValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		component spec.Component
	}{
		{
			name: "with persistence",
			component: spec.Component{
				Kind:     spec.ComponentKindStateful,
				Image:    "postgres:16",
				Replicas: new(1),
				Persistence: &spec.Persistence{
					Size:      "20Gi",
					MountPath: "/data",
				},
			},
		},
		{
			name: "identity only",
			component: spec.Component{
				Kind:     spec.ComponentKindStateful,
				Image:    "redis:7-alpine",
				Replicas: new(1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, spec.ValidateComponentPersistence(tt.component))
			require.NoError(t, spec.ValidateComponentReplicas(tt.component))
			assert.Nil(t, tt.component.Autoscaling)
		})
	}
}

func TestLockedInitCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "not interactive", got: errNotInteractive.Error(), want: "deployah init is interactive: run it from a terminal"},
		{name: "local kind prompt", got: promptLocalKind, want: "Set up a local Kind cluster (deployah cluster up)?"},
		{name: "other envs prompt", got: promptOtherEnvs, want: "Other environments (comma-separated). Empty is fine if you chose local."},
		{name: "component name hint", got: descComponentName, want: "e.g. web or api"},
		{name: "image prompt", got: promptImageFmt, want: "Container image for %s (registry/name:tag), e.g. nginx:latest"},
		{name: "advanced prompt", got: promptAdvancedFmt, want: "Configure advanced options for %s? (kind, health, env vars, scaling). You can edit YAML later."},
		{name: "expose prompt", got: promptExposeFmt, want: "Give %s a public URL (%s.%s.nip.io) with HTTPS?"},
		{name: "manifest schema URL", got: spec.ManifestSchemaURL(), want: "https://deployah.dev/schemas/v1-alpha.5/manifest.json"},
		{name: "platform schema URL", got: spec.PlatformSchemaURL(), want: "https://deployah.dev/schemas/platform/v1-alpha.3/platform.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestPrintInitCompleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dryRun  bool
		want    string
		notWant string
	}{
		{
			name:    "saved spec",
			want:    "Project initialization completed",
			notWant: "dry-run",
		},
		{
			name:   "dry-run",
			dryRun: true,
			want:   "Project initialization completed (dry-run mode)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			io, _, _, stderr := nabattest.NewIO()
			app := nabat.MustNew("test", nabat.WithIO(io))
			c := nabattest.Context(t, app)
			printInitCompleted(c, &ProjectConfig{
				Name:             "shop",
				EnvironmentNames: []string{"local"},
				Components:       map[string]spec.Component{"web": {}},
				SpecPath:         "deployah.yaml",
				DryRun:           tt.dryRun,
			})
			got := stderr.String()
			assert.Contains(t, got, tt.want)
			if tt.notWant != "" {
				assert.NotContains(t, got, tt.notWant)
			}
		})
	}
}

func TestRunInit_NoTTYIsInteractiveError(t *testing.T) {
	t.Parallel()

	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("deployah", nabat.WithIO(io))
	Register(app)
	err := nabattest.Run(t, app, []string{"init"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errNotInteractive)
}

func TestCollectProjectName_RequiresTTY(t *testing.T) {
	t.Parallel()
	io, _, _, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	err := collectProjectName(nabattest.Context(t, app), &ProjectConfig{})
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect project name")
}
