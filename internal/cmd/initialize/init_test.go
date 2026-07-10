package initialize

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveOverwrite covers the file-exists x force x interactive matrix
// that guards deployah init from silently clobbering an existing spec.
func TestResolveOverwrite(t *testing.T) {
	t.Parallel()

	existing := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("apiVersion: v1-alpha.2\n"), 0o600))
	missing := filepath.Join(t.TempDir(), "deployah.yaml")

	failConfirm := func(string) (bool, error) {
		return false, errors.New("confirm must not be called on this path")
	}

	tests := []struct {
		name        string
		path        string
		force       bool
		interactive bool
		confirm     func(string) (bool, error)
		wantProceed bool
		wantCancel  string
		wantErr     string
	}{
		{
			name:        "force with existing file always proceeds",
			path:        existing,
			force:       true,
			interactive: false,
			confirm:     failConfirm,
			wantProceed: true,
		},
		{
			name:        "force with missing file always proceeds",
			path:        missing,
			force:       true,
			interactive: false,
			confirm:     failConfirm,
			wantProceed: true,
		},
		{
			name:        "no force, missing file, non-interactive proceeds",
			path:        missing,
			force:       false,
			interactive: false,
			confirm:     failConfirm,
			wantProceed: true,
		},
		{
			name:        "no force, missing file, interactive proceeds",
			path:        missing,
			force:       false,
			interactive: true,
			confirm:     failConfirm,
			wantProceed: true,
		},
		{
			name:        "no force, existing file, non-interactive fails closed",
			path:        existing,
			force:       false,
			interactive: false,
			confirm:     failConfirm,
			wantProceed: false,
			wantErr:     "pass --force to overwrite",
		},
		{
			name:        "no force, existing file, interactive, confirmed",
			path:        existing,
			force:       false,
			interactive: true,
			confirm:     func(string) (bool, error) { return true, nil },
			wantProceed: true,
		},
		{
			name:        "no force, existing file, interactive, declined",
			path:        existing,
			force:       false,
			interactive: true,
			confirm:     func(string) (bool, error) { return false, nil },
			wantProceed: false,
			wantCancel:  "Cancelled: " + existing + " was not modified.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			proceed, cancelMsg, err := resolveOverwrite(tt.path, tt.force, tt.interactive, tt.confirm)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantProceed, proceed)
			assert.Equal(t, tt.wantCancel, cancelMsg)
		})
	}
}

// TestResolveOverwrite_ConfirmError verifies that an error from the confirm
// callback propagates instead of being swallowed.
func TestResolveOverwrite_ConfirmError(t *testing.T) {
	t.Parallel()

	existing := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("apiVersion: v1-alpha.2\n"), 0o600))

	sentinel := errors.New("boom")
	_, _, err := resolveOverwrite(existing, false, true, func(string) (bool, error) {
		return false, sentinel
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}
