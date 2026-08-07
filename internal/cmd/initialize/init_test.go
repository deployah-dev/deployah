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

// TestCheckOverwrite covers the file-exists x force matrix that guards
// deployah init from silently clobbering an existing spec.
func TestCheckOverwrite(t *testing.T) {
	t.Parallel()

	existing := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("apiVersion: v1-alpha.3\n"), 0o600))
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	tests := []struct {
		name        string
		path        string
		force       bool
		wantProceed bool
		wantErrIs   error
		wantHint    string
	}{
		{
			name:        "force with existing file always proceeds",
			path:        existing,
			force:       true,
			wantProceed: true,
		},
		{
			name:        "force with missing file always proceeds",
			path:        missing,
			force:       true,
			wantProceed: true,
		},
		{
			name:        "no force, missing file proceeds",
			path:        missing,
			force:       false,
			wantProceed: true,
		},
		{
			name:        "no force, existing file, non-interactive fails closed",
			path:        existing,
			force:       false,
			wantProceed: false,
			wantErrIs:   nabat.ErrConfirmationRequired,
			wantHint:    "--force",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			io, _, _, _ := nabattest.NewIO()
			app := nabat.MustNew("test", nabat.WithIO(io))
			c := nabattest.Context(t, app)

			proceed, err := checkOverwrite(c, &Options{Output: tt.path, Force: tt.force})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				var ce *nabat.ConfirmationError
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, tt.wantHint, ce.BypassHint)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantProceed, proceed)
		})
	}
}

// TestStatefulWizardDefaultsMatchValidation ensures the init wizard's
// stateful shapes (with or without persistence) pass validation.
func TestStatefulWizardDefaultsMatchValidation(t *testing.T) {
	t.Parallel()

	replicas := 1
	withDisk := spec.Component{
		Kind:     spec.ComponentKindStateful,
		Image:    "postgres:16",
		Replicas: &replicas,
		Persistence: &spec.Persistence{
			Size:      "20Gi",
			MountPath: "/data",
		},
	}
	require.NoError(t, spec.ValidateComponentPersistence(withDisk))
	require.NoError(t, spec.ValidateComponentReplicas(withDisk))

	identityOnly := spec.Component{
		Kind:     spec.ComponentKindStateful,
		Image:    "redis:7-alpine",
		Replicas: &replicas,
	}
	require.NoError(t, spec.ValidateComponentPersistence(identityOnly))
	require.NoError(t, spec.ValidateComponentReplicas(identityOnly))
	assert.Nil(t, identityOnly.Persistence)
	assert.Nil(t, identityOnly.Autoscaling, "init wizard skips autoscaling for stateful")
}
