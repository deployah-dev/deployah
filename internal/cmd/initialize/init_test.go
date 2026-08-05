package initialize

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
)

// TestCheckOverwrite covers the file-exists x force matrix that guards
// deployah init from silently clobbering an existing spec.
func TestCheckOverwrite(t *testing.T) {
	t.Parallel()

	existing := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("apiVersion: v1-alpha.2\n"), 0o600))
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
