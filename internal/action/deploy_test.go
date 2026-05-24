package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/manifest"
)

type mockDeployer struct {
	err error
}

func (m *mockDeployer) InstallApp(_ context.Context, _ *manifest.Manifest, _ string, _ bool) error {
	return m.err
}

type mockManifestLoader struct {
	m   *manifest.Manifest
	err error
}

func (m *mockManifestLoader) Manifest(_ context.Context, _ string) (*manifest.Manifest, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.m, nil
}

var testManifest = &manifest.Manifest{
	APIVersion: "v1-alpha.1",
	Project:    "my-app",
}

// Run loads the manifest and delegates to the deployer.
func TestDeploy_Run(t *testing.T) {
	t.Run("succeeds when deployer and loader succeed", func(t *testing.T) {
		d := action.NewDeploy(&mockDeployer{}, &mockManifestLoader{m: testManifest})
		m, err := d.Run(context.Background(), "prod", false)
		require.NoError(t, err)
		assert.Equal(t, "my-app", m.Project)
	})

	t.Run("returns error when manifest loader fails", func(t *testing.T) {
		d := action.NewDeploy(&mockDeployer{}, &mockManifestLoader{err: fmt.Errorf("not found")})
		_, err := d.Run(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "load manifest")
	})

	t.Run("returns error when deployer fails", func(t *testing.T) {
		d := action.NewDeploy(&mockDeployer{err: fmt.Errorf("helm error")}, &mockManifestLoader{m: testManifest})
		_, err := d.Run(context.Background(), "prod", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "install")
	})
}
